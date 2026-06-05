"""Rate limiting and retry wrappers for LangChain LLM instances."""
from __future__ import annotations

import asyncio
import math
import threading
import time
from typing import Any


class LLMWrapper:
    """Proxy a LangChain LLM while overriding invoke/ainvoke.

    LangChain models are often Pydantic objects, so assigning methods directly
    can fail under Pydantic v2. The wrapper keeps the delegate intact.
    """

    def __init__(self, delegate: Any, invoke_fn: Any, ainvoke_fn: Any):
        self._delegate = delegate
        self._invoke_fn = invoke_fn
        self._ainvoke_fn = ainvoke_fn

    def invoke(self, input: Any, config: Any = None, **kwargs: Any) -> Any:
        return self._invoke_fn(input, config=config, **kwargs)

    async def ainvoke(self, input: Any, config: Any = None, **kwargs: Any) -> Any:
        return await self._ainvoke_fn(input, config=config, **kwargs)

    def __getattr__(self, name: str) -> Any:
        return getattr(self._delegate, name)


def is_max_tokens_unsupported(exc: Exception) -> bool:
    """Detect "max_tokens is not supported, use max_completion_tokens" errors."""
    msg = str(exc)
    return "max_tokens" in msg and "max_completion_tokens" in msg


def apply_max_completion_tokens_fallback(primary: Any, fallback: Any) -> Any:
    """Retry through fallback when the provider rejects max_tokens."""
    orig_invoke = primary.invoke
    orig_ainvoke = primary.ainvoke

    def invoke(input: Any, config: Any = None, **kwargs: Any) -> Any:
        try:
            return orig_invoke(input, config=config, **kwargs)
        except Exception as exc:
            if is_max_tokens_unsupported(exc):
                return fallback.invoke(input, config=config, **kwargs)
            raise

    async def ainvoke(input: Any, config: Any = None, **kwargs: Any) -> Any:
        try:
            return await orig_ainvoke(input, config=config, **kwargs)
        except Exception as exc:
            if is_max_tokens_unsupported(exc):
                return await fallback.ainvoke(input, config=config, **kwargs)
            raise

    return LLMWrapper(primary, invoke, ainvoke)


# Async state: key -> {"lock": asyncio.Lock, "timestamps": list[float]}
_rpm_async_state: dict[str, dict[str, Any]] = {}
_rpm_sync_timestamps: dict[str, list[float]] = {}
_rpm_sync_locks: dict[str, threading.Lock] = {}

# Token state stores (timestamp, token_charge) entries in a 60-second window.
_tpm_async_state: dict[str, dict[str, Any]] = {}
_tpm_sync_usage: dict[str, list[tuple[float, int]]] = {}
_tpm_sync_locks: dict[str, threading.Lock] = {}


def _text_token_estimate(text: str) -> int:
    """Conservative local token estimate without tokenizer dependencies."""
    if not text:
        return 0
    cjk = 0
    other = 0
    for ch in text:
        if ch.isspace():
            continue
        code = ord(ch)
        if (
            0x4E00 <= code <= 0x9FFF
            or 0x3400 <= code <= 0x4DBF
            or 0x3040 <= code <= 0x30FF
            or 0xAC00 <= code <= 0xD7AF
        ):
            cjk += 1
        else:
            other += 1
    return cjk + math.ceil(other / 4)


def estimate_tokens(value: Any, max_output_tokens: int = 0) -> int:
    """Estimate request token charge from common LangChain/OpenAI payload shapes."""
    total = max(0, int(max_output_tokens or 0))
    if value is None:
        return max(total, 1)
    if isinstance(value, str):
        return max(total + _text_token_estimate(value), 1)
    if isinstance(value, dict):
        for key in ("content", "text", "value", "input"):
            if key in value:
                total += estimate_tokens(value[key], 0)
        return max(total, 1)
    if isinstance(value, (list, tuple)):
        for item in value:
            total += estimate_tokens(item, 0)
        return max(total, 1)

    content = getattr(value, "content", None)
    if content is not None:
        total += estimate_tokens(content, 0)
    else:
        total += _text_token_estimate(str(value))
    return max(total, 1)


def _clamp_token_charge(tpm_limit: int, token_count: int) -> int:
    if tpm_limit <= 0:
        return 0
    # A single oversized prompt should not deadlock forever; count it as a full
    # minute's budget and let the provider enforce its hard context limit.
    return max(1, min(int(token_count or 1), tpm_limit))


async def rate_limit_async(key: str, rpm_limit: int) -> None:
    """Enforce rpm_limit req/min using a 60-second sliding window."""
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


async def rate_limit_tokens_async(key: str, tpm_limit: int, token_count: int) -> None:
    """Enforce tpm_limit tokens/min using a 60-second sliding window."""
    charge = _clamp_token_charge(tpm_limit, token_count)
    if charge <= 0:
        return
    if key not in _tpm_async_state:
        _tpm_async_state[key] = {"lock": asyncio.Lock(), "usage": []}
    state = _tpm_async_state[key]
    async with state["lock"]:
        while True:
            now = time.monotonic()
            cutoff = now - 60.0
            state["usage"] = [(ts, n) for ts, n in state["usage"] if ts >= cutoff]
            used = sum(n for _, n in state["usage"])
            if used + charge <= tpm_limit:
                state["usage"].append((now, charge))
                return
            oldest_ts = state["usage"][0][0] if state["usage"] else now
            wait_secs = oldest_ts + 60.0 - now + 0.05
            await asyncio.sleep(max(wait_secs, 0.05))


def rate_limit_sync(key: str, rpm_limit: int) -> None:
    """Synchronous counterpart to rate_limit_async."""
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
        time.sleep(max(wait_secs, 0.05))


def rate_limit_tokens_sync(key: str, tpm_limit: int, token_count: int) -> None:
    """Synchronous counterpart to rate_limit_tokens_async."""
    charge = _clamp_token_charge(tpm_limit, token_count)
    if charge <= 0:
        return
    if key not in _tpm_sync_locks:
        _tpm_sync_locks[key] = threading.Lock()
    lock = _tpm_sync_locks[key]
    while True:
        with lock:
            if key not in _tpm_sync_usage:
                _tpm_sync_usage[key] = []
            usage = _tpm_sync_usage[key]
            now = time.monotonic()
            cutoff = now - 60.0
            _tpm_sync_usage[key] = [(ts, n) for ts, n in usage if ts >= cutoff]
            usage = _tpm_sync_usage[key]
            used = sum(n for _, n in usage)
            if used + charge <= tpm_limit:
                usage.append((now, charge))
                return
            oldest_ts = usage[0][0] if usage else now
            wait_secs = oldest_ts + 60.0 - now + 0.05
        time.sleep(max(wait_secs, 0.05))


def apply_llm_limits(llm: Any, key: str, rpm_limit: int, tpm_limit: int, max_output_tokens: int = 0) -> Any:
    """Wrap invoke/ainvoke to enforce per-profile RPM and TPM throttling."""
    orig_invoke = llm.invoke
    orig_ainvoke = llm.ainvoke

    def invoke(input: Any, config: Any = None, **kwargs: Any) -> Any:
        if tpm_limit > 0:
            rate_limit_tokens_sync(key, tpm_limit, estimate_tokens(input, max_output_tokens))
        if rpm_limit > 0:
            rate_limit_sync(key, rpm_limit)
        return orig_invoke(input, config=config, **kwargs)

    async def ainvoke(input: Any, config: Any = None, **kwargs: Any) -> Any:
        if tpm_limit > 0:
            await rate_limit_tokens_async(key, tpm_limit, estimate_tokens(input, max_output_tokens))
        if rpm_limit > 0:
            await asyncio.wait_for(rate_limit_async(key, rpm_limit), timeout=61.0)
        return await orig_ainvoke(input, config=config, **kwargs)

    return LLMWrapper(llm, invoke, ainvoke)


def apply_rpm_limit(llm: Any, key: str, rpm_limit: int) -> Any:
    """Backward-compatible RPM-only wrapper."""
    return apply_llm_limits(llm, key, rpm_limit, 0, 0)
