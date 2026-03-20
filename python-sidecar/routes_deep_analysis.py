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

class ChunkAnalyzeRequest(BaseModel):
    job_id: str
    project_id: str
    chunk_text: str
    chunk_index: int
    total_chunks: int
    llm_config: Optional[LLMConfig] = None

class ChunkData(BaseModel):
    characters: list  # [{name, role, description, traits: []}]
    world: dict       # {setting, time_period, locations: [], systems: []}
    outline: list     # [{level, title, summary}]

class MergeRequest(BaseModel):
    job_id: str
    project_id: str
    chunks: list[ChunkData]
    llm_config: Optional[LLMConfig] = None

# ── LLM helper (async, with built-in retry) ───────────────────────────────────

async def _llm_extract(prompt: str, cfg: LLMConfig, max_retries: int = 4) -> dict:
    """
    Call an OpenAI-compatible chat completion endpoint and return parsed JSON.
    Retries up to max_retries times with exponential back-off on transient errors
    (429, 500, 502, 503, 504).  Returns {} on exhaustion so callers can continue
    with an empty result rather than failing the whole job.
    """
    api_key = cfg.api_key or os.getenv("OPENAI_API_KEY", "")
    if not api_key:
        logger.error("LLM extraction failed: no API key configured (set api_key in llm_config or OPENAI_API_KEY env var)")
        return {}
    base_url = cfg.base_url.rstrip("/")
    # Normalize: strip trailing /chat/completions that some provider UIs include in the URL.
    # Without this, the constructed URL becomes /v1/chat/completions/chat/completions (404).
    base_url = re.sub(r"/chat/completions$", "", base_url, flags=re.IGNORECASE)
    headers = {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    }
    payload = {
        "model": cfg.model,
        "messages": [
            {"role": "system", "content": "你是一位专业的中文小说分析专家，擅长提取人物、世界观和情节大纲。"},
            {"role": "user", "content": prompt},
        ],
        "max_tokens": cfg.max_tokens,
        "temperature": cfg.temperature,
    }

    delay = 2.0
    async with httpx.AsyncClient(timeout=120.0) as client:
        for attempt in range(1, max_retries + 1):
            try:
                resp = await client.post(f"{base_url}/chat/completions",
                                         headers=headers, json=payload)
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
                        "LLM HTTP %d %s — will not retry | body: %s",
                        resp.status_code, label.get(resp.status_code, ""), body_snippet,
                    )
                    break
                resp.raise_for_status()
                data = resp.json()
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

_CHUNK_PROMPT = """请从以下小说片段中提取结构化信息，以JSON格式返回，格式严格如下：

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
    "locations": ["场景1", "场景2"],
    "systems": ["体系1（如修炼体系、魔法体系等）"]
  }},
  "outline": [
    {{
      "level": 1,
      "title": "情节段落标题",
      "summary": "该段情节概要（50字以内）"
    }}
  ]
}}

注意事项：
1. 只提取本片段中明确出现的信息，不要推测
2. characters数组最多包含10个重要人物
3. outline按出现顺序列出主要情节节点，最多15条
4. 严格返回JSON，不要任何其他文字

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

_MERGE_OUTLINE_PROMPT = """以下是从同一部小说不同章节片段提取的情节大纲（按顺序，JSON数组），
请整理成一个连贯的多层大纲，返回JSON数组：
[{{"level":1,"title":"...","summary":"..."}}]

原始数据：
{data}

要求：
1. 按原始顺序排列，保留重要情节节点
2. 去除明显重复的情节点，最多保留50条
3. level=1表示主要情节，level=2表示子情节
4. 只返回JSON数组，不要其他文字
"""


@router.post("/deep-analyze/merge")
async def merge_chunks(req: MergeRequest):
    """Merge per-chunk extractions into a unified, deduplicated result."""
    all_chars = []
    all_worlds = []
    all_outlines = []

    for chunk in req.chunks:
        all_chars.extend(chunk.characters or [])
        if chunk.world:
            all_worlds.append(chunk.world)
        all_outlines.extend(chunk.outline or [])

    # Prefer the llm_config forwarded by the Go service (contains the user's configured key).
    # Fall back to environment variables only when no config is provided.
    if req.llm_config and req.llm_config.api_key:
        cfg = LLMConfig(
            api_key=req.llm_config.api_key,
            base_url=req.llm_config.base_url,
            model=req.llm_config.model,
            max_tokens=req.llm_config.max_tokens,
            temperature=0.3,  # lower for merge/dedup task
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
    outline_data = json.dumps(all_outlines, ensure_ascii=False)[:8000]

    chars_result, world_result, outline_result = await asyncio.gather(
        _llm_extract(_MERGE_CHARACTERS_PROMPT.format(data=chars_data), cfg),
        _llm_extract(_MERGE_WORLD_PROMPT.format(data=world_data), cfg),
        _llm_extract(_MERGE_OUTLINE_PROMPT.format(data=outline_data), cfg),
    )

    # Normalize: merge_* prompts return arrays for chars/outline, dict for world
    final_chars = chars_result if isinstance(chars_result, list) else chars_result.get("characters", all_chars[:30])
    final_world = world_result if isinstance(world_result, dict) else (all_worlds[0] if all_worlds else {})
    final_outline = outline_result if isinstance(outline_result, list) else outline_result.get("outline", all_outlines[:50])

    return {
        "job_id": req.job_id,
        "characters": final_chars,
        "world": final_world,
        "outline": final_outline,
    }
