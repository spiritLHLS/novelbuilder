"""
Retrieval Node — Hybrid retrieval combining:
  • Neo4j (structural / graph): characters, world rules, relationships
  • Qdrant (semantic / vector): similar style passages, chapter summaries

Both queries run and their results populate world_track and narrative_track.
Single-query approach prevents N+1 problems.
"""
from __future__ import annotations

import logging
import os
from typing import Any

from agent.state import AgentState, GraphEntity, VectorHit, WorldContext, NarrativeContext

logger = logging.getLogger(__name__)


def _truncate_text(text: str, max_chars: int) -> str:
    clean = " ".join(str(text).split())
    if max_chars <= 0:
        return ""
    if len(clean) <= max_chars:
        return clean
    return clean[: max_chars - 1].rstrip() + "…"


def _dedupe_hits(hits: list[dict[str, Any]], *, limit: int, max_chars: int) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    seen: set[str] = set()
    for hit in hits:
        content = _truncate_text(hit.get("content", ""), max_chars)
        if not content or content in seen:
            continue
        seen.add(content)
        out.append({**hit, "content": content})
        if len(out) >= limit:
            break
    return out


# ── Neo4j retrieval ───────────────────────────────────────────────────────────

async def retrieve_world_node(state: AgentState) -> dict[str, Any]:
    """
    Query Neo4j for world-knowledge in a single Cypher query:
      - Character cores (name, role, traits, relations)
      - Constitution rules
      - Active foreshadowings
    """
    from graph_store.neo4j_client import Neo4jClient

    project_id = state["project_id"]
    client = Neo4jClient.get_instance()

    entities: list[GraphEntity] = []
    world_track = WorldContext()

    try:
        # Single Cypher — characters + relations (no N+1)
        char_results = await client.query(
            """
            MATCH (p:Project {id: $pid})-[:HAS_CHARACTER]->(c:Character)
            OPTIONAL MATCH (c)-[r:RELATES_TO]->(other:Character)
            RETURN c.id AS id, c.name AS name, c.role_type AS role,
                   c.core_traits AS traits,
                   collect({
                     rel_type: type(r),
                     target_name: other.name,
                     target_id: other.id
                   }) AS relations
            """,
            {"pid": project_id},
        )

        char_cores = []
        for row in char_results:
            ent = GraphEntity(
                id=row.get("id", ""),
                name=row.get("name", ""),
                entity_type="Character",
                properties={"role": row.get("role", ""), "traits": row.get("traits", "")},
                relations=[
                    r for r in (row.get("relations") or [])
                    if r.get("target_name")
                ],
            )
            entities.append(ent)
            char_cores.append({
                "name": row.get("name", ""),
                "role": row.get("role", ""),
                "traits": row.get("traits", ""),
                "relations": ent["relations"],
            })

        # Single Cypher — constitution rules
        rule_results = await client.query(
            """
            MATCH (p:Project {id: $pid})-[:HAS_RULE]->(r:Rule)
            WHERE r.immutable = true
            RETURN r.content AS content ORDER BY r.priority DESC
            """,
            {"pid": project_id},
        )
        rules = [row["content"] for row in rule_results if row.get("content")]

        # Single Cypher — active foreshadowings
        fs_results = await client.query(
            """
            MATCH (p:Project {id: $pid})-[:HAS_FORESHADOWING]->(f:Foreshadowing)
            WHERE f.status = 'active'
            RETURN f.content AS content ORDER BY f.priority DESC LIMIT 10
            """,
            {"pid": project_id},
        )
        foreshadowings = [row["content"] for row in fs_results if row.get("content")]

        world_track["character_cores"] = char_cores
        world_track["constitution_rules"] = rules
        world_track["foreshadowing_active"] = foreshadowings
        world_track["world_bible_summary"] = ""  # populated separately if needed

    except Exception as exc:
        logger.warning("Neo4j world retrieval failed: %s", repr(exc), exc_info=True)
        # Graceful degradation — return empty world context
        world_track["character_cores"] = []
        world_track["constitution_rules"] = []
        world_track["foreshadowing_active"] = []

    return {
        "world_track": world_track,
        "graph_entities": entities,
    }


# ── Qdrant retrieval ──────────────────────────────────────────────────────────

async def retrieve_narrative_node(state: AgentState) -> dict[str, Any]:
    """
    Query Qdrant for narrative-continuity context:
      - Recent chapter summaries (semantic search)
      - Style samples from reference material
    Collections queried: chapter_summaries, style_samples
    """
    from vector_store.qdrant_store import QdrantStore

    project_id = state["project_id"]
    query = state.get("user_prompt", "") + " " + state.get("outline_hint", "")
    store = QdrantStore.get_instance()
    short_term = state.get("short_term_paragraphs", [])
    summary_limit = 3 if short_term else 4

    narrative_track = NarrativeContext()
    hits: list[VectorHit] = []

    try:
        # Summary retrieval — top 5 most relevant recent chapter summaries
        summary_hits = await store.search(
            project_id=project_id,
            collection="chapter_summaries",
            query=query,
            limit=summary_limit + 2,
        )
        summary_hits = _dedupe_hits(summary_hits, limit=summary_limit, max_chars=420)
        recent_summaries = [h["content"] for h in summary_hits]

        # Style sample retrieval — top 3
        style_hits = await store.search(
            project_id=project_id,
            collection="style_samples",
            query=query,
            limit=3,
        )
        style_hits = _dedupe_hits(style_hits, limit=2, max_chars=320)
        style_samples = [h["content"] for h in style_hits]

        # Character voice samples — top 2 (for protagonist dialogue fidelity)
        voice_hits = []
        try:
            voice_hits = await store.search(
                project_id=project_id,
                collection="character_voices",
                query=query,
                limit=2,
            )
        except Exception:
            pass  # Collection may not exist yet

        if voice_hits:
            deduped_voice_hits = _dedupe_hits(voice_hits, limit=1, max_chars=240)
            style_samples.extend([h["content"] for h in deduped_voice_hits])

        narrative_track["recent_chapter_summaries"] = recent_summaries
        narrative_track["style_samples"] = style_samples
        narrative_track["current_arc_summary"] = ""
        narrative_track["plot_momentum"] = recent_summaries[0][:180] if recent_summaries else ""

        # Also append short-term paragraphs as immediate context
        if short_term:
            condensed_stm = "\n\n".join(_truncate_text(p, 180) for p in short_term[-3:])
            narrative_track["recent_chapter_summaries"] = (
                [condensed_stm] + recent_summaries
            )[:4]

        hits = summary_hits + style_hits

    except Exception as exc:
        logger.warning("Qdrant narrative retrieval failed: %s", repr(exc), exc_info=True)
        narrative_track["recent_chapter_summaries"] = state.get("short_term_paragraphs", [])
        narrative_track["style_samples"] = []

    return {
        "narrative_track": narrative_track,
        "vector_hits": hits,
    }
