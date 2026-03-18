"""
AI小说生成平台 - Python Sidecar
负责参考书四层分析、风格指纹提取、人性化管线、困惑度/突发度估计
"""
import os
import json
import math
import re
import logging
from typing import Optional
from contextlib import asynccontextmanager

import httpx
import psycopg2
import psycopg2.extras
from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel

from analyzers.style_analyzer import StyleAnalyzer
from analyzers.narrative_analyzer import NarrativeAnalyzer
from analyzers.atmosphere_analyzer import AtmosphereAnalyzer
from analyzers.plot_extractor import PlotExtractor
from humanizer.pipeline import HumanizationPipeline
from humanizer.metrics import PerplexityBurstinessEstimator

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("python-sidecar")

# Database connection
def get_db():
    return psycopg2.connect(
        host=os.getenv("DB_HOST", "localhost"),
        port=int(os.getenv("DB_PORT", "5432")),
        dbname=os.getenv("DB_NAME", "novelbuilder"),
        user=os.getenv("DB_USER", "novelbuilder"),
        password=os.getenv("DB_PASSWORD", "novelbuilder"),
    )

@asynccontextmanager
async def lifespan(app: FastAPI):
    logger.info("Python sidecar starting up...")
    yield
    logger.info("Python sidecar shutting down...")

app = FastAPI(title="NovelBuilder Python Sidecar", version="1.0.0", lifespan=lifespan)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)

# Initialize analyzers
style_analyzer = StyleAnalyzer()
narrative_analyzer = NarrativeAnalyzer()
atmosphere_analyzer = AtmosphereAnalyzer()
plot_extractor = PlotExtractor()
humanizer = HumanizationPipeline()
metrics_estimator = PerplexityBurstinessEstimator()


class AnalyzeRequest(BaseModel):
    file_path: str
    material_id: str
    project_id: str


class HumanizeRequest(BaseModel):
    text: str
    style_fingerprint: Optional[dict] = None
    intensity: float = 0.7


class MetricsRequest(BaseModel):
    text: str


class StyleFingerprint(BaseModel):
    text: str


class EmbedRequest(BaseModel):
    text: str


# ===== Sensory keyword list (used for sample extraction) =====
_SENSORY_WORDS = [
    "看到", "望见", "瞥见", "注视", "凝望", "金色", "银色", "血红", "漆黑",
    "光芒", "阴影", "闪烁", "朦胧", "苍白", "翠绿",
    "听到", "听见", "声音", "响声", "回声", "轰鸣", "低语", "呢喃", "咆哮",
    "寂静", "沉默", "风声", "雨声", "心跳声",
    "闻到", "嗅到", "气味", "芳香", "恶臭", "清香", "花香", "血腥",
    "触摸", "抚摸", "感觉", "冰冷", "温热", "滚烫", "粗糙", "光滑",
    "柔软", "刺痛", "颤抖", "瑟瑟",
    "尝到", "品尝", "味道", "甜", "苦", "酸", "辣",
]


def _extract_style_samples(sentences: list, max_samples: int = 20) -> list:
    """Return evenly-distributed representative sentences (15–120 chars)."""
    candidates = [s for s in sentences if 15 <= len(s) <= 120]
    if len(candidates) <= max_samples:
        return candidates
    step = max(len(candidates) // max_samples, 1)
    return candidates[::step][:max_samples]


def _extract_sensory_samples(sentences: list, max_samples: int = 15) -> list:
    """Return sentences with the richest sensory language."""
    scored = []
    for sent in sentences:
        if len(sent) < 10:
            continue
        score = sum(1 for w in _SENSORY_WORDS if w in sent)
        if score > 0:
            scored.append((score, sent))
    scored.sort(key=lambda x: x[0], reverse=True)
    return [s for _, s in scored[:max_samples]]


# ===== Health =====
@app.get("/health")
async def health():
    return {"status": "ok", "service": "python-sidecar"}


# ===== 向量嵌入 =====
@app.post("/embed")
async def embed_text(req: EmbedRequest):
    """
    Generate a 1024-dim embedding vector using an OpenAI-compatible embeddings API.
    Configure via env vars:
      EMBEDDING_BASE_URL  (default: OPENAI_BASE_URL or https://api.openai.com/v1)
      EMBEDDING_API_KEY   (default: OPENAI_API_KEY)
      EMBEDDING_MODEL     (default: text-embedding-3-small)
    """
    base_url = (
        os.getenv("EMBEDDING_BASE_URL")
        or os.getenv("OPENAI_BASE_URL")
        or "https://api.openai.com/v1"
    ).rstrip("/")
    api_key = os.getenv("EMBEDDING_API_KEY") or os.getenv("OPENAI_API_KEY", "")
    model = os.getenv("EMBEDDING_MODEL", "text-embedding-3-small")

    if not api_key:
        raise HTTPException(
            status_code=503,
            detail="Embedding API not configured. Set EMBEDDING_API_KEY env var.",
        )

    # Truncate to safe token length (~8 k chars covers most 8192-token limits)
    text = req.text[:8000]

    payload: dict = {"input": text, "model": model}
    # text-embedding-3-* accepts a dimensions parameter (reduces output dims)
    if "text-embedding-3" in model:
        dims = int(os.getenv("EMBEDDING_DIMENSIONS", "1024"))
        payload["dimensions"] = dims

    try:
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                f"{base_url}/embeddings",
                headers={
                    "Authorization": f"Bearer {api_key}",
                    "Content-Type": "application/json",
                },
                json=payload,
            )
            resp.raise_for_status()
            data = resp.json()
            embedding = data["data"][0]["embedding"]
            return {
                "embedding": embedding,
                "model": model,
                "dimensions": len(embedding),
            }
    except httpx.HTTPStatusError as exc:
        logger.error(f"Embedding API HTTP error {exc.response.status_code}: {exc.response.text}")
        raise HTTPException(status_code=502, detail=f"Embedding API error: {exc.response.status_code}")
    except Exception as exc:
        logger.error(f"Embedding request failed: {exc}")
        raise HTTPException(status_code=502, detail=str(exc))


# ===== 四层参考书分析 =====
@app.post("/analyze")
async def analyze_reference(req: AnalyzeRequest):
    """
    四层参考书分析:
    Layer 1: 风格指纹层 (jieba分词统计, 句长分布, 标点频率)
    Layer 2: 叙事结构层 (POV类型, 时间线模式, 场景节奏)
    Layer 3: 氛围萃取层 (情绪基调, 感官描写频率, 环境意象库)
    Layer 4: 情节元素提取 -> 隔离区 (quarantine_zone)
    """
    # Read the file
    text = _read_file(req.file_path)
    if not text:
        raise HTTPException(status_code=400, detail="无法读取文件")

    # Split sentences once for reuse across layers and sample extraction
    import re as _re
    sentences = [s.strip() for s in _re.split(r'[。！？\n]+', text) if s.strip()]

    # Layer 1: Style fingerprint
    style_result = style_analyzer.analyze(text)

    # Layer 2: Narrative structure
    narrative_result = narrative_analyzer.analyze(text)

    # Layer 3: Atmosphere extraction
    atmosphere_result = atmosphere_analyzer.analyze(text)

    # Layer 4: Plot element extraction -> quarantine zone
    plot_result = plot_extractor.extract(text)

    # Extract text samples for RAG vector store
    style_samples = _extract_style_samples(sentences)
    sensory_samples = _extract_sensory_samples(sentences)

    # Save to database
    conn = get_db()
    try:
        with conn.cursor() as cur:
            # Update reference material with analysis results
            cur.execute("""
                UPDATE reference_materials
                SET style_fingerprint = %s,
                    narrative_structure = %s,
                    atmosphere_profile = %s,
                    analysis_status = 'completed',
                    updated_at = NOW()
                WHERE id = %s
            """, (
                json.dumps(style_result, ensure_ascii=False),
                json.dumps(narrative_result, ensure_ascii=False),
                json.dumps(atmosphere_result, ensure_ascii=False),
                req.material_id,
            ))

            # Save plot elements to quarantine zone
            for element in plot_result.get("elements", []):
                cur.execute("""
                    INSERT INTO quarantine_zone.plot_elements
                    (material_id, element_type, content, similarity_hash, is_locked)
                    VALUES (%s, %s, %s, %s, true)
                    ON CONFLICT DO NOTHING
                """, (
                    req.material_id,
                    element.get("type", "unknown"),
                    json.dumps(element.get("content", {}), ensure_ascii=False),
                    element.get("hash", ""),
                ))

            conn.commit()
    finally:
        conn.close()

    return {
        "style_layer": style_result,
        "narrative_layer": narrative_result,
        "atmosphere_layer": atmosphere_result,
        "plot_elements_count": len(plot_result.get("elements", [])),
        "style_samples": style_samples,
        "sensory_samples": sensory_samples,
        "status": "completed",
    }


# ===== 风格指纹提取 =====
@app.post("/style-fingerprint")
async def extract_style(req: StyleFingerprint):
    """提取文本的风格指纹"""
    result = style_analyzer.analyze(req.text)
    return {"fingerprint": result}


# ===== 人性化管线 =====
@app.post("/humanize")
async def humanize_text(req: HumanizeRequest):
    """
    8步人性化管线:
    Step 1: 逻辑指纹打断 (Logic Fingerprint Breaking)
    Step 2: 主语省略 (Subject Omission)
    Step 3: 对话压缩 (Dialogue Compression)
    Step 4: 情感替    换 (Emotion Replacement)
    Step 5: 感官注    入 (Sensory Injection)
    Step 6: 自由间接引语 (Free Indirect Discourse)
    Step 7: 突	发度优化 (Burstiness Optimization)
    Step 8: 叙事顺序检查 (Narrative Sequence Check)
    """
    result = humanizer.process(req.text, req.style_fingerprint, req.intensity)
    return {"result": result}


# ===== 困惑度/突发度指标 =====
@app.post("/metrics/perplexity-burstiness")
async def estimate_metrics(req: MetricsRequest):
    """估计文本的困惑度和突发度，用于检测AI味"""
    result = metrics_estimator.estimate(req.text)
    return {"metrics": result}


def _read_file(file_path: str) -> str:
    """读取各种格式的参考书文件"""
    if not os.path.exists(file_path):
        return ""

    ext = os.path.splitext(file_path)[1].lower()

    if ext == ".pdf":
        try:
            from pdfminer.high_level import extract_text
            return extract_text(file_path)
        except Exception as e:
            logger.error(f"PDF extraction failed: {e}")
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
                        with z.open(name) as f:
                            tree = ElementTree.parse(f)
                            for elem in tree.iter():
                                if elem.text:
                                    text_parts.append(elem.text)
                                if elem.tail:
                                    text_parts.append(elem.tail)
            return "\n".join(text_parts)
        except Exception as e:
            logger.error(f"EPUB extraction failed: {e}")
            return ""
    else:
        try:
            with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
                return f.read()
        except Exception:
            return ""


if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("SIDECAR_PORT", "8081"))
    uvicorn.run(app, host="0.0.0.0", port=port)
