"""
Memory Node — implements RecurrentGPT memory:
  • Short-term: last N paragraphs from Redis
  • Long-term: graphiti graph memory (entity/event recall from Neo4j)
  • Update: after generation, compress and store new memories
"""
from __future__ import annotations

import json
import logging
import os
from typing import Any

import redis

from agent.state import AgentState, GraphEntity

logger = logging.getLogger(__name__)

# ── Redis short-term memory ───────────────────────────────────────────────────

def _get_redis() -> redis.Redis:
    return redis.Redis(
        host=os.getenv("REDIS_HOST", "127.0.0.1"),
        port=int(os.getenv("REDIS_PORT", "6379")),
        decode_responses=True,
    )


def _stm_key(project_id: str) -> str:
    return f"stm:{project_id}:paragraphs"


def load_short_term(project_id: str, n: int = 5) -> list[str]:
    """Return the last n paragraphs from Redis short-term memory."""
    try:
        r = _get_redis()
        items = r.lrange(_stm_key(project_id), -n, -1)
        return items
    except Exception as exc:
        logger.warning("Redis STM read failed: %s", exc)
        return []


def save_short_term(project_id: str, paragraphs: list[str]) -> None:
    """Append new paragraphs to Redis short-term memory (keep last 20)."""
    if not paragraphs:
        return
    try:
        r = _get_redis()
        key = _stm_key(project_id)
        pipe = r.pipeline()
        for p in paragraphs:
            pipe.rpush(key, p)
        pipe.ltrim(key, -20, -1)
        pipe.execute()
    except Exception as exc:
        logger.warning("Redis STM write failed: %s", exc)


# ── graphiti long-term memory ─────────────────────────────────────────────────

def _build_graphiti(llm_cfg: dict[str, Any] | None = None):
    """Build graphiti client lazily to avoid import errors if Neo4j is down."""
    try:
        from graphiti_core import Graphiti
        from graphiti_core.llm_client.openai_client import OpenAIClient as GOpenAI
        from graphiti_core.embedder.openai_embedder import OpenAIEmbedder as GEmbed

        cfg = llm_cfg or {}
        neo4j_uri = os.getenv("NEO4J_URI", "bolt://127.0.0.1:7687")
        neo4j_user = os.getenv("NEO4J_USER", "neo4j")
        neo4j_pw = os.getenv("NEO4J_PASSWORD", "novelbuilder")
        api_key = (
            cfg.get("graphiti_api_key")
            or cfg.get("api_key")
            or os.getenv("GRAPHITI_LLM_API_KEY")
            or os.getenv("OPENAI_API_KEY", "placeholder")
        )
        base_url = (
            cfg.get("graphiti_base_url")
            or cfg.get("base_url")
            or os.getenv("GRAPHITI_LLM_BASE_URL")
            or os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
        )
        model = (
            cfg.get("graphiti_model")
            or cfg.get("model")
            or os.getenv("GRAPHITI_LLM_MODEL", "gpt-4o-mini")
        )

        llm_client = GOpenAI(api_key=api_key, base_url=base_url, model=model)
        embedder = GEmbed(api_key=api_key, base_url=base_url)

        return Graphiti(neo4j_uri, neo4j_user, neo4j_pw,
                        llm_client=llm_client, embedder=embedder)
    except Exception as exc:
        logger.warning("graphiti init failed (Neo4j may be warming up): %s", exc)
        return None


# Module-level lazy singleton
_graphiti_instance = None
_graphiti_sig: tuple[str, str, str] | None = None


def get_graphiti(llm_cfg: dict[str, Any] | None = None):
    global _graphiti_instance
    global _graphiti_sig

    cfg = llm_cfg or {}
    sig = (
        str(cfg.get("graphiti_base_url") or cfg.get("base_url") or ""),
        str(cfg.get("graphiti_model") or cfg.get("model") or ""),
        str(cfg.get("graphiti_api_key") or cfg.get("api_key") or ""),
    )
    if _graphiti_instance is None or _graphiti_sig != sig:
        _graphiti_instance = _build_graphiti(cfg)
        _graphiti_sig = sig
    return _graphiti_instance


async def recall_long_term(project_id: str, query: str, limit: int = 8,
                           llm_cfg: dict[str, Any] | None = None) -> list[GraphEntity]:
    """Search graphiti for relevant facts about the project."""
    g = get_graphiti(llm_cfg)
    if g is None:
        return []
    try:
        # graphiti search returns episode/edge results
        results = await g.search(query=query, num_results=limit)
        entities: list[GraphEntity] = []
        for r in results:
            entities.append(GraphEntity(
                id=str(r.uuid) if hasattr(r, "uuid") else "",
                name=getattr(r, "name", ""),
                entity_type=getattr(r, "source_description", "Fact"),
                properties={"fact": getattr(r, "fact", str(r))},
                relations=[],
            ))
        return entities
    except Exception as exc:
        logger.warning("graphiti recall failed: %s", exc)
        return []


async def update_long_term(project_id: str, chapter_num: int,
                           chapter_text: str, summary: str,
                           llm_cfg: dict[str, Any] | None = None) -> None:
    """Store a new chapter's events into graphiti's graph."""
    g = get_graphiti(llm_cfg)
    if g is None:
        return
    try:
        episode_name = f"project:{project_id}:chapter:{chapter_num}"
        await g.add_episode(
            name=episode_name,
            episode_body=summary or chapter_text[:2000],
            source_description=f"Chapter {chapter_num} of project {project_id}",
        )
        logger.info("graphiti updated for project=%s chapter=%d", project_id, chapter_num)
    except Exception as exc:
        logger.warning("graphiti update failed: %s", exc)


# ── LangGraph nodes ───────────────────────────────────────────────────────────

async def recall_memory_node(state: AgentState) -> dict[str, Any]:
    """
    RecurrentGPT recall step:
      1. Load short-term paragraphs from Redis
      2. Search graphiti for long-term relevant facts
    """
    project_id = state["project_id"]
    query = state.get("user_prompt", "") + " " + state.get("outline_hint", "")
    llm_cfg = state.get("llm_config", {})

    short_term = load_short_term(project_id)
    long_term = await recall_long_term(project_id, query, llm_cfg=llm_cfg)

    logger.info("Memory recalled: stm=%d paragraphs, ltm=%d entities",
                len(short_term), len(long_term))
    return {
        "short_term_paragraphs": short_term,
        "long_term_facts": long_term,
    }


async def update_memory_node(state: AgentState) -> dict[str, Any]:
    """
    RecurrentGPT update step:
      1. Append generated paragraphs to Redis short-term memory
      2. Store chapter events into graphiti long-term memory
    """
    project_id = state["project_id"]
    chapter_num = state.get("chapter_num", 0)
    draft = state.get("draft", "")
    summary = state.get("chapter_summary", "")
    llm_cfg = state.get("llm_config", {})

    # Split draft into paragraphs for STM
    paragraphs = [p.strip() for p in draft.split("\n\n") if p.strip()]
    save_short_term(project_id, paragraphs[-5:] if len(paragraphs) > 5 else paragraphs)

    # Update graphiti LTM asynchronously
    if draft:
        await update_long_term(project_id, chapter_num, draft, summary, llm_cfg=llm_cfg)

    return {}
