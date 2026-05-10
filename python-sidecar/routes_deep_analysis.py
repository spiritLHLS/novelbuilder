"""
routes_deep_analysis.py — FastAPI router for chunked, background-aware deep
analysis of large reference novels.

Endpoints
---------
POST /deep-analyze/chunk
    Analyze a single text chunk and return extracted entities.
    Called repeatedly by the Go service (once per chunk) inside a task.

POST /deep-analyze/merge
    Merge all chunk extractions, deduplicate, return final result.
    Called by the Go service at the end of the task.

All heavy logic lives in the sibling modules:
  deep_analysis_models  — Pydantic models + small utilities
  deep_analysis_llm     — _llm_extract, _parse_json, chunk prompt
  deep_analysis_merge   — world/character/outline merge helpers
"""
from __future__ import annotations

import logging
import os

from fastapi import APIRouter

from deep_analysis_models import (
    LLMConfig,
    ChunkAnalyzeRequest,
    MergeRequest,
    _cfg_with_child_session,
    _split_delimited,
    _ensure_string_list,
    _normalize_constitutions,
)
from deep_analysis_llm import _llm_extract, _parse_json, _CHUNK_PROMPT
from deep_analysis_merge import (
    _sort_by_importance,
    _merge_world_chunked,
    _merge_characters_chunked,
    _dedup_outline,
    _dedup_glossary,
    _dedup_foreshadowings,
)

logger = logging.getLogger("python-agent")

router = APIRouter()


# ── Chunk analysis endpoint ───────────────────────────────────────────────────

@router.post("/deep-analyze/chunk")
async def analyze_chunk(req: ChunkAnalyzeRequest):
    """Analyze a single chunk of text and return extracted entities."""
    cfg = req.llm_config or LLMConfig()
    if not cfg.session_id:
        cfg = cfg.model_copy(update={"session_id": req.job_id})

    # Build prior context hint so the LLM skips already-known entities.
    prior_ctx = req.prior_context or {}
    if prior_ctx:
        lines = []
        if prior_ctx.get("characters"):
            lines.append("- 已知人物：" + "、".join(prior_ctx["characters"]))
        if prior_ctx.get("locations"):
            lines.append("- 已知地点：" + "、".join(prior_ctx["locations"]))
        if prior_ctx.get("systems"):
            lines.append("- 已知体系：" + "、".join(prior_ctx["systems"]))
        if prior_ctx.get("glossary"):
            lines.append("- 已知术语：" + "、".join(prior_ctx["glossary"]))
        prior_context_section = (
            "已在前序片段中提取的实体（请勿重复提取，仅补充本片段中出现的新信息）：\n"
            + "\n".join(lines)
            + "\n\n"
        )
    else:
        prior_context_section = ""

    prompt = _CHUNK_PROMPT.format(
        chunk_index=req.chunk_index + 1,
        total_chunks=req.total_chunks,
        text=req.chunk_text,
        prior_context_section=prior_context_section,
    )

    logger.info("deep-analyze chunk %d/%d job=%s", req.chunk_index + 1, req.total_chunks, req.job_id)
    result = await _llm_extract(prompt, cfg, request_label="reference_deep_analysis_chunk")

    # Convert flat LLM output to the structured format expected by downstream consumers.
    chars = result.get("characters", [])
    for ch in chars:
        # traits: "特点1，特点2" -> ["特点1", "特点2"]
        traits_raw = ch.pop("traits", "")
        if isinstance(traits_raw, str):
            ch["traits"] = _split_delimited(traits_raw, ",")
        elif not isinstance(traits_raw, list):
            ch["traits"] = []
        else:
            ch["traits"] = traits_raw
        # relationships: "角色A：关系；角色B：关系" -> [{"name": "角色A", "description": "关系"}]
        rel_raw = ch.pop("relationships", "")
        if isinstance(rel_raw, str):
            rel_list = []
            for item in _split_delimited(rel_raw, ";"):
                if "：" in item:
                    parts = item.split("：", 1)
                    rel_list.append({"name": parts[0].strip(), "description": parts[1].strip()})
                elif ":" in item:
                    parts = item.split(":", 1)
                    rel_list.append({"name": parts[0].strip(), "description": parts[1].strip()})
                else:
                    rel_list.append({"name": item, "description": ""})
            ch["relationships"] = rel_list
        elif not isinstance(rel_raw, list):
            ch["relationships"] = []

    # Rebuild world dict from flat keys
    world = {
        "setting": result.get("world_setting", ""),
        "time_period": result.get("world_time_period", ""),
        "locations": _split_delimited(result.get("world_locations", ""), ";"),
        "systems": _split_delimited(result.get("world_systems", ""), ";"),
        "social_structure": result.get("world_social_structure", ""),
        "core_conflict": result.get("world_core_conflict", ""),
        "factions": _split_delimited(result.get("world_factions", ""), ";"),
        "constitutions": _normalize_constitutions(result.get("world_constitutions", [])),
        "forbidden_anchors": _ensure_string_list(result.get("forbidden_anchors", []), ";"),
    }

    # Convert outline.characters string to involved_characters list
    outline = result.get("outline", [])
    for node in outline:
        chars_str = node.pop("characters", "")
        if isinstance(chars_str, str):
            node["involved_characters"] = _split_delimited(chars_str, ",")
        elif "involved_characters" not in node:
            node["involved_characters"] = []

    # Convert foreshadowings.characters string to related_characters list
    foreshadowings = result.get("foreshadowings", [])
    for f in foreshadowings:
        chars_str = f.pop("characters", "")
        if isinstance(chars_str, str):
            f["related_characters"] = _split_delimited(chars_str, ",")
        elif "related_characters" not in f:
            f["related_characters"] = []

    return {
        "job_id": req.job_id,
        "chunk_index": req.chunk_index,
        "characters": _sort_by_importance(chars),
        "world": world,
        "outline": outline,
        "glossary": result.get("glossary", []),
        "foreshadowings": foreshadowings,
    }


# ── Merge endpoint ────────────────────────────────────────────────────────────

@router.post("/deep-analyze/merge")
async def merge_chunks(req: MergeRequest):
    """Merge per-chunk extractions into a unified, deduplicated result."""
    import asyncio

    all_chars = []
    all_worlds = []
    all_outlines = []
    all_glossary = []
    all_foreshadowings = []

    for chunk in req.chunks:
        all_chars.extend(chunk.characters or [])
        if chunk.world:
            all_worlds.append(chunk.world)
        all_outlines.extend(chunk.outline or [])
        all_glossary.extend(chunk.glossary or [])
        all_foreshadowings.extend(chunk.foreshadowings or [])

    # Prefer the llm_config forwarded by the Go service (contains the user's configured key).
    # Fall back to environment variables only when no config is provided.
    if req.llm_config and req.llm_config.api_key:
        cfg = LLMConfig(
            api_key=req.llm_config.api_key,
            base_url=req.llm_config.base_url,
            model=req.llm_config.model,
            max_tokens=req.llm_config.max_tokens,
            temperature=req.llm_config.temperature,
            rpm_limit=req.llm_config.rpm_limit,
            omit_max_tokens=req.llm_config.omit_max_tokens,
            omit_temperature=req.llm_config.omit_temperature,
            api_style=req.llm_config.api_style,
            timeout=req.llm_config.timeout,
            json_mode=req.llm_config.json_mode,
            session_id=req.llm_config.session_id or req.job_id,
        )
    else:
        cfg = LLMConfig(
            api_key=os.getenv("OPENAI_API_KEY", ""),
            base_url=os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
            model=os.getenv("OPENAI_MODEL", "gpt-4o"),
            max_tokens=4096,
            temperature=0.3,
            session_id=req.job_id,
        )

    logger.info("merge: %d raw character entries, %d world entries", len(all_chars), len(all_worlds))
    chars_cfg = _cfg_with_child_session(cfg, "merge-characters")
    world_cfg = _cfg_with_child_session(cfg, "merge-world")
    final_chars_raw, world_result = await asyncio.gather(
        _merge_characters_chunked(all_chars, chars_cfg),
        _merge_world_chunked(all_worlds, world_cfg),
    )
    logger.info("merge: %d characters after chunked merge", len(final_chars_raw) if isinstance(final_chars_raw, list) else 0)

    # Convert flat merge output (traits/relationships as strings) back to structured lists
    final_chars = []
    for ch in final_chars_raw:
        traits_raw = ch.pop("traits", "")
        if isinstance(traits_raw, str):
            ch["traits"] = _split_delimited(traits_raw, ",")
        elif not isinstance(traits_raw, list):
            ch["traits"] = []
        rel_raw = ch.pop("relationships", "")
        if isinstance(rel_raw, str):
            rel_list = []
            for item in _split_delimited(rel_raw, ";"):
                if "：" in item:
                    parts = item.split("：", 1)
                    rel_list.append({"name": parts[0].strip(), "description": parts[1].strip()})
                elif ":" in item:
                    parts = item.split(":", 1)
                    rel_list.append({"name": parts[0].strip(), "description": parts[1].strip()})
                else:
                    rel_list.append({"name": item, "description": ""})
            ch["relationships"] = rel_list
        elif not isinstance(rel_raw, list):
            ch["relationships"] = []
        final_chars.append(ch)
    final_chars = _sort_by_importance(final_chars)
    final_world = world_result if isinstance(world_result, dict) else (all_worlds[0] if all_worlds else {})

    # Outline: Python dedup — preserve all nodes up to 300, ordered by level (macro first)
    deduped_outline = _dedup_outline(all_outlines)
    macro = [n for n in deduped_outline if n.get("level") == "macro"]
    meso  = [n for n in deduped_outline if n.get("level") == "meso"]
    micro = [n for n in deduped_outline if n.get("level") == "micro"]
    final_outline = macro + meso + micro

    # Glossary: Python dedup by term name
    final_glossary = _dedup_glossary(all_glossary)

    # Foreshadowings: Python dedup by content prefix
    final_foreshadowings = _dedup_foreshadowings(all_foreshadowings)

    return {
        "job_id": req.job_id,
        "characters": final_chars,
        "world": final_world,
        "outline": final_outline,
        "glossary": final_glossary,
        "foreshadowings": final_foreshadowings,
    }
