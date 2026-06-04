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

import httpx
from fastapi import FastAPI, HTTPException, BackgroundTasks
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import StreamingResponse

from json_repair import repair_json
from analyzers.style_analyzer import StyleAnalyzer
from analyzers.narrative_analyzer import NarrativeAnalyzer
from analyzers.atmosphere_analyzer import AtmosphereAnalyzer
from analyzers.plot_extractor import PlotExtractor
from humanizer.pipeline import HumanizationPipeline
from humanizer.metrics import PerplexityBurstinessEstimator
from api_models import (
    AgentRunRequest,
    BatchAgentRunRequest,
    EmbedRequest,
    GraphQueryRequest,
    GraphUpsertRequest,
    VectorDeleteBySourceRequest,
    VectorDeleteProjectRequest,
    VectorRebuildRequest,
    VectorSearchRequest,
    VectorUpsertRequest,
)
from app_config import parse_allowed_origins
from db_compat import SQLiteCompatConnection
from runtime_capabilities import detect_accelerators

try:
    import psycopg2
    import psycopg2.extras
except ImportError:  # PostgreSQL is optional in the SQLite profile.
    psycopg2 = None

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("python-agent")
# Suppress benign Neo4j schema-discovery warnings (empty DB, missing labels/props)
logging.getLogger("neo4j.notifications").setLevel(logging.ERROR)

# ── DB connection pool (for legacy analyze endpoint) ──────────────────────────
_db_pool = None

def _db_driver() -> str:
    return os.getenv("DB_DRIVER", "postgres").strip().lower()

def _env_int_min(name: str, default: int, minimum: int) -> int:
    try:
        value = int(os.getenv(name, str(default)))
    except (TypeError, ValueError):
        value = default
    return max(value, minimum)

def get_db():
    global _db_pool
    if _db_driver() in ("sqlite", "sqlite3"):
        return SQLiteCompatConnection(os.getenv("SQLITE_PATH", "/data/novelbuilder.db"))
    if psycopg2 is None:
        raise RuntimeError("psycopg2 is required when DB_DRIVER=postgres")
    if _db_pool is None:
        from psycopg2 import pool as pg_pool
        minconn = _env_int_min("SIDECAR_DB_MIN_CONNS", 5, 5)
        maxconn = max(_env_int_min("SIDECAR_DB_MAX_CONNS", 20, 20), minconn)
        _db_pool = pg_pool.ThreadedConnectionPool(
            minconn=minconn,
            maxconn=maxconn,
            host=os.getenv("DB_HOST", "127.0.0.1"),
            port=int(os.getenv("DB_PORT", "5432")),
            dbname=os.getenv("DB_NAME", "novelbuilder"),
            user=os.getenv("DB_USER", "novelbuilder"),
            password=os.getenv("DB_PASSWORD", ""),
            options="-c client_encoding=UTF8",
        )
    return _db_pool.getconn()

def put_db(conn):
    global _db_pool
    if _db_driver() in ("sqlite", "sqlite3"):
        conn.close()
        return
    if _db_pool is not None:
        _db_pool.putconn(conn)

# ── Lazy-init singletons ──────────────────────────────────────────────────────
_neo4j_client = None
_qdrant_store = None

def _service_enabled(env_name: str) -> bool:
    if env_name not in os.environ:
        return True
    raw = os.getenv(env_name, "").strip().lower()
    return raw not in ("", "0", "false", "off", "none", "disabled")

def neo4j_enabled() -> bool:
    return _service_enabled("NEO4J_URI")

def qdrant_enabled() -> bool:
    return _service_enabled("QDRANT_URL")

def get_neo4j():
    if not neo4j_enabled():
        raise HTTPException(status_code=503, detail="Neo4j is disabled for this deployment profile")
    global _neo4j_client
    if _neo4j_client is None:
        from graph_store.neo4j_client import Neo4jClient
        _neo4j_client = Neo4jClient.get_instance()
    return _neo4j_client

def get_qdrant():
    if not qdrant_enabled():
        raise HTTPException(status_code=503, detail="Qdrant is disabled for this deployment profile")
    global _qdrant_store
    if _qdrant_store is None:
        from vector_store.qdrant_store import QdrantStore
        _qdrant_store = QdrantStore.get_instance()
    return _qdrant_store

# ── In-memory agent session store ─────────────────────────────────────────────
# For production use Redis; this suffices for single-container deployment.
_agent_sessions: dict[str, dict] = {}
_SESSION_TTL_SECONDS = 3600  # 1 hour
_SESSION_CLEANUP_INTERVAL_SECONDS = int(os.getenv("AGENT_SESSION_CLEANUP_INTERVAL_SECONDS", "300"))
_session_lock = asyncio.Lock()


async def _cleanup_expired_sessions() -> None:
    """Remove completed/failed sessions older than TTL to prevent memory leak."""
    import time
    now = time.time()
    async with _session_lock:
        to_delete = [
            sid for sid, s in list(_agent_sessions.items())
            if s.get("status") in ("done", "error")
            and now - s.get("_created_at", now) > _SESSION_TTL_SECONDS
        ]
        for sid in to_delete:
            _agent_sessions.pop(sid, None)
        # Also clean batch sessions
        batch_delete = [
            bid for bid, s in list(_batch_sessions.items())
            if s.get("status") in ("done", "error")
            and now - s.get("_created_at", now) > _SESSION_TTL_SECONDS
        ]
        for bid in batch_delete:
            _batch_sessions.pop(bid, None)


async def _session_cleanup_loop() -> None:
    while True:
        await asyncio.sleep(max(30, _SESSION_CLEANUP_INTERVAL_SECONDS))
        try:
            await _cleanup_expired_sessions()
        except Exception as exc:
            logger.warning("session cleanup loop failed: %s", repr(exc), exc_info=True)

@asynccontextmanager
async def lifespan(app: FastAPI):
    logger.info("Agent service starting up...")
    logger.info("runtime accelerator selection: %s", json.dumps(detect_accelerators(), ensure_ascii=False))
    cleanup_task = asyncio.create_task(_session_cleanup_loop())
    # Warm up Neo4j schema
    if neo4j_enabled():
        try:
            neo4j = get_neo4j()
            await neo4j.ensure_schema()
            logger.info("Neo4j schema ensured")
        except Exception as exc:
            logger.warning("Neo4j schema init failed (may retry): %s", repr(exc), exc_info=True)
    else:
        logger.warning("Neo4j disabled by deployment profile")
    yield
    logger.info("Agent service shutting down...")
    cleanup_task.cancel()
    try:
        await cleanup_task
    except asyncio.CancelledError:
        pass
    # Close Neo4j driver
    try:
        if _neo4j_client is not None:
            await _neo4j_client.close()
    except Exception:
        pass
    # Close DB pool
    global _db_pool
    if _db_pool is not None:
        if hasattr(_db_pool, "closeall"):
            _db_pool.closeall()
        _db_pool = None

app = FastAPI(title="NovelBuilder Agent Service", version="2.0.0", lifespan=lifespan)


@app.get("/runtime/capabilities")
async def runtime_capabilities():
    return detect_accelerators()
app.add_middleware(
    CORSMiddleware,
    allow_origins=parse_allowed_origins(),
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

    The default cap is 100 so that a reference book analysed once can
    cover projects with up to 100 chapters. The Go rebuild service further
    re-samples from full chapter text according to the actual chapter count.
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


_ALLOWED_UPLOAD_DIR = os.path.abspath(os.getenv("UPLOAD_DIR", "/data/uploads"))

def _read_file(file_path: str) -> str:
    # Prevent path traversal: only allow files under the upload directory
    abs_path = os.path.abspath(file_path)
    try:
        is_under_upload_dir = os.path.commonpath([_ALLOWED_UPLOAD_DIR, abs_path]) == _ALLOWED_UPLOAD_DIR
    except ValueError:
        is_under_upload_dir = False
    if not is_under_upload_dir:
        logger.warning("Path traversal blocked: %s", file_path)
        return ""
    if not os.path.exists(abs_path):
        return ""
    file_path = abs_path
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
            logger.warning("Failed to read file %s", file_path, exc_info=True)
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
    await _cleanup_expired_sessions()
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


# ── Batch agent session store ──────────────────────────────────────────────────
# Tracks multi-chapter sequential generation sessions.
_batch_sessions: dict[str, dict] = {}


@app.post("/agent/batch-run")
async def agent_batch_run(req: BatchAgentRunRequest, background_tasks: BackgroundTasks):
    """Start a sequential multi-chapter generation session.

    Returns immediately; poll /agent/batch-status/{batch_id} or subscribe to
    /agent/batch-stream/{batch_id} for progress.
    """
    import time
    await _cleanup_expired_sessions()
    if not req.chapter_nums:
        raise HTTPException(status_code=400, detail="chapter_nums must not be empty")

    batch_id = str(uuid.uuid4())
    _batch_sessions[batch_id] = {
        "status": "running",
        "total": len(req.chapter_nums),
        "completed": 0,
        "current_chapter": None,
        "chapters": {},
        "error": None,
        "_created_at": time.time(),
    }

    async def _run():
        try:
            from agent.graph import run_agent
            from agent.state import AgentState

            for chapter_num in req.chapter_nums:
                if _batch_sessions[batch_id].get("status") == "error":
                    break

                _batch_sessions[batch_id]["current_chapter"] = chapter_num
                hint = req.outline_hints.get(str(chapter_num), "")

                initial: AgentState = {
                    "project_id": req.project_id,
                    "session_id": f"{batch_id}:ch{chapter_num}",
                    "task_type": "generate_chapter",
                    "user_prompt": hint or "请继续写下一章。",
                    "chapter_num": chapter_num,
                    "outline_hint": hint,
                    "style_profile": req.style_profile or {},
                    "llm_config": req.llm_config,
                    "max_retries": req.max_retries,
                    "messages": [],
                    "retry_count": 0,
                    "done": False,
                }

                try:
                    final = await run_agent(initial)
                    ch_result = {
                        "chapter_num": chapter_num,
                        "status": "done",
                        "final_text": final.get("final_text", final.get("draft", "")),
                        "chapter_summary": final.get("chapter_summary", ""),
                        "quality_score": final.get("quality_score", 0.0),
                        "quality_issues": final.get("quality_issues", []),
                    }
                except Exception as ch_exc:
                    logger.error(
                        "Batch chapter %d failed in batch %s: %s",
                        chapter_num, batch_id, ch_exc, exc_info=True,
                    )
                    ch_result = {
                        "chapter_num": chapter_num,
                        "status": "error",
                        "error": str(ch_exc),
                    }

                _batch_sessions[batch_id]["chapters"][str(chapter_num)] = ch_result
                _batch_sessions[batch_id]["completed"] += 1

            _batch_sessions[batch_id]["status"] = "done"
        except Exception as exc:
            logger.error("Batch session %s failed: %s", batch_id, exc, exc_info=True)
            _batch_sessions[batch_id]["status"] = "error"
            _batch_sessions[batch_id]["error"] = str(exc)

    background_tasks.add_task(_run)
    return {
        "batch_id": batch_id,
        "status": "running",
        "total": len(req.chapter_nums),
    }


@app.get("/agent/batch-status/{batch_id}")
async def agent_batch_status(batch_id: str):
    """Poll batch generation session status."""
    session = _batch_sessions.get(batch_id)
    if session is None:
        raise HTTPException(status_code=404, detail="Batch session not found")
    return {k: v for k, v in session.items() if not k.startswith("_")}


@app.get("/agent/batch-stream/{batch_id}")
async def agent_batch_stream(batch_id: str):
    """SSE stream for batch generation progress."""
    if batch_id not in _batch_sessions:
        raise HTTPException(status_code=404, detail="Batch session not found")

    async def event_gen():
        prev_completed = -1
        for _ in range(600):  # max 10 minutes
            session = _batch_sessions.get(batch_id, {})
            completed = session.get("completed", 0)
            if completed != prev_completed:
                evt = {
                    "completed": completed,
                    "total": session.get("total", 0),
                    "current_chapter": session.get("current_chapter"),
                    "status": session.get("status"),
                }
                yield f"data: {json.dumps(evt, ensure_ascii=False)}\n\n"
                prev_completed = completed
            if session.get("status") in ("done", "error"):
                final_evt = {
                    "status": session.get("status"),
                    "completed": session.get("completed", 0),
                    "total": session.get("total", 0),
                    "chapters": session.get("chapters", {}),
                    "error": session.get("error"),
                }
                yield f"data: {json.dumps(final_evt, ensure_ascii=False)}\n\n"
                break
            await asyncio.sleep(1)

    return StreamingResponse(
        event_gen(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


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
    elif req.entity_type == "Event":
        await neo4j.upsert_event(
            project_id=req.project_id,
            event_id=req.entity_id,
            description=req.properties.get("description", req.name),
            chapter_num=req.properties.get("chapter_num", 0),
            involved_chars=req.properties.get("involved_chars", []),
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
    synced = {"characters": 0, "rules": 0, "foreshadowings": 0, "errors": []}

    try:
        cur = conn.cursor(cursor_factory=psycopg2.extras.DictCursor if psycopg2 else None)

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
            try:
                await neo4j.upsert_character(
                    project_id=project_id,
                    char_id=str(c["id"]),
                    name=c["name"],
                    role_type=c["role_type"] or "supporting",
                    core_traits=c["core_traits"] or "",
                )
                synced["characters"] += 1
            except Exception as exc:
                logger.warning("graph sync: failed to upsert character %s: %s", c["id"], exc)
                synced["errors"].append(f"character {c['id']}: {exc}")

        # Batch load all foreshadowings (single query)
        cur.execute("""
            SELECT id, content, status, priority
            FROM foreshadowings WHERE project_id = %s
        """, (project_id,))
        for f in cur.fetchall():
            try:
                await neo4j.upsert_foreshadowing(
                    project_id=project_id,
                    fs_id=str(f["id"]),
                    content=f["content"],
                    status=f["status"],
                    priority=int(f["priority"]),
                )
                synced["foreshadowings"] += 1
            except Exception as exc:
                logger.warning("graph sync: failed to upsert foreshadowing %s: %s", f["id"], exc)
                synced["errors"].append(f"foreshadowing {f['id']}: {exc}")

        # Batch load world constitution rules (single query)
        cur.execute("""
            SELECT CASE
                     WHEN jsonb_typeof(elem) = 'object' THEN COALESCE(elem->>'rule', '')
                     ELSE elem #>> '{}'
                   END AS rule
            FROM world_bible_constitutions,
                 jsonb_array_elements(immutable_rules) AS elem
            WHERE project_id = %s
        """, (project_id,))
        for i, r in enumerate(cur.fetchall()):
            rule_id = f"{project_id}:rule:imm:{i}"
            try:
                await neo4j.upsert_rule(
                    project_id=project_id,
                    rule_id=rule_id,
                    content=r["rule"],
                    immutable=True,
                    priority=max(1, 10 - i),
                )
                synced["rules"] += 1
            except Exception as exc:
                logger.warning("graph sync: failed to upsert immutable rule %s: %s", rule_id, exc)
                synced["errors"].append(f"immutable rule {rule_id}: {exc}")

        cur.execute("""
            SELECT CASE
                     WHEN jsonb_typeof(elem) = 'object' THEN COALESCE(elem->>'rule', '')
                     ELSE elem #>> '{}'
                   END AS rule
            FROM world_bible_constitutions,
                 jsonb_array_elements(mutable_rules) AS elem
            WHERE project_id = %s
        """, (project_id,))
        for i, r in enumerate(cur.fetchall()):
            rule_id = f"{project_id}:rule:mut:{i}"
            try:
                await neo4j.upsert_rule(
                    project_id=project_id,
                    rule_id=rule_id,
                    content=r["rule"],
                    immutable=False,
                    priority=max(1, 5 - i),
                )
                synced["rules"] += 1
            except Exception as exc:
                logger.warning("graph sync: failed to upsert mutable rule %s: %s", rule_id, exc)
                synced["errors"].append(f"mutable rule {rule_id}: {exc}")

    finally:
        put_db(conn)

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
    from vector_store.qdrant_store import COLLECTIONS

    req.query = req.query.strip()
    if not req.query:
        raise HTTPException(status_code=400, detail="query is required")

    limit = req.top_k or req.limit or 5
    limit = max(1, min(int(limit), 50))
    requested = req.collections or ([req.collection] if req.collection else list(COLLECTIONS))
    collections = [c for c in dict.fromkeys(requested) if c in COLLECTIONS]
    if not collections:
        raise HTTPException(status_code=400, detail="no valid vector collection selected")

    hits = await store.search_collections(
        project_id=req.project_id,
        collections=collections,
        query=req.query,
        limit=limit,
        score_threshold=req.score_threshold,
    )
    return {"hits": hits, "results": hits}


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


@app.post("/vector/delete-project")
async def vector_delete_project(req: VectorDeleteProjectRequest):
    """Delete all vector data across every collection for a project (used before full rebuild)."""
    store = get_qdrant()
    await store.delete_all_project_vectors(req.project_id)
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
from routes_fanqie import router as fanqie_router

app.include_router(analysis_router)
app.include_router(audit_router)
app.include_router(novels_router)
app.include_router(deep_analysis_router)
app.include_router(fanqie_router)

if __name__ == '__main__':
    import uvicorn
    port = int(os.getenv('SIDECAR_PORT', '8081'))
    uvicorn.run(app, host='0.0.0.0', port=port)
