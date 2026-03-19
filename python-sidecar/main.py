"""
NovelBuilder Python Agent Service
Handles: LangGraph agent, Neo4j graph ops, Qdrant vector ops,
         reference analysis, humanization pipeline, metrics.
"""
import asyncio
import json
import logging
import math
import os
import re
import uuid
from contextlib import asynccontextmanager
from typing import Optional

import httpx
import psycopg2
import psycopg2.extras
from fastapi import FastAPI, HTTPException, BackgroundTasks
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import StreamingResponse
from pydantic import BaseModel

from analyzers.style_analyzer import StyleAnalyzer
from analyzers.narrative_analyzer import NarrativeAnalyzer
from analyzers.atmosphere_analyzer import AtmosphereAnalyzer
from analyzers.plot_extractor import PlotExtractor
from humanizer.pipeline import HumanizationPipeline
from humanizer.metrics import PerplexityBurstinessEstimator

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("python-agent")

# ── DB connection (for legacy analyze endpoint) ───────────────────────────────
def get_db():
    return psycopg2.connect(
        host=os.getenv("DB_HOST", "127.0.0.1"),
        port=int(os.getenv("DB_PORT", "5432")),
        dbname=os.getenv("DB_NAME", "novelbuilder"),
        user=os.getenv("DB_USER", "novelbuilder"),
        password=os.getenv("DB_PASSWORD", "novelbuilder"),
    )

# ── Lazy-init singletons ──────────────────────────────────────────────────────
_neo4j_client = None
_qdrant_store = None

def get_neo4j():
    global _neo4j_client
    if _neo4j_client is None:
        from graph_store.neo4j_client import Neo4jClient
        _neo4j_client = Neo4jClient.get_instance()
    return _neo4j_client

def get_qdrant():
    global _qdrant_store
    if _qdrant_store is None:
        from vector_store.qdrant_store import QdrantStore
        _qdrant_store = QdrantStore.get_instance()
    return _qdrant_store

# ── In-memory agent session store ─────────────────────────────────────────────
# For production use Redis; this suffices for single-container deployment.
_agent_sessions: dict[str, dict] = {}
_SESSION_TTL_SECONDS = 3600  # 1 hour


def _cleanup_expired_sessions() -> None:
    """Remove completed/failed sessions older than TTL to prevent memory leak."""
    import time
    now = time.time()
    to_delete = [
        sid for sid, s in list(_agent_sessions.items())
        if s.get("status") in ("done", "error")
        and now - s.get("_created_at", now) > _SESSION_TTL_SECONDS
    ]
    for sid in to_delete:
        _agent_sessions.pop(sid, None)

@asynccontextmanager
async def lifespan(app: FastAPI):
    logger.info("Agent service starting up...")
    # Warm up Neo4j schema
    try:
        neo4j = get_neo4j()
        await neo4j.ensure_schema()
        logger.info("Neo4j schema ensured")
    except Exception as exc:
        logger.warning("Neo4j schema init failed (may retry): %s", exc)
    yield
    logger.info("Agent service shutting down...")

app = FastAPI(title="NovelBuilder Agent Service", version="2.0.0", lifespan=lifespan)
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)

# Analyzers (legacy)
style_analyzer = StyleAnalyzer()
narrative_analyzer = NarrativeAnalyzer()
atmosphere_analyzer = AtmosphereAnalyzer()
plot_extractor = PlotExtractor()
humanizer = HumanizationPipeline()
metrics_estimator = PerplexityBurstinessEstimator()

# ── Pydantic models ───────────────────────────────────────────────────────────
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

class EmbedRequest(BaseModel):
    text: str

class AgentRunRequest(BaseModel):
    project_id: str
    task_type: str = "generate_chapter"
    user_prompt: str = ""
    chapter_num: Optional[int] = None
    outline_hint: Optional[str] = None
    style_profile: Optional[dict] = None
    llm_config: dict = {}
    max_retries: int = 2

class GraphUpsertRequest(BaseModel):
    project_id: str
    entity_type: str   # Character | Rule | Foreshadowing | Event
    entity_id: str
    name: str
    properties: dict = {}
    relations: list[dict] = []  # [{target_id, target_name, rel_type, description}]

class GraphQueryRequest(BaseModel):
    cypher: str
    params: dict = {}

class VectorUpsertRequest(BaseModel):
    project_id: str
    collection: str
    content: str
    metadata: dict = {}
    point_id: Optional[str] = None

class VectorSearchRequest(BaseModel):
    project_id: str
    collection: str
    query: str
    limit: int = 5

class VectorRebuildRequest(BaseModel):
    project_id: str
    items: list[dict]   # [{collection, content, metadata}]

class VectorDeleteBySourceRequest(BaseModel):
    project_id: str
    source_id: str

# ── New feature request models ────────────────────────────────────────────────

class AuditChapterRequest(BaseModel):
    chapter_id: str
    project_id: str
    chapter_text: str
    chapter_num: int = 1
    context: dict = {}  # outline_hint, characters, previous_summaries, foreshadowings, book_rules
    llm_config: dict = {}

class AntiDetectRequest(BaseModel):
    chapter_id: str
    text: str
    intensity: str = "medium"  # light | medium | heavy
    style_guide: str = ""
    anti_ai_wordlist: list[str] = []
    banned_patterns: list[str] = []
    llm_config: dict = {}

class CreativeBriefRequest(BaseModel):
    brief_text: str
    genre: str = "现代都市"
    llm_config: dict = {}

class ImportChaptersRequest(BaseModel):
    project_id: str
    import_id: str
    source_text: str
    split_pattern: str = r"第.{1,4}[章节回]"
    fanfic_mode: Optional[str] = None  # canon|au|ooc|cp
    llm_config: dict = {}

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

def _read_file(file_path: str) -> str:
    if not os.path.exists(file_path):
        return ""
    ext = os.path.splitext(file_path)[1].lower()
    if ext == ".pdf":
        try:
            from pdfminer.high_level import extract_text
            return extract_text(file_path)
        except Exception as e:
            logger.error("PDF extraction failed: %s", e)
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
            logger.error("EPUB extraction failed: %s", e)
            return ""
    else:
        try:
            with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
                return f.read()
        except Exception:
            return ""

# ═══════════════════════════════════════════════════════════════════════════════
# HEALTH
# ═══════════════════════════════════════════════════════════════════════════════
@app.get("/health")
async def health():
    return {"status": "ok", "service": "novelbuilder-agent"}

# ═══════════════════════════════════════════════════════════════════════════════
# AGENT ENDPOINTS
# ═══════════════════════════════════════════════════════════════════════════════

@app.post("/agent/run")
async def agent_run(req: AgentRunRequest, background_tasks: BackgroundTasks):
    """
    Start a LangGraph agent session asynchronously.
    Returns immediately with session_id; client polls /agent/status/{sid}.
    """
    import time
    _cleanup_expired_sessions()
    session_id = str(uuid.uuid4())
    _agent_sessions[session_id] = {
        "status": "running",
        "progress": [],
        "result": None,
        "error": None,
        "_created_at": time.time(),
    }

    async def _run():
        try:
            from agent.graph import run_agent
            from agent.state import AgentState

            initial: AgentState = {
                "project_id": req.project_id,
                "session_id": session_id,
                "task_type": req.task_type,
                "user_prompt": req.user_prompt,
                "chapter_num": req.chapter_num,
                "outline_hint": req.outline_hint,
                "style_profile": req.style_profile,
                "llm_config": req.llm_config,
                "max_retries": req.max_retries,
                "messages": [],
                "retry_count": 0,
                "done": False,
            }

            final = await run_agent(initial)

            _agent_sessions[session_id]["status"] = "done"
            _agent_sessions[session_id]["result"] = {
                "final_text": final.get("final_text", final.get("draft", "")),
                "chapter_summary": final.get("chapter_summary", ""),
                "quality_score": final.get("quality_score", 0.0),
                "quality_issues": final.get("quality_issues", []),
            }
        except Exception as exc:
            logger.error("Agent session %s failed: %s", session_id, exc, exc_info=True)
            _agent_sessions[session_id]["status"] = "error"
            _agent_sessions[session_id]["error"] = str(exc)

    background_tasks.add_task(_run)
    return {"session_id": session_id, "status": "running"}


@app.get("/agent/status/{session_id}")
async def agent_status(session_id: str):
    """Poll agent session status."""
    session = _agent_sessions.get(session_id)
    if session is None:
        raise HTTPException(status_code=404, detail="Session not found")
    return session


@app.get("/agent/stream/{session_id}")
async def agent_stream(session_id: str):
    """SSE stream for agent progress updates."""
    async def event_gen():
        import asyncio
        prev_len = 0
        for _ in range(300):  # max 5 minutes at 1s intervals
            session = _agent_sessions.get(session_id, {})
            progress = session.get("progress", [])
            if len(progress) > prev_len:
                for msg in progress[prev_len:]:
                    yield f"data: {json.dumps(msg, ensure_ascii=False)}\n\n"
                prev_len = len(progress)
            if session.get("status") in ("done", "error"):
                final = {"status": session["status"],
                         "result": session.get("result"),
                         "error": session.get("error")}
                yield f"data: {json.dumps(final, ensure_ascii=False)}\n\n"
                break
            await asyncio.sleep(1)

    return StreamingResponse(event_gen(), media_type="text/event-stream",
                             headers={"Cache-Control": "no-cache",
                                      "X-Accel-Buffering": "no"})

# ═══════════════════════════════════════════════════════════════════════════════
# GRAPH (Neo4j) ENDPOINTS
# ═══════════════════════════════════════════════════════════════════════════════

@app.get("/graph/entities/{project_id}")
async def graph_entities(project_id: str):
    """Return full project knowledge graph (nodes + edges)."""
    neo4j = get_neo4j()
    data = await neo4j.get_project_graph(project_id)
    return data


@app.post("/graph/query")
async def graph_query(req: GraphQueryRequest):
    """Execute a raw Cypher read query."""
    neo4j = get_neo4j()
    # Only allow read queries for safety
    if any(kw in req.cypher.upper() for kw in ("DELETE", "DROP", "DETACH", "CREATE", "MERGE", "SET")):
        raise HTTPException(status_code=400, detail="Only read queries allowed via this endpoint")
    results = await neo4j.query(req.cypher, req.params)
    return {"results": results}


@app.post("/graph/upsert")
async def graph_upsert(req: GraphUpsertRequest):
    """
    Upsert an entity into Neo4j.
    entity_type: Character | Rule | Foreshadowing | Event
    """
    neo4j = get_neo4j()

    if req.entity_type == "Character":
        await neo4j.upsert_character(
            project_id=req.project_id,
            char_id=req.entity_id,
            name=req.name,
            role_type=req.properties.get("role_type", "supporting"),
            core_traits=req.properties.get("core_traits", ""),
        )
        for rel in req.relations:
            await neo4j.upsert_character_relation(
                from_id=req.entity_id,
                to_id=rel["target_id"],
                rel_type=rel.get("rel_type", "RELATES_TO"),
                description=rel.get("description", ""),
            )
    elif req.entity_type == "Rule":
        await neo4j.upsert_rule(
            project_id=req.project_id,
            rule_id=req.entity_id,
            content=req.properties.get("content", req.name),
            immutable=req.properties.get("immutable", True),
            priority=req.properties.get("priority", 5),
        )
    elif req.entity_type == "Foreshadowing":
        await neo4j.upsert_foreshadowing(
            project_id=req.project_id,
            fs_id=req.entity_id,
            content=req.properties.get("content", req.name),
            status=req.properties.get("status", "active"),
            priority=req.properties.get("priority", 3),
        )
    else:
        raise HTTPException(status_code=400, detail=f"Unknown entity_type: {req.entity_type}")

    return {"ok": True, "entity_id": req.entity_id}


@app.post("/graph/sync-project/{project_id}")
async def graph_sync_project(project_id: str):
    """
    Sync characters, rules, and foreshadowings from PostgreSQL into Neo4j.
    Called after project creation / major update. Prevents N+1 by loading
    all entities in a single DB query each.
    """
    neo4j = get_neo4j()
    conn = get_db()
    synced = {"characters": 0, "rules": 0, "foreshadowings": 0}

    try:
        cur = conn.cursor(cursor_factory=psycopg2.extras.DictCursor)

        # Upsert project node
        cur.execute("SELECT title, genre FROM projects WHERE id = %s", (project_id,))
        row = cur.fetchone()
        if row:
            await neo4j.upsert_project(project_id, row["title"], row["genre"] or "")

        # Batch load all characters (single query, no N+1)
        cur.execute("""
            SELECT id, name, role_type, profile->>'core_traits' AS core_traits
            FROM characters WHERE project_id = %s
        """, (project_id,))
        chars = cur.fetchall()
        for c in chars:
            await neo4j.upsert_character(
                project_id=project_id,
                char_id=str(c["id"]),
                name=c["name"],
                role_type=c["role_type"] or "supporting",
                core_traits=c["core_traits"] or "",
            )
            synced["characters"] += 1

        # Batch load all foreshadowings (single query)
        cur.execute("""
            SELECT id, content, status, priority
            FROM foreshadowings WHERE project_id = %s
        """, (project_id,))
        for f in cur.fetchall():
            await neo4j.upsert_foreshadowing(
                project_id=project_id,
                fs_id=str(f["id"]),
                content=f["content"],
                status=f["status"],
                priority=int(f["priority"]),
            )
            synced["foreshadowings"] += 1

        # Batch load world constitution rules (single query)
        cur.execute("""
            SELECT jsonb_array_elements_text(immutable_rules) AS rule
            FROM world_bible_constitutions WHERE project_id = %s LIMIT 1
        """, (project_id,))
        for i, r in enumerate(cur.fetchall()):
            rule_id = f"{project_id}:rule:imm:{i}"
            await neo4j.upsert_rule(
                project_id=project_id,
                rule_id=rule_id,
                content=r["rule"],
                immutable=True,
                priority=10 - i,
            )
            synced["rules"] += 1

        cur.execute("""
            SELECT jsonb_array_elements_text(mutable_rules) AS rule
            FROM world_bible_constitutions WHERE project_id = %s LIMIT 1
        """, (project_id,))
        for i, r in enumerate(cur.fetchall()):
            rule_id = f"{project_id}:rule:mut:{i}"
            await neo4j.upsert_rule(
                project_id=project_id,
                rule_id=rule_id,
                content=r["rule"],
                immutable=False,
                priority=5 - i,
            )
            synced["rules"] += 1

    finally:
        conn.close()

    return {"ok": True, "synced": synced}

# ═══════════════════════════════════════════════════════════════════════════════
# VECTOR (Qdrant) ENDPOINTS
# ═══════════════════════════════════════════════════════════════════════════════

@app.post("/vector/upsert")
async def vector_upsert(req: VectorUpsertRequest):
    store = get_qdrant()
    pid = await store.upsert(
        project_id=req.project_id,
        collection=req.collection,
        content=req.content,
        metadata=req.metadata,
        point_id=req.point_id,
    )
    return {"ok": True, "point_id": pid}


@app.post("/vector/search")
async def vector_search(req: VectorSearchRequest):
    store = get_qdrant()
    hits = await store.search(
        project_id=req.project_id,
        collection=req.collection,
        query=req.query,
        limit=req.limit,
    )
    return {"hits": hits}


@app.post("/vector/rebuild")
async def vector_rebuild(req: VectorRebuildRequest):
    """
    Batch-rebuild vector collections for a project.
    Accepts a list of {collection, content, metadata} items.
    All embeddings computed in a single concurrent batch (no N+1).
    """
    store = get_qdrant()
    # Group by collection
    by_collection: dict[str, list[dict]] = {}
    for item in req.items:
        col = item.get("collection", "world_knowledge")
        by_collection.setdefault(col, []).append(item)

    inserted = 0
    for col, items in by_collection.items():
        await store.upsert_batch(
            project_id=req.project_id,
            collection=col,
            items=items,
        )
        inserted += len(items)

    return {"ok": True, "inserted": inserted}


@app.get("/vector/status/{project_id}")
async def vector_status(project_id: str):
    store = get_qdrant()
    stats = await store.get_collection_stats(project_id)
    total = sum(s["count"] for s in stats)
    return {"project_id": project_id, "collections": stats, "total_chunks": total}


@app.post("/vector/delete-by-source")
async def vector_delete_by_source(req: VectorDeleteBySourceRequest):
    store = get_qdrant()
    await store.delete_by_source_id(req.project_id, req.source_id)
    return {"ok": True}


# ═══════════════════════════════════════════════════════════════════════════════
# EMBEDDING (legacy + new local via sentence-transformers)
# ═══════════════════════════════════════════════════════════════════════════════

@app.post("/embed")
async def embed_text(req: EmbedRequest):
    """
    Generate embedding. Tries local sentence-transformers first (faster, no API cost),
    falls back to OpenAI-compatible API.
    """
    from vector_store.qdrant_store import embed as local_embed
    vec = await asyncio.get_event_loop().run_in_executor(None, local_embed, req.text[:8000])
    if vec:
        return {"embedding": vec, "model": "local-sentence-transformer", "dimensions": len(vec)}

    # Fallback: remote API
    base_url = (
        os.getenv("EMBEDDING_BASE_URL") or
        os.getenv("OPENAI_BASE_URL") or
        "https://api.openai.com/v1"
    ).rstrip("/")
    api_key = os.getenv("EMBEDDING_API_KEY") or os.getenv("OPENAI_API_KEY", "")
    model = os.getenv("EMBEDDING_MODEL", "text-embedding-3-small")

    if not api_key:
        raise HTTPException(status_code=503, detail="No embedding service configured")

    payload: dict = {"input": req.text[:8000], "model": model}
    if "text-embedding-3" in model:
        payload["dimensions"] = int(os.getenv("EMBEDDING_DIMENSIONS", "1024"))

    async with httpx.AsyncClient(timeout=30.0) as client:
        resp = await client.post(
            f"{base_url}/embeddings",
            headers={"Authorization": f"Bearer {api_key}"},
            json=payload,
        )
        resp.raise_for_status()
        data = resp.json()
        embedding = data["data"][0]["embedding"]
    return {"embedding": embedding, "model": model, "dimensions": len(embedding)}


# ═══════════════════════════════════════════════════════════════════════════════
# LEGACY ANALYSIS ENDPOINTS (kept for backward compatibility)
# ═══════════════════════════════════════════════════════════════════════════════

@app.post("/analyze")
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


@app.post("/style-fingerprint")
async def extract_style(req: EmbedRequest):
    result = style_analyzer.analyze(req.text)
    return {"fingerprint": result}


@app.post("/humanize")
async def humanize_text(req: HumanizeRequest):
    result = humanizer.process(req.text, req.style_fingerprint, req.intensity)
    return {"result": result}


@app.post("/metrics")
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


@app.post("/metrics/perplexity-burstiness")
async def estimate_metrics(req: MetricsRequest):
    result = metrics_estimator.estimate(req.text)
    return {"metrics": result}


# ═══════════════════════════════════════════════════════════════════════════════
# 33-DIMENSION CHAPTER AUDIT
# ═══════════════════════════════════════════════════════════════════════════════

_AUDIT_SYSTEM = """你是一位专业的中文网络小说审核员，请对下面的章节草稿进行严格的多维度审计。

你必须严格按照以下33个维度逐一评分，每个维度给出0.0-1.0的分数和具体问题列表。
当问题列表为空时，passed=true；否则passed=false。

返回严格的JSON格式（不要输出其他内容）：
{
  "dimensions": {
    "character_memory": {"score": 0.9, "passed": true, "issues": []},
    "resource_continuity": {"score": 0.8, "passed": true, "issues": []},
    "foreshadowing_recovery": {"score": 0.7, "passed": false, "issues": ["伏笔X尚未回收"]},
    "outline_deviation": {...},
    "narrative_pace": {...},
    "emotional_arc": {...},
    "world_rule_compliance": {...},
    "timeline_consistency": {...},
    "pov_consistency": {...},
    "dialogue_naturalness": {...},
    "scene_description_quality": {...},
    "conflict_escalation": {...},
    "character_motivation": {...},
    "tension_management": {...},
    "hook_strength": {...},
    "subplot_advancement": {...},
    "relationship_dynamics": {...},
    "power_system_consistency": {...},
    "geographic_consistency": {...},
    "prop_continuity": {...},
    "language_variety": {...},
    "sentence_rhythm": {...},
    "vocabulary_richness": {...},
    "cliche_density": {...},
    "ai_pattern_detection": {...},
    "repetitive_sentence_structure": {...},
    "excessive_summarization": {...},
    "high_freq_ai_words": {...},
    "show_vs_tell": {...},
    "sensory_detail": {...},
    "inner_monologue_quality": {...},
    "chapter_length_adequacy": {...},
    "ending_hook": {...}
  },
  "overall_score": 0.82,
  "passed": true,
  "ai_probability": 0.3,
  "top_issues": ["问题1", "问题2"]
}

禁止输出任何JSON以外的内容。"""

# High-frequency AI-flavor words to detect
_AI_FLAVOR_WORDS = [
    "首先","其次","然后","最后","总的来说","综上所述","不得不说","值得注意的是",
    "总而言之","不仅如此","与此同时","尽管如此","事实上","毋庸置疑","显而易见",
    "另一方面","从某种程度上说","在某种意义上","不可否认","可以肯定的是",
    "正如","诚然","固然","况且","何况","由此可见","据此","基于此",
]

def _heuristic_audit(text: str, context: dict) -> dict:
    """Fast heuristic pass — runs without LLM, fills in obvious issues."""
    dimensions: dict = {}
    
    # chapter_length_adequacy
    wc = len(text)
    dims_length = {"score": 1.0, "passed": True, "issues": []}
    if wc < 1000:
        dims_length = {"score": 0.3, "passed": False, "issues": [f"篇幅过短（{wc}字，建议至少1500字）"]}
    elif wc < 2000:
        dims_length = {"score": 0.7, "passed": True, "issues": [f"篇幅偏短（{wc}字）"]}
    dimensions["chapter_length_adequacy"] = dims_length

    # ai_pattern_detection — check AI-flavor words
    ai_hits = [w for w in _AI_FLAVOR_WORDS if w in text]
    ai_score = max(0.0, 1.0 - len(ai_hits) * 0.08)
    dimensions["ai_pattern_detection"] = {
        "score": ai_score,
        "passed": len(ai_hits) < 5,
        "issues": [f"检测到AI味高频词：{', '.join(ai_hits[:8])}"] if ai_hits else [],
    }

    # high_freq_ai_words
    dimensions["high_freq_ai_words"] = {
        "score": ai_score,
        "passed": len(ai_hits) < 3,
        "issues": [f"高频AI惯用词（{len(ai_hits)}处）"] if len(ai_hits) >= 3 else [],
    }

    # repetitive_sentence_structure — detect repetitive sentence starters
    sentences = [s.strip() for s in re.split(r'[。！？…]', text) if len(s.strip()) > 5]
    starters = [s[:3] for s in sentences if len(s) >= 3]
    from collections import Counter
    start_counts = Counter(starters)
    repeated_starters = [(k, v) for k, v in start_counts.items() if v >= 4]
    dimensions["repetitive_sentence_structure"] = {
        "score": max(0.0, 1.0 - len(repeated_starters) * 0.15),
        "passed": len(repeated_starters) == 0,
        "issues": [f'句式开头重复过多：\u201c{k}\u201d出现{v}次' for k, v in repeated_starters[:3]],
    }

    # excessive_summarization — detect summary-style sentences
    summary_cues = ["总的来说", "总而言之", "综上", "简单来说", "说到底"]
    summary_hits = [c for c in summary_cues if c in text]
    dimensions["excessive_summarization"] = {
        "score": 1.0 - len(summary_hits) * 0.2,
        "passed": len(summary_hits) == 0,
        "issues": [f"过度总结式表达：{', '.join(summary_hits)}"] if summary_hits else [],
    }

    # cliche_density
    cliches = ["泪如雨下", "心如刀割", "血脉喷张", "怒火中烧", "虎躯一震", "眼神一凛",
               "眼冒金星", "不禁", "不由得", "忍不住", "顿时", "瞬间", "猛然"]
    cliche_hits = [c for c in cliches if text.count(c) >= 2]
    dimensions["cliche_density"] = {
        "score": max(0.0, 1.0 - len(cliche_hits) * 0.1),
        "passed": len(cliche_hits) < 4,
        "issues": [f"陈词滥调过多：{', '.join(cliche_hits[:5])}"] if len(cliche_hits) >= 4 else [],
    }

    return dimensions


@app.post("/audit/chapter")
async def audit_chapter(req: AuditChapterRequest):
    """
    33-dimension chapter audit.
    Phase 1: fast heuristic (no LLM).
    Phase 2: LLM deep eval (if llm_config provided).
    """
    import json
    
    heuristic_dims = _heuristic_audit(req.chapter_text, req.context)

    llm_dims: dict = {}
    ai_probability = 0.0
    top_issues: list = []

    if req.llm_config.get("api_key"):
        try:
            from langchain_openai import ChatOpenAI
            from langchain.schema import SystemMessage, HumanMessage
            
            base_url = req.llm_config.get("base_url", "https://api.openai.com/v1")
            model = req.llm_config.get("model", "gpt-4o-mini")
            api_key = req.llm_config.get("api_key")
            
            llm = ChatOpenAI(
                base_url=base_url,
                api_key=api_key,
                model=model,
                temperature=0.2,
                max_tokens=3000,
            )

            context_str = ""
            if req.context.get("outline_hint"):
                context_str += f"\n【本章大纲】{req.context['outline_hint']}"
            if req.context.get("book_rules"):
                context_str += f"\n【创作规则】{req.context['book_rules'][:500]}"
            if req.context.get("previous_summaries"):
                summaries = req.context["previous_summaries"][-3:]
                context_str += f"\n【前情摘要】{'；'.join(summaries)}"
            if req.context.get("characters"):
                context_str += f"\n【主要角色】{str(req.context['characters'])[:400]}"
            if req.context.get("foreshadowings"):
                context_str += f"\n【待回收伏笔】{str(req.context['foreshadowings'])[:300]}"

            human_content = (
                f"{context_str}\n\n"
                f"【章节正文（第{req.chapter_num}章）】\n{req.chapter_text[:6000]}"
            )

            response = await llm.ainvoke([
                SystemMessage(content=_AUDIT_SYSTEM),
                HumanMessage(content=human_content),
            ])

            raw = response.content.strip()
            # Strip markdown code fences if present
            if raw.startswith("```"):
                raw = re.sub(r"^```[a-z]*\n?", "", raw)
                raw = re.sub(r"\n?```$", "", raw)

            data = json.loads(raw)
            llm_dims = data.get("dimensions", {})
            ai_probability = data.get("ai_probability", 0.0)
            top_issues = data.get("top_issues", [])

        except Exception as exc:
            logger.warning("LLM audit failed, using heuristic only: %s", exc)

    # Merge: LLM dims override heuristic where available
    merged = {**heuristic_dims, **llm_dims}

    # Fill any missing dimensions with neutral scores
    all_dims = [
        "character_memory", "resource_continuity", "foreshadowing_recovery",
        "outline_deviation", "narrative_pace", "emotional_arc",
        "world_rule_compliance", "timeline_consistency", "pov_consistency",
        "dialogue_naturalness", "scene_description_quality", "conflict_escalation",
        "character_motivation", "tension_management", "hook_strength",
        "subplot_advancement", "relationship_dynamics", "power_system_consistency",
        "geographic_consistency", "prop_continuity", "language_variety",
        "sentence_rhythm", "vocabulary_richness", "cliche_density",
        "ai_pattern_detection", "repetitive_sentence_structure",
        "excessive_summarization", "high_freq_ai_words", "show_vs_tell",
        "sensory_detail", "inner_monologue_quality", "chapter_length_adequacy",
        "ending_hook",
    ]
    for d in all_dims:
        if d not in merged:
            merged[d] = {"score": 0.8, "passed": True, "issues": []}

    scores = [v["score"] for v in merged.values()]
    overall_score = round(sum(scores) / len(scores), 3) if scores else 0.0
    passed = overall_score >= 0.65

    all_issues = top_issues + [
        issue
        for dim in merged.values()
        for issue in dim.get("issues", [])
    ]

    return {
        "dimensions": merged,
        "overall_score": overall_score,
        "passed": passed,
        "ai_probability": ai_probability,
        "issues": list(dict.fromkeys(all_issues))[:20],  # deduplicate, cap at 20
    }


# ═══════════════════════════════════════════════════════════════════════════════
# ANTI-AI REWRITE (去AI味)
# ═══════════════════════════════════════════════════════════════════════════════

_ANTI_DETECT_SYSTEM = """你是一位专业的中文小说改写编辑，擅长将AI生成的文章改写成具有鲜明人类写作风格的作品。

改写原则：
1. 【词汇替换】替换AI高频词（首先/其次/总而言之/不得不说等），用更自然的表达
2. 【句式变化】打破重复句式结构，混用长短句，增加节奏感
3. 【减少总结】删除过度概括/总结性句子，改为具体的场景描写或细节
4. 【增加人味】加入口语化表达、主观感受、细节描写
5. 【情绪具体化】将抽象情感（"他感到悲伤"）改为具体行为/感官描写
6. 【禁用句式】避免：以"这"开头的总结句、大量"不仅...还"结构、过渡词堆叠
7. 【文风注入】{style_guide}

改写强度：{intensity}
- light: 仅替换最明显的AI痕迹词汇，保持原文结构
- medium: 句式重组 + 词汇替换 + 增加细节
- heavy: 全面重写，保留核心情节但大幅改变表达方式

严禁：
- 改变情节内容、角色名称、故事事实
- 添加原文没有的情节元素
- 删除关键情节

输出格式（严格JSON）：
{"rewritten_text": "...改写后全文...", "changes_made": ["改动说明1", "改动说明2"]}"""


@app.post("/anti-detect/rewrite")
async def anti_detect_rewrite(req: AntiDetectRequest):
    """Anti-AI rewrite: de-flavor AI-generated chapter text."""
    import json

    if not req.llm_config.get("api_key"):
        raise HTTPException(status_code=400, detail="llm_config.api_key is required for anti-detect rewrite")

    # Measure AI probability before
    metrics_before = metrics_estimator.estimate(req.text)
    ai_prob_before = metrics_before.get("ai_probability", 0.0)

    try:
        from langchain_openai import ChatOpenAI
        from langchain.schema import SystemMessage, HumanMessage

        style_guide = req.style_guide or "保持与原作品一致的风格"
        wordlist_note = ""
        if req.anti_ai_wordlist:
            wordlist_note = f"\n【禁用词汇】以下词汇须替换：{', '.join(req.anti_ai_wordlist[:30])}"
        patterns_note = ""
        if req.banned_patterns:
            patterns_note = f"\n【禁用句式】{'; '.join(req.banned_patterns[:10])}"

        system_prompt = (
            _ANTI_DETECT_SYSTEM
            .replace("{style_guide}", style_guide)
            .replace("{intensity}", req.intensity)
            + wordlist_note + patterns_note
        )

        llm = ChatOpenAI(
            base_url=req.llm_config.get("base_url", "https://api.openai.com/v1"),
            api_key=req.llm_config["api_key"],
            model=req.llm_config.get("model", "gpt-4o"),
            temperature=0.85,
            max_tokens=int(req.llm_config.get("max_tokens", 8192)),
        )

        response = await llm.ainvoke([
            SystemMessage(content=system_prompt),
            HumanMessage(content=f"请改写以下章节文本：\n\n{req.text[:8000]}"),
        ])

        raw = response.content.strip()
        if raw.startswith("```"):
            raw = re.sub(r"^```[a-z]*\n?", "", raw)
            raw = re.sub(r"\n?```$", "", raw)

        data = json.loads(raw)
        rewritten = data.get("rewritten_text", req.text)
        changes = data.get("changes_made", [])

    except json.JSONDecodeError:
        # LLM didn't return JSON — treat entire response as rewritten text
        rewritten = response.content.strip()  # type: ignore[possibly-undefined]
        changes = ["格式解析失败，使用原始输出"]
    except Exception as exc:
        raise HTTPException(status_code=500, detail=f"Anti-detect rewrite failed: {exc}")

    metrics_after = metrics_estimator.estimate(rewritten)
    ai_prob_after = metrics_after.get("ai_probability", 0.0)

    return {
        "original_text": req.text,
        "rewritten_text": rewritten,
        "changes_made": changes,
        "ai_prob_before": ai_prob_before,
        "ai_prob_after": ai_prob_after,
    }


# ═══════════════════════════════════════════════════════════════════════════════
# CREATIVE BRIEF → STORY BIBLE + BOOK RULES
# ═══════════════════════════════════════════════════════════════════════════════

_BRIEF_SYSTEM = """你是一位资深网文策划，擅长从零碎的创意简报生成完整的故事设定和写作规则。

根据用户提供的创作简报，生成：
1. 世界圣经（world_bible）：完整的世界观、核心人物、规则体系
2. 创作规则（rules_content）：故事的创作约束和基本规律
3. 风格指南（style_guide）：文风、叙事视角、节奏要求
4. AI禁用词列表（anti_ai_wordlist）：该书应该避免的AI味表达词汇
5. 禁用句式（banned_patterns）：该书风格中应避免的句式模式

返回严格的JSON格式：
{
  "world_bible": {
    "world_overview": "...",
    "power_system": {...},
    "main_characters": [...],
    "key_locations": [...],
    "core_conflicts": [...],
    "world_rules": [...],
    "prohibited": [...]
  },
  "rules_content": "...",
  "style_guide": "...",
  "anti_ai_wordlist": ["词1", "词2"],
  "banned_patterns": ["句式1", "句式2"]
}

确保world_bible详尽、rules_content具体可操作、style_guide有针对性。"""


@app.post("/creative-brief")
async def generate_creative_brief(req: CreativeBriefRequest):
    """Generate story_bible + book_rules from a creative brief document."""
    import json

    if not req.llm_config.get("api_key"):
        raise HTTPException(status_code=400, detail="llm_config.api_key is required")

    try:
        from langchain_openai import ChatOpenAI
        from langchain.schema import SystemMessage, HumanMessage

        llm = ChatOpenAI(
            base_url=req.llm_config.get("base_url", "https://api.openai.com/v1"),
            api_key=req.llm_config["api_key"],
            model=req.llm_config.get("model", "gpt-4o"),
            temperature=0.7,
            max_tokens=int(req.llm_config.get("max_tokens", 8192)),
        )

        human_content = f"【题材】{req.genre}\n\n【创作简报】\n{req.brief_text[:6000]}"
        response = await llm.ainvoke([
            SystemMessage(content=_BRIEF_SYSTEM),
            HumanMessage(content=human_content),
        ])

        raw = response.content.strip()
        if raw.startswith("```"):
            raw = re.sub(r"^```[a-z]*\n?", "", raw)
            raw = re.sub(r"\n?```$", "", raw)

        return json.loads(raw)

    except Exception as exc:
        raise HTTPException(status_code=500, detail=f"Creative brief generation failed: {exc}")


# ═══════════════════════════════════════════════════════════════════════════════
# CHAPTER IMPORT + REVERSE ENGINEERING
# ═══════════════════════════════════════════════════════════════════════════════

_REVERSE_SYSTEM = """你是一位文学分析专家，擅长从已有小说文本中逆向工程出完整的创作知识库。

分析给定的章节集合，提取以下7个真相文件：
1. world_state: 世界状态（地点、时代背景、规则体系）
2. character_matrix: 角色矩阵（主要角色、性格、当前状态、相互关系）
3. resource_ledger: 资源账本（主角持有的物品、能力、货币、重要道具）
4. foreshadowing_hooks: 伏笔清单（已埋下的伏笔及当前状态）
5. plot_threads: 剧情线（已推进的情节线及当前进展）
6. theme_analysis: 主题分析（核心主题、价值观、叙事风格）
7. style_fingerprint: 文风指纹（句长分布、词汇特征、叙述视角、节奏模式）

返回严格的JSON格式，7个顶层键对应7个真相文件。每个文件须详尽、具体。"""


@app.post("/import-chapters/analyze")
async def import_chapters_analyze(req: ImportChaptersRequest):
    """
    Split source text into chapters and reverse-engineer 7 truth files.
    Returns chapters list + reverse_engineered dict.
    This is designed to be called as a background task.
    """
    import json

    # Step 1: Split into chapters
    try:
        pattern = req.split_pattern or r"第.{1,4}[章节回]"
        parts = re.split(f"({pattern})", req.source_text)
    except re.error:
        parts = re.split(r"(第.{1,4}[章节回])", req.source_text)

    chapters = []
    title_buf = ""
    for part in parts:
        match = re.match(req.split_pattern or r"第.{1,4}[章节回]", part)
        if match:
            title_buf = part.strip()
        else:
            content = part.strip()
            if content and len(content) > 50:
                chapters.append({
                    "title": title_buf or f"第{len(chapters)+1}章",
                    "content": content,
                    "chapter_num": len(chapters) + 1,
                })
                title_buf = ""

    if not chapters:
        # No chapter markers found — treat entire text as one chapter
        chapters = [{"title": "第1章", "content": req.source_text, "chapter_num": 1}]

    # Step 2: Reverse-engineer using LLM (if configured)
    reverse_engineered: dict = {}
    if req.llm_config.get("api_key") and chapters:
        try:
            from langchain_openai import ChatOpenAI
            from langchain.schema import SystemMessage, HumanMessage

            llm = ChatOpenAI(
                base_url=req.llm_config.get("base_url", "https://api.openai.com/v1"),
                api_key=req.llm_config["api_key"],
                model=req.llm_config.get("model", "gpt-4o"),
                temperature=0.3,
                max_tokens=int(req.llm_config.get("max_tokens", 8192)),
            )

            # Use representative sample: first 3 + last 2 chapters
            sample_chapters = chapters[:3] + chapters[-2:] if len(chapters) > 5 else chapters
            sample_text = "\n\n---\n\n".join(
                f"【{c['title']}】\n{c['content'][:1500]}" for c in sample_chapters
            )

            fanfic_note = ""
            if req.fanfic_mode:
                mode_map = {
                    "canon": "正典延续（canon）：保持原作设定完整性",
                    "au": "架空世界（AU）：基于原作角色但世界设定不同",
                    "ooc": "性格重塑（OOC）：角色性格可大幅改变",
                    "cp": "CP向：侧重特定角色关系",
                }
                fanfic_note = f"\n\n【同人模式】这是{mode_map.get(req.fanfic_mode, req.fanfic_mode)}类型的创作，请相应标注同人专属分析。"

            response = await llm.ainvoke([
                SystemMessage(content=_REVERSE_SYSTEM + fanfic_note),
                HumanMessage(content=f"请分析以下章节样本：\n\n{sample_text}"),
            ])

            raw = response.content.strip()
            if raw.startswith("```"):
                raw = re.sub(r"^```[a-z]*\n?", "", raw)
                raw = re.sub(r"\n?```$", "", raw)
            reverse_engineered = json.loads(raw)
        except Exception as exc:
            logger.warning("Reverse engineering LLM call failed: %s", exc)
            reverse_engineered = {"error": str(exc)}

    return {
        "import_id": req.import_id,
        "chapters": chapters,
        "total_chapters": len(chapters),
        "reverse_engineered": reverse_engineered,
    }


if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("SIDECAR_PORT", "8081"))
    uvicorn.run(app, host="0.0.0.0", port=port)
