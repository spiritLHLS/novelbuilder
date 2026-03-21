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

logger = logging.getLogger("python-agent")

router = APIRouter()

# ── Request models ────────────────────────────────────────────────────────────

class LLMConfig(BaseModel):
    api_key: str = ""
    base_url: str = "https://api.openai.com/v1"
    model: str = "gpt-4o"
    max_tokens: int = 4096
    temperature: float = 0.4   # lower for extraction tasks
    rpm_limit: int = 0         # 0 = unlimited
    omit_max_tokens: bool = False   # skip max_tokens field (some providers reject it)
    omit_temperature: bool = False  # skip temperature field
    api_style: str = "chat_completions"  # or "responses" (OpenAI Responses API)

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

async def _llm_extract(prompt: str, cfg: LLMConfig, max_retries: int = 4) -> dict:
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

    system_msg = "你是一位专业的中文小说分析专家，擅长提取人物、世界观和情节大纲。"
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

    delay = 2.0
    async with httpx.AsyncClient(timeout=120.0) as client:
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
                    label = {400: "Bad Request (check model name)", 401: "Unauthorized (invalid API key)", 403: "Forbidden (API key rejected by gateway)"}
                    logger.error(
                        "LLM HTTP %d %s \u2014 will not retry | body: %s",
                        resp.status_code, label.get(resp.status_code, ""), body_snippet,
                    )
                    break
                resp.raise_for_status()
                data = resp.json()
                # Extract text from response depending on API style
                if use_responses_api:
                    # Responses API: data["output"][0]["content"][0]["text"]
                    raw = data["output"][0]["content"][0]["text"]
                else:
                    raw = data["choices"][0]["message"]["content"]
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
                logger.warning(
                    "LLM empty/invalid JSON attempt %d/%d: %s",
                    attempt, max_retries, repr(exc),
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
    """Strip markdown fences, repair truncated JSON, return dict."""
    raw = raw.strip()
    if raw.startswith("```"):
        raw = re.sub(r"^```[a-z]*\n?", "", raw)
        raw = re.sub(r"\n?```$", "", raw.strip())
    raw = raw.strip()
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        # Try to close unclosed structures from truncated output
        raw += "]" * (raw.count("[") - raw.count("]"))
        raw += "}" * (raw.count("{") - raw.count("}"))
        try:
            return json.loads(raw)
        except Exception:
            return {}


# ── Chunk analysis ────────────────────────────────────────────────────────────

_CHUNK_PROMPT = """请从以下小说片段中提取结构化信息，严格按照以下JSON格式返回：

{{
  "characters": [
    {{
      "name": "人物姓名",
      "role": "主角/配角/反派/其他",
      "description": "人物简介（50字以内）",
      "traits": ["性格特点1", "性格特点2"]
    }}
  ],
  "world": {{
    "setting": "世界背景描述（100字以内）",
    "time_period": "时代背景",
    "locations": ["场景地点1", "场景地点2"],
    "systems": ["体系1（如修炼体系、魔法体系等）"]
  }},
  "outline": [
    {{
      "level": "macro",
      "title": "卷/篇/大情节标题",
      "summary": "概要（30字以内）"
    }},
    {{
      "level": "meso",
      "title": "章节/情节节点标题",
      "summary": "概要（30字以内）"
    }},
    {{
      "level": "micro",
      "title": "场景/细节标题",
      "summary": "概要（20字以内）"
    }}
  ],
  "glossary": [
    {{
      "term": "专有名词/术语",
      "definition": "解释（30字以内）",
      "category": "character/place/item/concept/other"
    }}
  ],
  "foreshadowings": [
    {{
      "content": "伏笔描述（50字以内）",
      "related_characters": ["相关角色名"],
      "priority": 3
    }}
  ]
}}

提取规则：
1. 只提取本片段中明确出现的信息，不要推测
2. characters：最多10个重要人物
3. outline：macro级别最多5条（大情节/卷级），meso级别最多20条（章节级），micro级别最多10条（场景级）
4. glossary：本片段出现的专有名词，最多15条
5. foreshadowings：本片段中设置的伏笔，最多5条
6. 严格返回JSON，不要任何其他文字

小说片段（第{chunk_index}/{total_chunks}段）：
{text}
"""


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

    return {
        "job_id": req.job_id,
        "chunk_index": req.chunk_index,
        "characters": result.get("characters", []),
        "world": result.get("world", {}),
        "outline": result.get("outline", []),
        "glossary": result.get("glossary", []),
        "foreshadowings": result.get("foreshadowings", []),
    }


# ── Merge ─────────────────────────────────────────────────────────────────────

_MERGE_CHARACTERS_PROMPT = """以下是从同一部小说不同章节片段提取的人物列表（JSON数组），
请合并去重，整合同一人物的信息，返回精炼后的人物列表JSON数组，格式：
[{{"name":"...","role":"...","description":"...","traits":["..."]}}]

原始数据：
{data}

要求：
1. 同名或明显相同的人物合并为一条，description和traits取并集最全版本
2. 最终保留重要人物，至多30个
3. 只返回JSON数组，不要其他文字
"""

_MERGE_WORLD_PROMPT = """以下是从同一部小说不同章节片段提取的世界观信息（JSON数组），
请整合所有信息，返回一个统一的世界观JSON对象：
{{"setting":"...","time_period":"...","locations":["..."],"systems":["..."]}}

原始数据：
{data}

要求：
1. setting综合所有描述，100字以内
2. locations和systems取所有片段的并集并去重
3. 只返回JSON对象，不要其他文字
"""

_MERGE_GLOSSARY_PROMPT = """以下是从同一部小说不同章节片段提取的术语列表（JSON数组），
请合并去重，返回精炼后的术语列表JSON数组，格式：
[{{"term":"...","definition":"...","category":"..."}}]

原始数据：
{data}

要求：
1. 同名或明显相同的术语合并为一条，definition取最完整版本
2. 最终保留所有不重复术语，最多200条
3. 只返回JSON数组，不要其他文字
"""

_MERGE_FORESHADOWINGS_PROMPT = """以下是从同一部小说不同章节片段提取的伏笔列表（JSON数组），
请整合去重，返回精炼后的伏笔列表JSON数组，格式：
[{{"content":"...","related_characters":["..."],"priority":3}}]

原始数据：
{data}

要求：
1. 内容明显重复的伏笔合并为一条
2. 最终保留重要伏笔，最多100条
3. 只返回JSON数组，不要其他文字
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
    return result[:200]


def _dedup_foreshadowings(items: list) -> list:
    """Deduplicate foreshadowings by first 30 chars of content."""
    seen: set[str] = set()
    result = []
    for f in items:
        key = (f.get("content") or "")[:30].strip().lower()
        if key and key not in seen:
            seen.add(key)
            result.append(f)
    return result[:100]


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
    final_chars = chars_result if isinstance(chars_result, list) else chars_result.get("characters", all_chars[:30])
    final_world = world_result if isinstance(world_result, dict) else (all_worlds[0] if all_worlds else {})

    # Outline: Python dedup — preserve all nodes up to 300, ordered by level (macro first)
    deduped_outline = _dedup_outline(all_outlines)
    # Sort: macro first, then meso, then micro (preserving relative order within each level)
    level_order = {"macro": 0, "meso": 1, "micro": 2}
    macro = [n for n in deduped_outline if n.get("level") == "macro"]
    meso  = [n for n in deduped_outline if n.get("level") == "meso"]
    micro = [n for n in deduped_outline if n.get("level") == "micro"]
    final_outline = macro[:50] + meso[:200] + micro[:100]

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
