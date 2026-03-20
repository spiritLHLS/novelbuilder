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

class NarrativeReviseRequest(BaseModel):
    chapter_id: str
    chapter_text: str
    failing_dimensions: list[str] = []
    top_issues: list[str] = []
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


def _repair_json(raw: str) -> dict:
    """Strip markdown fences and attempt basic repair of truncated/malformed LLM JSON output."""
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
    # Return 404 immediately if the session doesn't exist yet to avoid
    # a silent 5-minute spin.
    if session_id not in _agent_sessions:
        raise HTTPException(status_code=404, detail="Session not found")

    async def event_gen():
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
    # Only allow read queries for safety — block known write/admin keywords.
    _WRITE_KEYWORDS = ("DELETE", "DROP", "DETACH", "CREATE", "MERGE", "SET", "REMOVE", "CALL")
    if any(kw in req.cypher.upper() for kw in _WRITE_KEYWORDS):
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

# ── Router modules ───────────────────────────────────────────────────────
from routes_analysis import router as analysis_router
from routes_audit import router as audit_router
from routes_novels import router as novels_router
from routes_deep_analysis import router as deep_analysis_router

app.include_router(analysis_router)
app.include_router(audit_router)
app.include_router(novels_router)
app.include_router(deep_analysis_router)

if __name__ == '__main__':
    import uvicorn
    port = int(os.getenv('SIDECAR_PORT', '8081'))
    uvicorn.run(app, host='0.0.0.0', port=port)
