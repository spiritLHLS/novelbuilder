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
import re
import time
from typing import Optional

import httpx
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

from json_repair import repair_json, is_response_complete

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

class ChunkAnalyzeRequest(BaseModel):
    job_id: str
    project_id: str
    chunk_text: str
    chunk_index: int
    total_chunks: int
    llm_config: Optional[LLMConfig] = None

class ChunkData(BaseModel):
    characters: list     # [{name, role, description, traits: []}]
    world: dict          # {setting, time_period, locations: [], systems: []}
    outline: list        # [{level, title, summary}]  level is "macro"/"meso"/"micro"
    glossary: list = []  # [{term, definition, category}]
    foreshadowings: list = []  # [{content, related_characters: [], priority}]

class MergeRequest(BaseModel):
    job_id: str
    project_id: str
    chunks: list[ChunkData]
    llm_config: Optional[LLMConfig] = None

# ── Per-profile sliding-window rate limiter ────────────────────────────────────

# Maps (base_url, model) → {"lock": asyncio.Lock, "timestamps": list[float]}
_rpm_state: dict[str, dict] = {}


async def _rate_limit(cfg: LLMConfig) -> None:
    """Enforce cfg.rpm_limit requests/minute using a 60-second sliding window.
    A lock serialises window inspection so only one coroutine enters the
    critical section at a time, preventing burst over-runs."""
    if cfg.rpm_limit <= 0:
        return
    key = f"{cfg.base_url}|{cfg.model}"
    if key not in _rpm_state:
        _rpm_state[key] = {"lock": asyncio.Lock(), "timestamps": []}
    state = _rpm_state[key]

    async with state["lock"]:
        while True:
            now = time.monotonic()
            cutoff = now - 60.0
            state["timestamps"] = [t for t in state["timestamps"] if t >= cutoff]

            if len(state["timestamps"]) < cfg.rpm_limit:
                state["timestamps"].append(now)
                return

            # Must wait until the oldest request falls outside the 60-second window.
            wait_secs = state["timestamps"][0] + 60.0 - now + 0.05
            logger.debug("RPM limit %d reached, waiting %.1fs", cfg.rpm_limit, wait_secs)
            await asyncio.sleep(max(wait_secs, 0.05))


# ── LLM helper (async, with built-in retry) ───────────────────────────────────

async def _llm_extract(prompt: str, cfg: LLMConfig, max_retries: int = 6) -> dict:
    """
    Call an OpenAI-compatible endpoint and return parsed JSON.
    Supports two API styles:
      - "chat_completions": POST {base_url}/chat/completions  (default)
      - "responses":        POST {base_url}/responses          (OpenAI Responses API)
    Retries up to max_retries times with exponential back-off on transient errors
    (429, 500, 502, 503, 504).  Returns {} on exhaustion.
    """
    api_key = cfg.api_key or os.getenv("OPENAI_API_KEY", "")
    if not api_key:
        logger.error("LLM extraction failed: no API key configured (set api_key in llm_config or OPENAI_API_KEY env var)")
        return {}
    base_url = cfg.base_url.rstrip("/")
    # Normalize: strip trailing /chat/completions that some provider UIs include in the URL.
    base_url = re.sub(r"/chat/completions$", "", base_url, flags=re.IGNORECASE)
    # Also strip /responses for the responses style.
    base_url = re.sub(r"/responses$", "", base_url, flags=re.IGNORECASE)
    headers = {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    }

    system_msg = "你是一位专业的中文小说分析专家，擅长提取人物、世界观和情节大纲。请严格以JSON格式输出结果，不要添加任何额外说明。"
    use_responses_api = cfg.api_style == "responses"

    if use_responses_api:
        # OpenAI Responses API format: POST /responses
        endpoint = f"{base_url}/responses"
        payload: dict = {
            "model": cfg.model,
            "input": [
                {
                    "role": "system",
                    "content": [{"type": "input_text", "text": system_msg}],
                },
                {
                    "role": "user",
                    "content": [{"type": "input_text", "text": prompt}],
                },
            ],
            "store": False,
        }
        if not cfg.omit_max_tokens:
            payload["max_output_tokens"] = cfg.max_tokens
    else:
        # Standard chat/completions format
        endpoint = f"{base_url}/chat/completions"
        payload = {
            "model": cfg.model,
            "messages": [
                {"role": "system", "content": system_msg},
                {"role": "user", "content": prompt},
            ],
        }
        if not cfg.omit_max_tokens:
            payload["max_tokens"] = cfg.max_tokens
        if not cfg.omit_temperature:
            payload["temperature"] = cfg.temperature
        if cfg.json_mode:
            payload["response_format"] = {"type": "json_object"}

    delay = 10.0
    http_timeout = httpx.Timeout(connect=30.0, read=float(cfg.timeout), write=30.0, pool=30.0)
    async with httpx.AsyncClient(timeout=http_timeout) as client:
        for attempt in range(1, max_retries + 1):
            try:
                await _rate_limit(cfg)
                resp = await client.post(endpoint, headers=headers, json=payload)
                if resp.status_code in (429, 500, 502, 503, 504):
                    try:
                        body_snippet = resp.text[:400]
                    except Exception:
                        body_snippet = "<unreadable>"
                    logger.warning(
                        "LLM HTTP %d attempt %d/%d, backing off %.1fs | body: %s",
                        resp.status_code, attempt, max_retries, delay, body_snippet,
                    )
                    await asyncio.sleep(delay + (0.5 * attempt))
                    delay = min(delay * 2, 60.0)
                    continue
                if resp.status_code in (400, 401, 403):
                    try:
                        body_snippet = resp.text[:400]
                    except Exception:
                        body_snippet = "<unreadable>"
                    # Special case: some models (e.g. o-series) reject max_tokens and
                    # require max_completion_tokens. Rebuild payload and retry once.
                    if (
                        resp.status_code == 400
                        and not use_responses_api
                        and "max_tokens" in payload
                        and "max_tokens" in body_snippet
                        and "max_completion_tokens" in body_snippet
                    ):
                        logger.warning(
                            "LLM HTTP 400: max_tokens unsupported, retrying with max_completion_tokens (attempt %d/%d)",
                            attempt, max_retries,
                        )
                        payload = {k: v for k, v in payload.items() if k != "max_tokens"}
                        payload["max_completion_tokens"] = cfg.max_tokens
                        continue
                    label = {400: "Bad Request (check model name)", 401: "Unauthorized (invalid API key)", 403: "Forbidden (API key rejected by gateway)"}
                    logger.error(
                        "LLM HTTP %d %s \u2014 will not retry | body: %s",
                        resp.status_code, label.get(resp.status_code, ""), body_snippet,
                    )
                    break
                resp.raise_for_status()
                resp_text = resp.text
                data = json.loads(resp_text)
                # Extract text from response depending on API style
                if use_responses_api:
                    # Responses API: data["output"][0]["content"][0]["text"]
                    raw = data["output"][0]["content"][0]["text"]
                    finish_reason = data.get("status", "unknown")
                else:
                    message = data["choices"][0]["message"]
                    raw = message.get("content") or ""
                    finish_reason = data["choices"][0].get("finish_reason", "unknown")
                    # Some providers surface thinking content separately; log it for tracing.
                    thinking = message.get("reasoning_content") or message.get("thinking") or ""
                    if thinking:
                        logger.debug(
                            "LLM thinking tokens on attempt %d/%d: %d chars (first 200): %.200s",
                            attempt, max_retries, len(thinking), thinking,
                        )

                logger.info(
                    "LLM raw response attempt %d/%d | finish_reason=%s | len=%d | first 500: %.500s",
                    attempt, max_retries, finish_reason, len(raw), raw,
                )

                # Retry immediately on empty content (content filter, null, etc.)
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

                # finish_reason="length" means the model hit token limit → truncated output
                # Try to repair and use the partial response before retrying
                if finish_reason == "length":
                    logger.warning(
                        "LLM finish_reason=length (truncated) on attempt %d/%d, "
                        "trying json_repair on partial response (len=%d)",
                        attempt, max_retries, len(raw),
                    )
                    try:
                        return _parse_json(raw)
                    except Exception:
                        pass
                    if attempt < max_retries:
                        logger.warning("json_repair failed, retrying attempt %d/%d", attempt, max_retries)
                        await asyncio.sleep(delay)
                        delay = min(delay * 1.5, 30.0)
                        continue

                # Heuristic completeness check (catches truncation not flagged by finish_reason)
                is_complete, issues = is_response_complete(raw)
                if not is_complete and attempt < max_retries:
                    logger.warning(
                        "LLM response heuristically incomplete on attempt %d/%d: %s. Retrying...",
                        attempt, max_retries, issues,
                    )
                    reduced_tokens = max(int(cfg.max_tokens * 0.75), 2048)
                    if use_responses_api:
                        payload["max_output_tokens"] = reduced_tokens
                    else:
                        payload["max_tokens"] = reduced_tokens
                    await asyncio.sleep(delay)
                    delay = min(delay * 1.5, 30.0)
                    continue

                return _parse_json(raw)
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
                raw_snippet = locals().get("resp_text", "<not captured>")[:500]
                logger.warning(
                    "LLM empty/invalid JSON attempt %d/%d: %s | raw_response: %s",
                    attempt, max_retries, repr(exc), raw_snippet,
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
4. world_locations和world_systems均为分号分隔的字符串
5. outline：macro为卷/大情节，meso为章节级，micro为场景级，每级不超过10条
6. outline.characters和foreshadowings.characters均为逗号分隔的字符串，不使用数组
7. glossary：提取最重要的专有名词，不超过20条
8. foreshadowings：提取最重要的伏笔，不超过10条
9. 严格返回JSON，不要任何其他文字，确保所有括号/引号完整闭合

小说片段（第{chunk_index}/{total_chunks}段）：
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


@router.post("/deep-analyze/chunk")
async def analyze_chunk(req: ChunkAnalyzeRequest):
    """Analyze a single chunk of text and return extracted entities."""
    cfg = req.llm_config or LLMConfig()
    prompt = _CHUNK_PROMPT.format(
        chunk_index=req.chunk_index + 1,
        total_chunks=req.total_chunks,
        text=req.chunk_text,  # chunk size is calibrated by Go based on model context window
    )

    logger.info("deep-analyze chunk %d/%d job=%s", req.chunk_index + 1, req.total_chunks, req.job_id)
    result = await _llm_extract(prompt, cfg)

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
        "characters": chars,
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
5. 只返回JSON数组，不要其他文字，确保所有括号/引号完整闭合
"""

_MERGE_WORLD_PROMPT = """以下是从同一部小说不同章节片段提取的世界观信息（JSON数组），
请整合所有信息，返回一个统一的世界观JSON对象：
{{"setting":"...","time_period":"...","locations":["..."],"systems":["..."]}}

原始数据：
{data}

要求：
1. setting综合所有描述，100字以内
2. locations取并集去重，不超过30条；systems取并集去重，不超过15条
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
            temperature=0.3,  # lower for merge/dedup task
            rpm_limit=req.llm_config.rpm_limit,
            omit_max_tokens=req.llm_config.omit_max_tokens,
            omit_temperature=req.llm_config.omit_temperature,
            api_style=req.llm_config.api_style,
        )
    else:
        cfg = LLMConfig(
            api_key=os.getenv("OPENAI_API_KEY", ""),
            base_url=os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
            model=os.getenv("OPENAI_MODEL", "gpt-4o"),
            max_tokens=4096,
            temperature=0.3,
        )

    chars_data = json.dumps(all_chars, ensure_ascii=False)[:8000]
    world_data = json.dumps(all_worlds, ensure_ascii=False)[:6000]

    # Characters and world use LLM for intelligent merging.
    # Outline, glossary and foreshadowings use fast Python dedup to avoid
    # token limits losing data from long novels.
    chars_result, world_result = await asyncio.gather(
        _llm_extract(_MERGE_CHARACTERS_PROMPT.format(data=chars_data), cfg),
        _llm_extract(_MERGE_WORLD_PROMPT.format(data=world_data), cfg),
    )

    # Normalize: merge_* prompts return arrays for chars, dict for world
    final_chars_raw = chars_result if isinstance(chars_result, list) else chars_result.get("characters", all_chars[:30])
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
