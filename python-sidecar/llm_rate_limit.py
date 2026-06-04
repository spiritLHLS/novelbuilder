"""Rate limiting and retry wrappers for LangChain LLM instances."""
from __future__ import annotations

import asyncio
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


def apply_rpm_limit(llm: Any, key: str, rpm_limit: int) -> Any:
    """Wrap invoke/ainvoke to enforce per-profile RPM throttling."""
    orig_invoke = llm.invoke
    orig_ainvoke = llm.ainvoke

    def invoke(input: Any, config: Any = None, **kwargs: Any) -> Any:
        rate_limit_sync(key, rpm_limit)
        return orig_invoke(input, config=config, **kwargs)

    async def ainvoke(input: Any, config: Any = None, **kwargs: Any) -> Any:
        await asyncio.wait_for(rate_limit_async(key, rpm_limit), timeout=61.0)
        return await orig_ainvoke(input, config=config, **kwargs)

    return LLMWrapper(llm, invoke, ainvoke)
