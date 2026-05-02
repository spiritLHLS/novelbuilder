"""
routes_deep_analysis.py — chunked, background-aware deep analysis of large
reference novels (3 M+ characters).

Endpoints
---------
POST /deep-analyze/start
    { file_path, job_id, project_id, total_chunks, chunk_index, llm_config? }
    → Analyzes a single chunk and returns extracted entities for that chunk.
    Called repeatedly by the Go service (once per chunk) inside a task.

POST /deep-analyze/merge
    { job_id, project_id, chunks: [{characters, world, outline}] }
    → Merges all chunk extractions, deduplicates, returns final result.
    Called by Go service at the end of the task.

Design
------
* Each /deep-analyze/start call is stateless on the Python side.
* The Go service owns job state, progress, and retry scheduling.
* Exponential back-off is implemented BOTH in Go (caller) and implicitly
  here through httpx retries so that transient OpenAI errors are absorbed.
* The LLM config is forwarded per-call so the Go gateway routing (by agent type
  "reference_analyzer") is respected.

JSON Repair Strategy
--------------------
* Automatic repair of truncated/malformed JSON responses from LLM
* Multi-strategy approach: direct parse → strip fences → close unclosed → extract JSON-like
* Response completeness validation to detect truncation
* Automatic retry on transient failures (429, 5xx)
"""
from __future__ import annotations

import asyncio
import json
import logging
import os
from typing import Optional

import httpx
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

from json_repair import repair_json, is_response_complete
from llm_utils import ainvoke_text

logger = logging.getLogger("python-agent")

router = APIRouter()

# ── Request models ────────────────────────────────────────────────────────────

class LLMConfig(BaseModel):
    api_key: str = ""
    base_url: str = "https://api.openai.com/v1"
    model: str = "gpt-4o"
    max_tokens: int = 8000  # deepseek-chat max is 8K; use full capacity
    temperature: float = 0.4   # lower for extraction tasks
    rpm_limit: int = 0         # 0 = unlimited
    omit_max_tokens: bool = False   # skip max_tokens field (some providers reject it)
    omit_temperature: bool = False  # skip temperature field
    api_style: str = "chat_completions"  # or "responses" (OpenAI Responses API)
    timeout: int = 600          # seconds; thinking/reasoning models can take several minutes
    json_mode: bool = True      # add response_format={type:json_object}; disable for reasoning models
    session_id: str = ""       # stable task/session identifier propagated from the caller when available

class ChunkAnalyzeRequest(BaseModel):
    job_id: str
    project_id: str
    chunk_text: str
    chunk_index: int
    total_chunks: int
    llm_config: Optional[LLMConfig] = None
    prior_context: Optional[dict] = None  # {characters:[...], locations:[...], systems:[...], glossary:[...]}

class ChunkData(BaseModel):
    characters: list     # [{name, role, description, traits: []}]
    world: dict          # {setting, time_period, locations: [], systems: [], social_structure, core_conflict, factions, constitutions, forbidden_anchors}
    outline: list        # [{level, title, summary}]  level is "macro"/"meso"/"micro"
    glossary: list = []  # [{term, definition, category}]
    foreshadowings: list = []  # [{content, related_characters: [], priority}]

class MergeRequest(BaseModel):
    job_id: str
    project_id: str
    chunks: list[ChunkData]
    llm_config: Optional[LLMConfig] = None


def _child_session_id(session_id: str, suffix: str) -> str:
    base = str(session_id or "").strip()
    child = str(suffix or "").strip()
    if not base or not child:
        return base
    return f"{base}:{child}"


def _cfg_with_child_session(cfg: LLMConfig, suffix: str) -> LLMConfig:
    session_id = _child_session_id(cfg.session_id, suffix)
    if session_id == cfg.session_id:
        return cfg
    return cfg.model_copy(update={"session_id": session_id})

# ── LLM helper (async, with built-in retry) ───────────────────────────────────

async def _llm_extract(
    prompt: str,
    cfg: LLMConfig,
    max_retries: int = 6,
    _meta: dict | None = None,
    request_label: str = "reference_deep_analysis",
) -> dict:
    """
        Call the shared llm_utils routing layer and return parsed JSON.
    Retries up to max_retries times with exponential back-off on transient errors
    (429, 500, 502, 503, 504).  Returns {} on exhaustion.

    Optional _meta dict:  caller can pass an empty dict to receive metadata.
      _meta["truncated"] = True  when finish_reason=length (output cut short).
      _meta["partial"]   = <parsed dict/list>  the repaired partial result.
    """
    cfg_dict = cfg.model_dump()
    api_key = cfg_dict.get("api_key") or os.getenv("OPENAI_API_KEY", "")
    if not api_key:
        logger.error("LLM extraction failed: no API key configured (set api_key in llm_config or OPENAI_API_KEY env var)")
        return {}

    system_msg = "你是一位专业的中文小说分析专家，擅长提取人物、世界观和情节大纲。请严格以JSON格式输出结果，不要添加任何额外说明。"

    delay = 10.0
    for attempt in range(1, max_retries + 1):
        try:
            raw, response_meta = await ainvoke_text(
                prompt,
                cfg_dict,
                system_prompt=system_msg,
                session_id=cfg_dict.get("session_id") or None,
                task_name=request_label,
                extra_metadata={
                    "session_id": cfg_dict.get("session_id") or None,
                },
            )
            finish_reason = str(
                response_meta.get("finish_reason")
                or response_meta.get("status")
                or response_meta.get("stop_reason")
                or "unknown"
            )
            thinking = str(response_meta.get("reasoning_content") or "")
            if thinking:
                logger.debug(
                    "LLM thinking tokens on attempt %d/%d: %d chars (first 200): %.200s",
                    attempt, max_retries, len(thinking), thinking,
                )

            logger.info(
                "LLM raw response attempt %d/%d | finish_reason=%s | len=%d | first 500: %.500s",
                attempt, max_retries, finish_reason, len(raw), raw,
            )

            if not raw.strip():
                logger.warning(
                    "LLM returned empty content on attempt %d/%d (finish_reason=%s), retrying in %.1fs",
                    attempt, max_retries, finish_reason, delay,
                )
                if attempt < max_retries:
                    await asyncio.sleep(delay)
                    delay = min(delay * 2, 60.0)
                    continue
                break

            if finish_reason == "length":
                logger.warning(
                    "LLM finish_reason=length (truncated) on attempt %d/%d, "
                    "trying json_repair on partial response (len=%d)",
                    attempt, max_retries, len(raw),
                )
                if _meta is not None:
                    _meta["truncated"] = True
                try:
                    partial = _parse_json(raw)
                    if _meta is not None:
                        _meta["partial"] = partial
                    return partial
                except Exception:
                    pass
                if attempt < max_retries:
                    logger.warning("json_repair failed, retrying attempt %d/%d", attempt, max_retries)
                    await asyncio.sleep(delay)
                    delay = min(delay * 1.5, 30.0)
                    continue

            is_complete, issues = is_response_complete(raw)
            if not is_complete and attempt < max_retries:
                logger.warning(
                    "LLM response heuristically incomplete on attempt %d/%d: %s. Retrying...",
                    attempt, max_retries, issues,
                )
                if not cfg_dict.get("omit_max_tokens", False):
                    cfg_dict["max_tokens"] = max(int(int(cfg_dict.get("max_tokens", cfg.max_tokens)) * 0.75), 2048)
                await asyncio.sleep(delay)
                delay = min(delay * 1.5, 30.0)
                continue

            return _parse_json(raw)
        except httpx.HTTPStatusError as exc:
            status_code = exc.response.status_code if exc.response is not None else 0
            body_snippet = exc.response.text[:400] if exc.response is not None else "<unreadable>"
            if status_code in (429, 500, 502, 503, 504):
                logger.warning(
                    "LLM HTTP %d attempt %d/%d, backing off %.1fs | body: %s",
                    status_code, attempt, max_retries, delay, body_snippet,
                )
                await asyncio.sleep(delay + (0.5 * attempt))
                delay = min(delay * 2, 60.0)
                continue
            label = {400: "Bad Request (check model name)", 401: "Unauthorized (invalid API key)", 403: "Forbidden (API key rejected by gateway)"}
            logger.error(
                "LLM HTTP %d %s — will not retry | body: %s",
                status_code, label.get(status_code, ""), body_snippet,
            )
            break
        except (httpx.TimeoutException, httpx.NetworkError) as exc:
            logger.warning(
                "LLM network error attempt %d/%d: %s | cause: %s",
                attempt, max_retries,
                repr(exc),
                repr(exc.__cause__) if exc.__cause__ else "none",
            )
            if attempt == max_retries:
                break
            await asyncio.sleep(delay)
            delay = min(delay * 2, 60.0)
        except json.JSONDecodeError as exc:
            logger.warning(
                "LLM empty/invalid JSON attempt %d/%d: %s | raw_response: %.500s",
                attempt, max_retries, repr(exc), locals().get("raw", "<not captured>"),
            )
            if attempt == max_retries:
                break
            await asyncio.sleep(delay)
            delay = min(delay * 2, 60.0)
        except Exception as exc:
            logger.error("LLM unexpected error attempt %d/%d: %s", attempt, max_retries, repr(exc), exc_info=True)
            break
    logger.error("LLM extraction exhausted after %d attempts", max_retries)
    return {}


def _parse_json(raw: str) -> dict:
    """Strip markdown fences, repair truncated JSON, return dict.
    
    Uses intelligent repair strategies to handle:
    - Truncated responses
    - Incomplete nested structures
    - Unclosed strings and objects
    """
    # First check if response appears complete
    is_complete, issues = is_response_complete(raw)
    if not is_complete:
        logger.warning("Response appears incomplete: %s", issues)
    
    # Use advanced repair logic
    result = repair_json(raw)
    if not result:
        logger.warning(
            "LLM JSON parse failed after repair, returning empty dict | raw_content: %.1000s",
            raw,
        )
    return result


# ── Chunk analysis ────────────────────────────────────────────────────────────

_CHUNK_PROMPT = """请从以下小说片段中提取结构化信息，严格按照以下JSON格式返回：

{{
  "characters": [
    {{
      "name": "人物姓名",
      "role": "主角/配角/反派/其他",
      "description": "人物简介（50字以内）",
      "traits": "特点1，特点2，特点3",
      "motivation": "核心动机（30字以内）",
      "growth_arc": "成长简述（30字以内）",
      "relationships": "角色A：关系；角色B：关系"
    }}
  ],
  "world_setting": "世界背景（100字以内）",
  "world_time_period": "时代背景（20字以内）",
  "world_locations": "地点1；地点2；地点3",
  "world_systems": "体系1；体系2",
    "world_social_structure": "社会结构/秩序（50字以内）",
    "world_core_conflict": "世界层面的核心冲突（50字以内）",
    "world_factions": "势力1；势力2；势力3",
    "world_constitutions": [
        {{
            "type": "immutable",
            "rule": "不可变规则（30字以内）",
            "reason": "规则存在原因（20字以内）"
        }},
        {{
            "type": "mutable",
            "rule": "可变规则（30字以内）",
            "reason": "规则存在原因（20字以内）"
        }}
    ],
    "forbidden_anchors": ["绝不能出现的设定/禁忌1", "绝不能出现的设定/禁忌2"],
  "outline": [
    {{
      "level": "macro",
      "title": "大情节标题（15字以内）",
      "summary": "概要（30字以内）",
      "characters": "角色名1，角色名2"
    }},
    {{
      "level": "meso",
      "title": "章节标题（15字以内）",
      "summary": "概要（30字以内）",
      "characters": "角色名1"
    }},
    {{
      "level": "micro",
      "title": "场景标题（10字以内）",
      "summary": "概要（20字以内）",
      "characters": "角色名1"
    }}
  ],
  "glossary": [
    {{
      "term": "专有名词",
      "definition": "解释（30字以内）",
      "category": "character/place/item/concept/other"
    }}
  ],
  "foreshadowings": [
    {{
      "content": "伏笔描述（50字以内）",
      "characters": "相关角色名1，相关角色名2",
      "priority": 3
    }}
  ]
}}

提取规则：
1. 只提取本片段中明确出现的信息，不要推测
2. 每个字段严格控制在括号内的字数限制，确保JSON能够完整输出
3. traits为逗号分隔的字符串，relationships为"角色：关系"用分号分隔的字符串
4. world_locations、world_systems、world_factions均为分号分隔的字符串
5. world_constitutions只保留最重要的世界规则，最多8条，type只能是immutable或mutable
6. forbidden_anchors只提取文本中明确出现且确实被视为禁忌/铁律的元素，最多8条
7. outline：macro为卷/大情节，meso为章节级，micro为场景级，每级不超过10条
8. outline.characters和foreshadowings.characters均为逗号分隔的字符串，不使用数组
9. glossary：提取最重要的专有名词，不超过20条
10. foreshadowings：提取最重要的伏笔，不超过10条
11. 严格返回JSON，不要任何其他文字，确保所有括号/引号完整闭合

{prior_context_section}小说片段（第{chunk_index}/{total_chunks}段）：
{text}
"""


def _split_delimited(s: str, sep: str = ",") -> list[str]:
    """Split a delimted string on both Chinese and English comma or semicolon variants.

    sep="," → split on Chinese '，' and English ','
    sep=";" → split on Chinese '；' and English ';'
    """
    if not s:
        return []
    if sep in (",", "，"):
        normalized = s.replace("，", ",")
        parts = normalized.split(",")
    else:
        normalized = s.replace("；", ";")
        parts = normalized.split(";")
    return [p.strip() for p in parts if p.strip()]


def _ensure_string_list(value: object, sep: str = ",") -> list[str]:
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    if isinstance(value, str):
        return _split_delimited(value, sep)
    return []


def _normalize_constitutions(value: object) -> list[dict]:
    items = value if isinstance(value, list) else []
    normalized: list[dict] = []
    seen: set[tuple[str, str]] = set()
    for item in items:
        if not isinstance(item, dict):
            continue
        raw_type = str(item.get("type", "immutable")).strip().lower()
        rule_type = "mutable" if raw_type == "mutable" else "immutable"
        rule = str(item.get("rule", "")).strip()
        reason = str(item.get("reason", "")).strip()
        key = (rule_type, rule.lower())
        if not rule or key in seen:
            continue
        seen.add(key)
        normalized.append({"type": rule_type, "rule": rule, "reason": reason})
    return normalized


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


# ── Merge ─────────────────────────────────────────────────────────────────────

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


# ── Role importance helpers ──────────────────────────────────────────────────

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
    # Use a conservative 6 000-byte cap so the output fits in ≤ 4 K tokens on
    # any model (each condensed entry is ~150-300 bytes, so this is ~20-40 entries
    # which produce ~6-12 K tokens of output — well within budget).
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
    # 8 000 bytes keeps the prompt+data within a narrow enough window that the
    # condensed output (≈ 300 bytes/entry × ~25 entries = ~7 500 bytes ≈ 2 500
    # tokens) fits comfortably in the 8 K output budget of deepseek-chat.
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
    batch_results: list[dict] = []
    for i, batch in enumerate(batches):
        logger.info("merge_characters: LLM batch %d/%d (%d entries)", i + 1, len(batches), len(batch))
        batch_cfg = _cfg_with_child_session(cfg, f"batch-{i + 1}")
        merged = await _llm_merge_chars_batch(batch, batch_cfg)
        batch_results.extend(merged)

    if len(batches) == 1:
        return _sort_by_importance(batch_results)

    # Step 4: Final merge pass over all condensed batch results.
    # _llm_merge_chars_batch handles truncation recursively so no data is lost.
    logger.info(
        "merge_characters: final merge pass over %d entries from %d batches",
        len(batch_results), len(batches),
    )
    final_cfg = _cfg_with_child_session(cfg, "final")
    return _sort_by_importance(await _llm_merge_chars_batch(batch_results, final_cfg))


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


@router.post("/deep-analyze/merge")
async def merge_chunks(req: MergeRequest):
    """Merge per-chunk extractions into a unified, deduplicated result."""
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

    # Characters: multi-pass chunked merge (handles 300w+ novels with 100s of
    # unique characters without any truncation).
    # World: also multi-pass — locations/systems via Python set-union, settings via
    # batched LLM summarization — no hard byte cap anywhere.
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
    # Sort: macro first, then meso, then micro (preserving relative order within each level)
    level_order = {"macro": 0, "meso": 1, "micro": 2}
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
