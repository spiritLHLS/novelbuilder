from __future__ import annotations
import asyncio
import json
import logging
import os
import re

import httpx
import psycopg2
import psycopg2.extras
from fastapi import APIRouter, BackgroundTasks, HTTPException
from fastapi.responses import StreamingResponse
from pydantic import BaseModel
from typing import Optional

from analyzers.style_analyzer import StyleAnalyzer
from analyzers.narrative_analyzer import NarrativeAnalyzer
from analyzers.atmosphere_analyzer import AtmosphereAnalyzer
from analyzers.plot_extractor import PlotExtractor
from humanizer.pipeline import HumanizationPipeline
from humanizer.metrics import PerplexityBurstinessEstimator

logger = logging.getLogger("python-agent")


# ── Pydantic request models (must be defined here so FastAPI can resolve them) ─
class AnalyzeRequest(BaseModel):
    file_path: str
    material_id: str
    project_id: str

class EmbedRequest(BaseModel):
    text: str

class HumanizeRequest(BaseModel):
    text: str
    style_fingerprint: Optional[dict] = None
    intensity: float = 0.7

class MetricsRequest(BaseModel):
    text: str


def get_db():
    import psycopg2, os
    return psycopg2.connect(
        host=os.getenv("DB_HOST", "127.0.0.1"),
        port=int(os.getenv("DB_PORT", "5432")),
        dbname=os.getenv("DB_NAME", "novelbuilder"),
        user=os.getenv("DB_USER", "novelbuilder"),
        password=os.getenv("DB_PASSWORD", "novelbuilder"),
        options="-c client_encoding=UTF8",
    )


def get_qdrant():
    from vector_store.qdrant_store import QdrantStore
    return QdrantStore.get_instance()


style_analyzer = StyleAnalyzer()
narrative_analyzer = NarrativeAnalyzer()
atmosphere_analyzer = AtmosphereAnalyzer()
plot_extractor = PlotExtractor()
humanizer = HumanizationPipeline()
metrics_estimator = PerplexityBurstinessEstimator()


# ── Sensory words ─────────────────────────────────────────────────────────────
_SENSORY_WORDS = [
    "看到","望见","瞥见","注视","凝望","金色","银色","血红","漆黑",
    "光芒","阴影","闪烁","朦胧","苍白","翠绿",
    "听到","听见","声音","响声","回声","轰鸣","低语","呢喃","咆哮",
    "寂静","沉默","风声","雨声","心跳声",
    "闻到","嗅到","气味","芳香","恶臭","清香","花香","血腥",
    "触摸","抚摸","感觉","冰冷","温热","滚烫","粗糙","光滑",
    "柔软","刺痛","颤抖","瑟瑟",
    "尝到","品尝","味道","甜","苦","酸","辣",
]

def _extract_style_samples(sentences: list, max_samples: int = 20) -> list:
    candidates = [s for s in sentences if 15 <= len(s) <= 120]
    if len(candidates) <= max_samples:
        return candidates
    step = max(len(candidates) // max_samples, 1)
    return candidates[::step][:max_samples]

def _extract_sensory_samples(sentences: list, max_samples: int = 15) -> list:
    scored = []
    for sent in sentences:
        if len(sent) < 10:
            continue
        score = sum(1 for w in _SENSORY_WORDS if w in sent)
        if score > 0:
            scored.append((score, sent))
    scored.sort(key=lambda x: x[0], reverse=True)
    return [s for _, s in scored[:max_samples]]


def _repair_json(raw: str) -> dict:
    """Strip markdown fences and attempt basic repair of truncated/malformed LLM JSON output."""
    original_raw = raw
    raw = raw.strip()
    if raw.startswith("```"):
        raw = re.sub(r"^```[a-z]*\n?", "", raw)
        raw = re.sub(r"\n?```$", "", raw)
    raw = raw.strip()
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        # Try to close unclosed braces/brackets from truncated output
        open_b = raw.count("{") - raw.count("}")
        open_sq = raw.count("[") - raw.count("]")
        if open_sq > 0:
            raw += "]" * open_sq
        if open_b > 0:
            raw += "}" * open_b
        try:
            return json.loads(raw)
        except Exception:
            logger.warning(
                "JSON repair failed, returning empty dict | raw_content: %.500s",
                original_raw,
            )
            return {}

def _read_file(file_path: str) -> str:
    if not os.path.exists(file_path):
        return ""
    ext = os.path.splitext(file_path)[1].lower()
    if ext == ".pdf":
        try:
            from pdfminer.high_level import extract_text
            return extract_text(file_path)
        except Exception as e:
            logger.error("PDF extraction failed: %s", repr(e), exc_info=True)
            return ""
    elif ext in (".txt", ".md", ".text"):
        with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
            return f.read()
    elif ext == ".epub":
        try:
            import zipfile
            from xml.etree import ElementTree
            text_parts = []
            with zipfile.ZipFile(file_path, "r") as z:
                for name in z.namelist():
                    if name.endswith((".xhtml", ".html", ".htm")):
                        with z.open(name) as f2:
                            tree = ElementTree.parse(f2)
                            for elem in tree.iter():
                                if elem.text:
                                    text_parts.append(elem.text)
                                if elem.tail:
                                    text_parts.append(elem.tail)
            return "\n".join(text_parts)
        except Exception as e:
            logger.error("EPUB extraction failed: %s", repr(e), exc_info=True)
            return ""
    else:
        try:
            with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
                return f.read()
        except Exception:
            return ""

router = APIRouter()

# LEGACY ANALYSIS ENDPOINTS (kept for backward compatibility)
# ═══════════════════════════════════════════════════════════════════════════════

@router.post("/analyze")
async def analyze_reference(req: AnalyzeRequest, background_tasks: BackgroundTasks):
    text = _read_file(req.file_path)
    if not text:
        raise HTTPException(status_code=400, detail="无法读取文件")

    sentences = [s.strip() for s in re.split(r'[。！？\n]+', text) if s.strip()]
    style_result = style_analyzer.analyze(text)
    narrative_result = narrative_analyzer.analyze(text)
    atmosphere_result = atmosphere_analyzer.analyze(text)
    plot_result = plot_extractor.extract(text)
    style_samples = _extract_style_samples(sentences)
    sensory_samples = _extract_sensory_samples(sentences)

    conn = get_db()
    try:
        with conn.cursor() as cur:
            cur.execute("""
                UPDATE reference_materials
                SET style_layer = %s, narrative_layer = %s, atmosphere_layer = %s,
                    sample_texts = %s, status = 'completed'
                WHERE id = %s
            """, (
                json.dumps(style_result, ensure_ascii=False),
                json.dumps(narrative_result, ensure_ascii=False),
                json.dumps(atmosphere_result, ensure_ascii=False),
                json.dumps({"style": style_samples, "sensory": sensory_samples}, ensure_ascii=False),
                req.material_id,
            ))
            for element in plot_result.get("elements", []):
                cur.execute("""
                    INSERT INTO quarantine_zone.plot_elements
                    (id, material_id, element_type, content)
                    VALUES (gen_random_uuid(), %s, %s, %s)
                    ON CONFLICT DO NOTHING
                """, (
                    req.material_id,
                    element.get("type", "unknown"),
                    json.dumps(element.get("content", {}), ensure_ascii=False),
                ))
            conn.commit()
    finally:
        conn.close()

    # Async: push style samples into Qdrant
    async def _push_to_qdrant():
        store = get_qdrant()
        items = [{"collection": "style_samples", "content": s,
                  "metadata": {"material_id": req.material_id, "project_id": req.project_id}}
                 for s in style_samples if s]
        await store.upsert_batch(req.project_id, "style_samples", items)

    background_tasks.add_task(_push_to_qdrant)

    return {
        "style_layer": style_result,
        "narrative_layer": narrative_result,
        "atmosphere_layer": atmosphere_result,
        "plot_elements_count": len(plot_result.get("elements", [])),
        "style_samples": style_samples,
        "sensory_samples": sensory_samples,
        "status": "completed",
    }


@router.post("/style-fingerprint")
async def extract_style(req: EmbedRequest):
    result = style_analyzer.analyze(req.text)
    return {"fingerprint": result}


@router.post("/humanize")
async def humanize_text(req: HumanizeRequest):
    result = humanizer.process(req.text, req.style_fingerprint, req.intensity)
    return {"result": result}


@router.post("/metrics")
async def metrics_flat(req: MetricsRequest):
    """
    Flat /metrics endpoint consumed by the Go originality_service.
    Returns perplexity, burstiness, ai_probability, verdict at top level.
    """
    result = metrics_estimator.estimate(req.text)
    return {
        "perplexity": result.get("perplexity_estimate", 0.0),
        "burstiness": result.get("burstiness", 0.0),
        "ai_probability": result.get("ai_probability", 0.0),
        "verdict": result.get("verdict", "uncertain"),
    }


@router.post("/metrics/perplexity-burstiness")
async def estimate_metrics(req: MetricsRequest):
    result = metrics_estimator.estimate(req.text)
    return {"metrics": result}


# ═══════════════════════════════════════════════════════════════════════════════
