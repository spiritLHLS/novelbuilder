"""
deep_analysis_models.py — Shared Pydantic models and small helper utilities
used by the deep-analysis pipeline.
"""
from __future__ import annotations

from typing import Optional

from pydantic import BaseModel


# ── Request / response models ─────────────────────────────────────────────────

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


# ── Small utilities shared across sub-modules ─────────────────────────────────

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


def _split_delimited(s: str, sep: str = ",") -> list[str]:
    """Split a delimited string on both Chinese and English comma or semicolon variants.

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
