"""
deep_analysis_llm.py — Low-level LLM extraction helpers for the deep-analysis
pipeline.  Contains the async _llm_extract / _parse_json functions and the
chunk-analysis prompt template.
"""
from __future__ import annotations

import asyncio
import json
import logging
import os

import httpx

from json_repair import repair_json, is_response_complete
from llm_utils import ainvoke_text
from deep_analysis_models import LLMConfig

logger = logging.getLogger("python-agent")

# ── Chunk extraction prompt ───────────────────────────────────────────────────

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


# ── LLM extraction (async, with built-in retry) ───────────────────────────────

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
