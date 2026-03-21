"""Shared LLM builder: dispatches to the correct LangChain model class based on api_style."""
from __future__ import annotations

import asyncio
import os
import re
import threading
import time

# Map legacy api_style strings to the canonical path-based values.
_LEGACY: dict[str, str] = {
    "chat_completions": "/chat/completions",
    "claude":           "/messages",
    "responses":        "/responses",
}

# Models that require max_completion_tokens instead of max_tokens (e.g. o1, o3, o4-mini).
_O_SERIES_RE = re.compile(r"\bo[1-9](-mini|-preview|-pro)?\b", re.IGNORECASE)


def _needs_completion_tokens(model: str, cfg: dict) -> bool:
    """Return True when max_completion_tokens should be used instead of max_tokens."""
    return cfg.get("use_max_completion_tokens", False) or bool(_O_SERIES_RE.search(model))


def _is_max_tokens_unsupported(exc: Exception) -> bool:
    """Detect the specific 400 error: 'max_tokens is not supported, use max_completion_tokens'."""
    msg = str(exc)
    return "max_tokens" in msg and "max_completion_tokens" in msg


def _apply_max_completion_tokens_fallback(primary, fallback):
    """Patch invoke/ainvoke on *primary* to transparently retry via *fallback* on the token-param error."""
    _orig_invoke = primary.invoke
    _orig_ainvoke = primary.ainvoke

    def _invoke(input, config=None, **kwargs):
        try:
            return _orig_invoke(input, config=config, **kwargs)
        except Exception as exc:
            if _is_max_tokens_unsupported(exc):
                return fallback.invoke(input, config=config, **kwargs)
            raise

    async def _ainvoke(input, config=None, **kwargs):
        try:
            return await _orig_ainvoke(input, config=config, **kwargs)
        except Exception as exc:
            if _is_max_tokens_unsupported(exc):
                return await fallback.ainvoke(input, config=config, **kwargs)
            raise

    primary.invoke = _invoke
    primary.ainvoke = _ainvoke
    return primary


# ── Per-profile sliding-window rate limiter ───────────────────────────────────
# Async state: key → {"lock": asyncio.Lock, "timestamps": list[float]}
_rpm_async_state: dict[str, dict] = {}
# Sync state: separate timestamps dict + threading locks
_rpm_sync_timestamps: dict[str, list[float]] = {}
_rpm_sync_locks: dict[str, threading.Lock] = {}


async def _rate_limit_async(key: str, rpm_limit: int) -> None:
    """Enforce rpm_limit req/min using a 60-second sliding window (async version)."""
    if rpm_limit <= 0:
        return
    if key not in _rpm_async_state:
        _rpm_async_state[key] = {"lock": asyncio.Lock(), "timestamps": []}
    state = _rpm_async_state[key]
    async with state["lock"]:
        while True:
            now = time.monotonic()
            cutoff = now - 60.0
            state["timestamps"] = [t for t in state["timestamps"] if t >= cutoff]
            if len(state["timestamps"]) < rpm_limit:
                state["timestamps"].append(now)
                return
            wait_secs = state["timestamps"][0] + 60.0 - now + 0.05
            await asyncio.sleep(max(wait_secs, 0.05))


def _rate_limit_sync(key: str, rpm_limit: int) -> None:
    """Enforce rpm_limit req/min using a 60-second sliding window (sync version)."""
    if rpm_limit <= 0:
        return
    if key not in _rpm_sync_locks:
        _rpm_sync_locks[key] = threading.Lock()
    lock = _rpm_sync_locks[key]
    while True:
        with lock:
            if key not in _rpm_sync_timestamps:
                _rpm_sync_timestamps[key] = []
            ts = _rpm_sync_timestamps[key]
            now = time.monotonic()
            cutoff = now - 60.0
            _rpm_sync_timestamps[key] = [t for t in ts if t >= cutoff]
            ts = _rpm_sync_timestamps[key]
            if len(ts) < rpm_limit:
                ts.append(now)
                return
            wait_secs = ts[0] + 60.0 - now + 0.05
        # Release lock before sleeping so other threads can check state
        time.sleep(max(wait_secs, 0.05))


def _apply_rpm_limit(llm, key: str, rpm_limit: int):
    """Wrap a LangChain model's invoke/ainvoke to enforce RPM rate limiting."""
    _orig_invoke = llm.invoke
    _orig_ainvoke = llm.ainvoke

    def _invoke(input, config=None, **kwargs):
        _rate_limit_sync(key, rpm_limit)
        return _orig_invoke(input, config=config, **kwargs)

    async def _ainvoke(input, config=None, **kwargs):
        await _rate_limit_async(key, rpm_limit)
        return await _orig_ainvoke(input, config=config, **kwargs)

    llm.invoke = _invoke
    llm.ainvoke = _ainvoke
    return llm


def build_llm(
    cfg: dict,
    default_temperature: float = 0.7,
    default_max_tokens: int = 4096,
    streaming: bool = False,
):
    """Return the appropriate LangChain chat model for the given LLM profile config.

    Dispatch table (based on cfg['api_style']):
      - ends with '/messages'  → ChatAnthropic
      - == 'gemini'            → ChatGoogleGenerativeAI
      - anything else          → ChatOpenAI (OpenAI-compatible / Responses fallback)
    """
    api_key  = cfg.get("api_key") or os.getenv("OPENAI_API_KEY", "")
    model    = cfg.get("model", "gpt-4o")
    base_url = (cfg.get("base_url") or "").rstrip("/")
    raw_style = cfg.get("api_style", "")
    api_style = _LEGACY.get(raw_style, raw_style)

    omit_temperature = cfg.get("omit_temperature", False)
    omit_max_tokens  = cfg.get("omit_max_tokens", False)

    rpm_limit = int(cfg.get("rpm_limit", 0) or 0)
    # Key uses the original base_url (before any SDK-specific stripping) + model.
    rpm_key = f"{base_url}|{model}"

    def _maybe_rpm(llm):
        """Apply sliding-window RPM limiter if configured."""
        if rpm_limit > 0:
            return _apply_rpm_limit(llm, rpm_key, rpm_limit)
        return llm

    # ── Anthropic ─────────────────────────────────────────────────────────────
    if api_style.endswith("/messages"):
        from langchain_anthropic import ChatAnthropic

        kwargs: dict = {"model": model, "api_key": api_key}

        # The Anthropic SDK base_url must NOT include the /messages suffix.
        if base_url:
            full = base_url + api_style          # e.g. "https://…/v1/messages"
            sdk_base = full.removesuffix("/messages")  # "https://…/v1"
            if sdk_base:
                kwargs["base_url"] = sdk_base

        if not omit_temperature:
            kwargs["temperature"] = float(cfg.get("temperature", default_temperature))
        if not omit_max_tokens:
            kwargs["max_tokens"] = int(cfg.get("max_tokens", default_max_tokens))
        return _maybe_rpm(ChatAnthropic(**kwargs))

    # ── Google Gemini ─────────────────────────────────────────────────────────
    if api_style == "gemini":
        from langchain_google_genai import ChatGoogleGenerativeAI

        kwargs = {"model": model, "google_api_key": api_key}
        if not omit_temperature:
            kwargs["temperature"] = float(cfg.get("temperature", default_temperature))
        if not omit_max_tokens:
            kwargs["max_output_tokens"] = int(cfg.get("max_tokens", default_max_tokens))
        return _maybe_rpm(ChatGoogleGenerativeAI(**kwargs))

    # ── OpenAI / OpenAI-compatible (default) ──────────────────────────────────
    from langchain_openai import ChatOpenAI

    # For path-based api_style (e.g. "/chat/completions", "/v1/chat/completions"),
    # compute the SDK-compatible base URL by stripping the /chat/completions suffix.
    if base_url and api_style:
        full = base_url + api_style
        if full.endswith("/chat/completions"):
            base_url = full.removesuffix("/chat/completions")

    tokens_val = int(cfg.get("max_tokens", default_max_tokens))
    use_completion_tokens = _needs_completion_tokens(model, cfg)

    kwargs = {"api_key": api_key, "model": model, "streaming": streaming}
    if base_url:
        kwargs["base_url"] = base_url
    if not omit_temperature:
        kwargs["temperature"] = float(cfg.get("temperature", default_temperature))
    if not omit_max_tokens:
        if use_completion_tokens:
            kwargs["max_completion_tokens"] = tokens_val
        else:
            kwargs["max_tokens"] = tokens_val

    primary = ChatOpenAI(**kwargs)

    # For models where we used max_tokens (unknown/generic models), attach a dynamic
    # fallback: if the API responds with 400 "max_tokens not supported, use
    # max_completion_tokens", transparently rebuild and retry.
    if not omit_max_tokens and not use_completion_tokens:
        fallback_kwargs = {**kwargs}
        fallback_kwargs.pop("max_tokens", None)
        fallback_kwargs["max_completion_tokens"] = tokens_val
        fallback = ChatOpenAI(**fallback_kwargs)
        return _maybe_rpm(_apply_max_completion_tokens_fallback(primary, fallback))

    return _maybe_rpm(primary)
