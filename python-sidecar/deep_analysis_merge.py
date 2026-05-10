"""
deep_analysis_merge.py — Merge / deduplication helpers for the deep-analysis
pipeline.  These functions aggregate per-chunk extractions into a unified,
deduplicated result set.
"""
from __future__ import annotations

import asyncio
import json
import logging

from deep_analysis_models import LLMConfig, _cfg_with_child_session, _normalize_constitutions
from deep_analysis_llm import _llm_extract

logger = logging.getLogger("python-agent")

# ── LLM prompts used during merge ─────────────────────────────────────────────

_MERGE_CHARACTERS_PROMPT = """以下是从同一部小说不同章节片段提取的人物列表（JSON数组），
请合并去重，整合同一人物的信息，返回精炼后的人物列表JSON数组，格式：
[{{"name":"...","role":"...","description":"50字以内","traits":"特点1，特点2，特点3","motivation":"30字以内","growth_arc":"30字以内","relationships":"角色A：关系；角色B：关系"}}]

原始数据：
{data}

要求：
1. 同名或明显相同的人物合并为一条，各字段取最完整版本并精简到字数限制内
2. 保留所有重要人物（主角、主要配角、反派等），不限数量
3. traits为逗号分隔的字符串（最多5个特点），relationships为"角色：关系"用分号分隔（最多5条）
4. 每个字段严格控制字数，优先保证JSON完整输出
5. 按重要度从高到低排列：主角最先，其次主要配角、反派，配角、其他类最后
6. 只返回JSON数组，不要其他文字，确保所有括号/引号完整闭合
"""

_MERGE_WORLD_SETTING_PROMPT = """以下是从同一部小说不同章节片段提取的世界观背景描述（文字列表），
请将这些描述综合整合成一段连贯的世界观summary，100字以内，不要列举原文，用自己的语言综合。
只返回综合后的纯文字，不要任何JSON或其他格式。

原始描述列表：
{data}
"""

_MERGE_WORLD_PROMPT = """以下是从同一部小说不同章节片段提取的世界观信息（JSON数组），
请整合所有信息，返回一个统一的世界观JSON对象：
{{"setting":"...","time_period":"...","locations":["..."],"systems":["..."]}}

原始数据：
{data}

要求：
1. setting综合所有描述，100字以内
2. locations取并集去重，不超过50条；systems取并集去重，不超过25条
3. 只返回JSON对象，不要其他文字，确保所有括号/引号完整闭合
"""

_MERGE_GLOSSARY_PROMPT = """以下是从同一部小说不同章节片段提取的术语列表（JSON数组），
请合并去重，返回精炼后的术语列表JSON数组，格式：
[{{"term":"...","definition":"30字以内","category":"character/place/item/concept/other"}}]

原始数据：
{data}

要求：
1. 同名或明显相同的术语合并为一条，definition精简到30字以内
2. 保留所有不重复术语，总数不超过100条
3. 只返回JSON数组，不要其他文字，确保所有括号/引号完整闭合
"""

_MERGE_FORESHADOWINGS_PROMPT = """以下是从同一部小说不同章节片段提取的伏笔列表（JSON数组），
请整合去重，返回精炼后的伏笔列表JSON数组，格式：
[{{"content":"50字以内","related_characters":["角色名1","角色名2"],"priority":3}}]

原始数据：
{data}

要求：
1. 内容明显重复的伏笔合并为一条，content精简到50字以内
2. 保留所有不重复伏笔，总数不超过50条
3. related_characters为字符串数组
4. 只返回JSON数组，不要其他文字，确保所有括号/引号完整闭合
"""


# ── Role importance helpers ───────────────────────────────────────────────────

_ROLE_PRIORITY: dict[str, int] = {
    "主角": 0,
    "protagonist": 0,
    "主要配角": 1,
    "主要角色": 1,
    "反派": 2,
    "villain": 2,
    "antagonist": 2,
    "配角": 3,
    "supporting": 3,
    "其他": 4,
    "other": 4,
}


def _role_priority(role: str) -> int:
    """Return sort key for a character role string (lower = more important)."""
    if not role:
        return 4
    r = role.strip()
    if r in _ROLE_PRIORITY:
        return _ROLE_PRIORITY[r]
    rl = r.lower()
    if "主角" in rl or "protagonist" in rl:
        return 0
    if "主要" in rl:
        return 1
    if "反派" in rl or "villain" in rl or "antagonist" in rl:
        return 2
    if "配角" in rl or "supporting" in rl:
        return 3
    return 4


def _sort_by_importance(chars: list) -> list:
    """Return characters sorted by role importance (most important first)."""
    return sorted(chars, key=lambda c: _role_priority(c.get("role", "") if isinstance(c, dict) else ""))


# ── World chunked merge ───────────────────────────────────────────────────────

async def _merge_world_chunked(all_worlds: list, cfg: LLMConfig) -> dict:
    """Merge world data from all chunks without any hard truncation.

    Strategy:
    - locations, systems, factions, forbidden_anchors: Python set-union (no LLM, no truncation).
    - setting text: batch into ≤12 000-byte chunks and LLM-summarize each,
      then do one final summary pass for multi-batch novels.
    - time_period / social_structure / core_conflict: pick the longest/most-informative value.
    """
    if not all_worlds:
        return {}

    loc_set: set[str] = set()
    sys_set: set[str] = set()
    faction_set: set[str] = set()
    forbidden_set: set[str] = set()
    settings: list[str] = []
    time_candidates: list[str] = []
    social_candidates: list[str] = []
    conflict_candidates: list[str] = []
    constitution_map: dict[tuple[str, str], dict] = {}

    for w in all_worlds:
        if not isinstance(w, dict):
            continue
        s = (w.get("setting") or "").strip()
        if s:
            settings.append(s)
        t = (w.get("time_period") or "").strip()
        if t:
            time_candidates.append(t)
        social = (w.get("social_structure") or "").strip()
        if social:
            social_candidates.append(social)
        conflict = (w.get("core_conflict") or "").strip()
        if conflict:
            conflict_candidates.append(conflict)
        for loc in (w.get("locations") or []):
            if isinstance(loc, str) and loc.strip():
                loc_set.add(loc.strip())
        for sys_ in (w.get("systems") or []):
            if isinstance(sys_, str) and sys_.strip():
                sys_set.add(sys_.strip())
        for faction in (w.get("factions") or []):
            if isinstance(faction, str) and faction.strip():
                faction_set.add(faction.strip())
        for forbidden in (w.get("forbidden_anchors") or []):
            if isinstance(forbidden, str) and forbidden.strip():
                forbidden_set.add(forbidden.strip())
        for item in _normalize_constitutions(w.get("constitutions", [])):
            key = (item["type"], item["rule"].lower())
            if key not in constitution_map or len(item.get("reason", "")) > len(constitution_map[key].get("reason", "")):
                constitution_map[key] = item

    # Pick the most informative scalar fields (longest non-empty string).
    time_period = max(time_candidates, key=len) if time_candidates else ""
    social_structure = max(social_candidates, key=len) if social_candidates else ""
    core_conflict = max(conflict_candidates, key=len) if conflict_candidates else ""

    # Deduplicate settings text by 30-char prefix to remove near-duplicate short entries.
    unique_settings: list[str] = []
    seen_prefixes: set[str] = set()
    for s in settings:
        prefix = s[:30].lower()
        if prefix not in seen_prefixes:
            seen_prefixes.add(prefix)
            unique_settings.append(s)

    # Batch settings texts for LLM summarization (≤12 000 bytes per batch).
    MAX_SETTING_BYTES = 12_000
    batches: list[list[str]] = []
    cur: list[str] = []
    cur_size = 0
    for s in unique_settings:
        if cur and cur_size + len(s) > MAX_SETTING_BYTES:
            batches.append(cur)
            cur = [s]
            cur_size = len(s)
        else:
            cur.append(s)
            cur_size += len(s)
    if cur:
        batches.append(cur)

    setting_summary = ""
    if not batches:
        pass
    elif len(batches) == 1 and len(batches[0]) == 1:
        setting_summary = batches[0][0][:200]
    else:
        batch_summaries: list[str] = []
        for batch in batches:
            data_text = "\n".join(f"{j+1}. {s}" for j, s in enumerate(batch))
            result = await _llm_extract(_MERGE_WORLD_SETTING_PROMPT.format(data=data_text), cfg)
            # _MERGE_WORLD_SETTING_PROMPT asks for plain text; LLM may wrap it in a JSON key
            if isinstance(result, dict):
                raw = result.get("setting") or result.get("text") or result.get("summary") or ""
            else:
                raw = ""
            batch_summaries.append(raw or " ".join(batch)[:200])
        if len(batch_summaries) == 1:
            setting_summary = batch_summaries[0]
        else:
            combined = "\n".join(f"{j+1}. {s}" for j, s in enumerate(batch_summaries))
            result = await _llm_extract(_MERGE_WORLD_SETTING_PROMPT.format(data=combined), cfg)
            if isinstance(result, dict):
                setting_summary = result.get("setting") or result.get("text") or result.get("summary") or ""
            if not setting_summary:
                setting_summary = " / ".join(batch_summaries)[:300]

    return {
        "setting": setting_summary,
        "time_period": time_period,
        "locations": sorted(loc_set),
        "systems": sorted(sys_set),
        "social_structure": social_structure,
        "core_conflict": core_conflict,
        "factions": sorted(faction_set),
        "constitutions": sorted(constitution_map.values(), key=lambda item: (0 if item.get("type") == "immutable" else 1, item.get("rule", ""))),
        "forbidden_anchors": sorted(forbidden_set),
    }


async def _llm_merge_chars_batch(batch: list, cfg: LLMConfig, depth: int = 0) -> list:
    """Recursively merge a character batch, splitting in half when the output is
    truncated (finish_reason=length).

    When the LLM cannot fit all merged entries in its output token budget, we
    split the input in half, merge each half independently (which produces
    shorter outputs), then combine and optionally do one final merge pass.

    Recursion is capped at depth 5 (≤ 32 sub-batches), which is more than
    enough for any realistic novel character list.
    """
    MAX_DEPTH = 5
    # Safety: single entry or recursion limit reached — return as-is.
    if not batch:
        return []
    if len(batch) <= 1 or depth > MAX_DEPTH:
        return batch

    chars_data = json.dumps(batch, ensure_ascii=False)
    meta: dict = {}
    result = await _llm_extract(_MERGE_CHARACTERS_PROMPT.format(data=chars_data), cfg, _meta=meta)
    merged = result if isinstance(result, list) else result.get("characters", [])

    if not meta.get("truncated"):
        # Complete result — use it directly.
        logger.debug(
            "_llm_merge_chars_batch: depth=%d %d entries → %d (complete)",
            depth, len(batch), len(merged),
        )
        return merged

    # ── Output truncated: split in half and merge each half recursively ───────
    logger.warning(
        "_llm_merge_chars_batch: truncated at depth=%d with %d entries, "
        "splitting in half and retrying each side",
        depth, len(batch),
    )
    mid = len(batch) // 2
    left_cfg = _cfg_with_child_session(cfg, f"d{depth + 1}-left")
    right_cfg = _cfg_with_child_session(cfg, f"d{depth + 1}-right")
    left, right = await asyncio.gather(
        _llm_merge_chars_batch(batch[:mid], left_cfg, depth + 1),
        _llm_merge_chars_batch(batch[mid:], right_cfg, depth + 1),
    )

    # Python-level name-dedup on the combined halves before the final pass.
    combined_map: dict[str, dict] = {}
    for ch in left + right:
        name = (ch.get("name") or "").strip()
        if not name:
            continue
        key = name.lower()
        if key not in combined_map:
            combined_map[key] = dict(ch)
        else:
            existing = combined_map[key]
            if len(str(ch.get("description", ""))) > len(str(existing.get("description", ""))):
                combined_map[key] = {**existing, **{k: v for k, v in ch.items() if v}}
            else:
                for k, v in ch.items():
                    if v and not existing.get(k):
                        existing[k] = v
    combined = list(combined_map.values())

    # Attempt one final LLM merge pass only if the payload is small enough.
    final_data = json.dumps(combined, ensure_ascii=False)
    SAFE_FINAL_BYTES = 6_000
    if len(final_data) <= SAFE_FINAL_BYTES:
        final_meta: dict = {}
        final_cfg = _cfg_with_child_session(cfg, f"d{depth}-final")
        final_result = await _llm_extract(
            _MERGE_CHARACTERS_PROMPT.format(data=final_data), final_cfg, _meta=final_meta,
        )
        if not final_meta.get("truncated"):
            final_merged = final_result if isinstance(final_result, list) else final_result.get("characters", combined)
            logger.debug(
                "_llm_merge_chars_batch: depth=%d final-pass %d → %d",
                depth, len(combined), len(final_merged),
            )
            return final_merged

    logger.info(
        "_llm_merge_chars_batch: returning python-deduped result (%d entries) after recursive split at depth=%d",
        len(combined), depth,
    )
    return combined


async def _merge_characters_chunked(all_chars: list, cfg: LLMConfig) -> list:
    """Merge characters in multiple LLM passes without data loss.

    Strategy:
    1. Python exact-name dedup (keep richest entry per name).
    2. Pack into batches of ≤ MAX_BATCH_BYTES JSON bytes.
    3. LLM-merge each batch via _llm_merge_chars_batch (recursive split on
       finish_reason=length — no partial data loss on token-limit truncation).
    4. Final LLM merge pass over all condensed batch results.
    """
    if not all_chars:
        return []

    # Step 1: Python dedup by exact name (case-insensitive). Keep the entry with
    # the richest description; absorb non-empty fields from duplicates.
    name_map: dict[str, dict] = {}
    for ch in all_chars:
        name = (ch.get("name") or "").strip()
        if not name:
            continue
        key = name.lower()
        if key not in name_map:
            name_map[key] = dict(ch)
        else:
            existing = name_map[key]
            if len(str(ch.get("description", ""))) > len(str(existing.get("description", ""))):
                name_map[key] = {**existing, **{k: v for k, v in ch.items() if v}}
            else:
                for k, v in ch.items():
                    if v and not existing.get(k):
                        existing[k] = v

    deduped = list(name_map.values())
    logger.info("merge_characters: %d raw → %d after python name-dedup", len(all_chars), len(deduped))

    if not deduped:
        return []

    # Step 2: Pack into batches capped at MAX_BATCH_BYTES.
    MAX_BATCH_BYTES = 8_000
    batches: list[list[dict]] = []
    current_batch: list[dict] = []
    current_size = 0
    for ch in deduped:
        ch_json = json.dumps(ch, ensure_ascii=False)
        if current_batch and current_size + len(ch_json) > MAX_BATCH_BYTES:
            batches.append(current_batch)
            current_batch = [ch]
            current_size = len(ch_json)
        else:
            current_batch.append(ch)
            current_size += len(ch_json)
    if current_batch:
        batches.append(current_batch)

    logger.info("merge_characters: %d deduped entries → %d LLM batch(es)", len(deduped), len(batches))

    # Step 3: LLM-merge each batch with automatic recursive splitting on truncation.
    # Add delay between batches to avoid API rate limiting.
    batch_results: list[dict] = []
    for i, batch in enumerate(batches):
        if i > 0:
            await asyncio.sleep(3.0)
        logger.info("merge_characters: LLM batch %d/%d (%d entries)", i + 1, len(batches), len(batch))
        batch_cfg = _cfg_with_child_session(cfg, f"batch-{i + 1}")
        merged = await _llm_merge_chars_batch(batch, batch_cfg)
        if merged:
            batch_results.extend(merged)
        else:
            logger.warning("merge_characters: batch %d/%d returned empty result, keeping original entries", i + 1, len(batches))
            batch_results.extend(batch)

    if len(batches) == 1:
        return _sort_by_importance(batch_results)

    # Step 4: Final merge pass over all condensed batch results.
    if len(batch_results) < 20:
        logger.info(
            "merge_characters: %d condensed entries (< 20), using python-level dedup only",
            len(batch_results),
        )
        final_map: dict[str, dict] = {}
        for ch in batch_results:
            name = (ch.get("name") or "").strip()
            if not name:
                continue
            key = name.lower()
            if key not in final_map:
                final_map[key] = dict(ch)
            else:
                existing = final_map[key]
                if len(str(ch.get("description", ""))) > len(str(existing.get("description", ""))):
                    final_map[key] = {**existing, **{k: v for k, v in ch.items() if v}}
                else:
                    for k, v in ch.items():
                        if v and not existing.get(k):
                            existing[k] = v
        return _sort_by_importance(list(final_map.values()))

    logger.info(
        "merge_characters: final LLM merge pass over %d entries from %d batches",
        len(batch_results), len(batches),
    )
    final_cfg = _cfg_with_child_session(cfg, "final")
    return _sort_by_importance(await _llm_merge_chars_batch(batch_results, final_cfg))


# ── Simple dedup helpers ──────────────────────────────────────────────────────

def _dedup_outline(nodes: list) -> list:
    """Deduplicate outline nodes by title (exact match + simple prefix).  Preserves order."""
    seen: set[str] = set()
    result = []
    for node in nodes:
        title = (node.get("title") or "").strip()
        if not title:
            continue
        key = title[:20].lower()  # first 20 chars as dedup key
        if key not in seen:
            seen.add(key)
            result.append(node)
    return result


def _dedup_glossary(terms: list) -> list:
    """Deduplicate glossary terms by term name (case-insensitive).  Preserves order."""
    seen: set[str] = set()
    result = []
    for t in terms:
        key = (t.get("term") or "").strip().lower()
        if key and key not in seen:
            seen.add(key)
            result.append(t)
    return result


def _dedup_foreshadowings(items: list) -> list:
    """Deduplicate foreshadowings by first 30 chars of content."""
    seen: set[str] = set()
    result = []
    for f in items:
        key = (f.get("content") or "")[:30].strip().lower()
        if key and key not in seen:
            seen.add(key)
            result.append(f)
    return result
