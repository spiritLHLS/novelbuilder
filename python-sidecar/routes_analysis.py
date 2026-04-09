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

from json_repair import repair_json

logger = logging.getLogger("python-agent")


# ── Pydantic request models ──────────────────────────────────────────────────
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
    """Get a connection from the shared pool in main.py."""
    from main import get_db as _get_db
    return _get_db()

def put_db(conn):
    from main import put_db as _put_db
    _put_db(conn)


def get_qdrant():
    from vector_store.qdrant_store import QdrantStore
    return QdrantStore.get_instance()


def _get_analyzers():
    """Lazy-import analyzer singletons from main to avoid duplicate instantiation."""
    from main import (style_analyzer, narrative_analyzer, atmosphere_analyzer,
                      plot_extractor, humanizer, metrics_estimator)
    return style_analyzer, narrative_analyzer, atmosphere_analyzer, plot_extractor, humanizer, metrics_estimator


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

def _extract_style_samples(sentences: list, max_samples: int = 100) -> list:
    """Extract style samples evenly spaced from candidate sentences.

    The default cap is 100 (raised from 20) so that a reference book analysed
    once can cover projects with up to 100 chapters without stale extracts.
    At rebuild time the Go service re-samples from full chapter text according
    to the actual chapter count, so this value is a safe upper bound.
    """
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
    """Strip markdown fences and attempt basic repair of truncated/malformed LLM JSON output.
    
    Uses centralized repair logic with multiple strategies.
    """
    result = repair_json(raw)
    if not result:
        logger.warning(
            "JSON repair failed, returning empty dict | raw_content: %.800s",
            raw,
        )
    return result


_ALLOWED_UPLOAD_DIR = os.path.abspath(os.getenv("UPLOAD_DIR", "/app/uploads"))

def _read_file(file_path: str) -> str:
    abs_path = os.path.abspath(file_path)
    if not abs_path.startswith(_ALLOWED_UPLOAD_DIR):
        logger.warning("Path traversal blocked: %s", file_path)
        return ""
    if not os.path.exists(abs_path):
        return ""
    ext = os.path.splitext(abs_path)[1].lower()
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
    sa, na, aa, pe, _, _ = _get_analyzers()
    style_result = sa.analyze(text)
    narrative_result = na.analyze(text)
    atmosphere_result = aa.analyze(text)
    plot_result = pe.extract(text)
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
        conn.commit()
    finally:
        put_db(conn)

    # Best-effort: insert plot elements into quarantine_zone (separate transaction,
    # so a permission failure does NOT roll back the main analysis results above)
    if plot_result.get("elements"):
        qconn = get_db()
        try:
            with qconn.cursor() as qcur:
                for element in plot_result["elements"]:
                    qcur.execute("""
                        INSERT INTO quarantine_zone.plot_elements
                        (id, material_id, element_type, content)
                        VALUES (gen_random_uuid(), %s, %s, %s)
                        ON CONFLICT DO NOTHING
                    """, (
                        req.material_id,
                        element.get("type", "unknown"),
                        json.dumps(element.get("content", {}), ensure_ascii=False),
                    ))
            qconn.commit()
        except Exception as qe:
            logger.warning("quarantine_zone insert skipped (check GRANT USAGE on quarantine_zone): %s", repr(qe))
            qconn.rollback()
        finally:
            put_db(qconn)

    # Async: push style samples into Qdrant
    async def _push_to_qdrant():
        try:
            store = get_qdrant()
            items = [{"collection": "style_samples", "content": s,
                      "metadata": {"material_id": req.material_id, "project_id": req.project_id}}
                     for s in style_samples if s]
            await store.upsert_batch(req.project_id, "style_samples", items)
        except Exception as e:
            logger.warning("Qdrant style push failed (non-fatal): %s", repr(e))

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
    sa, _, _, _, _, _ = _get_analyzers()
    result = sa.analyze(req.text)
    return {"fingerprint": result}


@router.post("/humanize")
async def humanize_text(req: HumanizeRequest):
    _, _, _, _, h, _ = _get_analyzers()
    result = h.process(req.text, req.style_fingerprint, req.intensity)
    return {"result": result}


@router.post("/metrics")
async def metrics_flat(req: MetricsRequest):
    """
    Flat /metrics endpoint consumed by the Go originality_service.
    Returns perplexity, burstiness, ai_probability, verdict at top level.
    """
    _, _, _, _, _, me = _get_analyzers()
    result = me.estimate(req.text)
    return {
        "perplexity": result.get("perplexity_estimate", 0.0),
        "burstiness": result.get("burstiness", 0.0),
        "ai_probability": result.get("ai_probability", 0.0),
        "verdict": result.get("verdict", "uncertain"),
    }


@router.post("/metrics/perplexity-burstiness")
async def estimate_metrics(req: MetricsRequest):
    _, _, _, _, _, me = _get_analyzers()
    result = me.estimate(req.text)
    return {"metrics": result}


# ═══════════════════════════════════════════════════════════════════════════════
