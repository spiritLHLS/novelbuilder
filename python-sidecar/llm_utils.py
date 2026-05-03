"""Shared LLM builder: dispatches to the correct LangChain model class based on api_style."""
from __future__ import annotations

import asyncio
import json
import logging
import os
import re
import threading
import time
from typing import Any, Callable

import httpx

try:
    import redis
except Exception:  # pragma: no cover - optional dependency during local analysis
    redis = None

logger = logging.getLogger("python-agent")

# Map legacy api_style strings to the canonical path-based values.
_LEGACY: dict[str, str] = {
    "chat_completions": "/chat/completions",
    "claude":           "/messages",
    "responses":        "/responses",
}

# Models that require max_completion_tokens instead of max_tokens (e.g. o1, o3, o4-mini).
_O_SERIES_RE = re.compile(r"\bo[1-9](-mini|-preview|-pro)?\b", re.IGNORECASE)

_CHAT_SESSION_TTL_SECONDS = 24 * 60 * 60
_CHAT_SESSION_MAX_MESSAGES = 8
_CHAT_SESSION_KEEP_RECENT = 4
_CHAT_SESSION_MAX_RUNES = 12000
_CHAT_SESSION_SUMMARY_RUNES = 4000
_CHAT_SESSION_EXCERPT_RUNES = 240
_CHAT_SESSION_KEY_PREFIX = "ai_gateway_session:"

_session_store_lock = threading.RLock()
_session_memory: dict[str, dict[str, Any]] = {}
_session_redis_client = None


def _needs_completion_tokens(model: str, cfg: dict) -> bool:
    """Return True when max_completion_tokens should be used instead of max_tokens."""
    return cfg.get("use_max_completion_tokens", False) or bool(_O_SERIES_RE.search(model))


def _is_max_tokens_unsupported(exc: Exception) -> bool:
    """Detect the specific 400 error: 'max_tokens is not supported, use max_completion_tokens'."""
    msg = str(exc)
    return "max_tokens" in msg and "max_completion_tokens" in msg


class _LLMWrapper:
    """Thin proxy around a LangChain LLM that overrides invoke/ainvoke without
    mutating the underlying Pydantic model (Pydantic v2 rejects arbitrary field
    assignment, so we never do ``llm.invoke = fn`` directly)."""

    def __init__(self, delegate, invoke_fn, ainvoke_fn):
        self._delegate = delegate
        self._invoke_fn = invoke_fn
        self._ainvoke_fn = ainvoke_fn

    def invoke(self, input, config=None, **kwargs):
        return self._invoke_fn(input, config=config, **kwargs)

    async def ainvoke(self, input, config=None, **kwargs):
        return await self._ainvoke_fn(input, config=config, **kwargs)

    def __getattr__(self, name):
        return getattr(self._delegate, name)


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

    return _LLMWrapper(primary, _invoke, _ainvoke)


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
        time.sleep(max(wait_secs, 0.05))


def _apply_rpm_limit(llm, key: str, rpm_limit: int):
    """Wrap a LangChain model's invoke/ainvoke to enforce RPM rate limiting."""
    _orig_invoke = llm.invoke
    _orig_ainvoke = llm.ainvoke

    def _invoke(input, config=None, **kwargs):
        _rate_limit_sync(key, rpm_limit)
        return _orig_invoke(input, config=config, **kwargs)

    async def _ainvoke(input, config=None, **kwargs):
        await asyncio.wait_for(_rate_limit_async(key, rpm_limit), timeout=61.0)
        return await _orig_ainvoke(input, config=config, **kwargs)

    return _LLMWrapper(llm, _invoke, _ainvoke)


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
    timeout = cfg.get("timeout")
    model_kwargs = dict(cfg.get("model_kwargs") or {})
    session_id = str(cfg.get("session_id") or "").strip()

    if cfg.get("json_mode"):
        model_kwargs.setdefault("response_format", {"type": "json_object"})
    if session_id:
        model_kwargs.setdefault("user", session_id[:64])

    kwargs = {"api_key": api_key, "model": model, "streaming": streaming}
    if base_url:
        kwargs["base_url"] = base_url
    if timeout:
        kwargs["timeout"] = float(timeout)
    if model_kwargs:
        kwargs["model_kwargs"] = model_kwargs
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


def build_invoke_config(
    cfg: dict | None = None,
    *,
    session_id: str | None = None,
    task_name: str | None = None,
    extra_metadata: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Build a consistent LangChain invoke config with task/session metadata."""
    cfg = cfg or {}
    resolved_session = str(session_id or cfg.get("session_id") or "").strip()

    metadata: dict[str, Any] = {}
    if extra_metadata:
        for key, value in extra_metadata.items():
            if value is not None and value != "":
                metadata[key] = value
    if resolved_session:
        metadata.setdefault("session_id", resolved_session)
    if task_name:
        metadata.setdefault("task_name", task_name)

    config: dict[str, Any] = {}
    tags: list[str] = []
    if task_name:
        config["run_name"] = task_name
        tags.append(task_name)
    if resolved_session:
        config["configurable"] = {"session_id": resolved_session}
        tags.append(f"session:{resolved_session[:24]}")
    if metadata:
        config["metadata"] = metadata
    if tags:
        config["tags"] = tags
    return config


def _extract_text_content(content: Any) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts: list[str] = []
        for item in content:
            text = _extract_text_content(item)
            if text:
                parts.append(text)
        return "\n".join(parts).strip()
    if isinstance(content, dict):
        for key in ("text", "content", "value", "output_text"):
            if key in content:
                return _extract_text_content(content[key])
    return str(content or "").strip()


def _normalize_session_id(value: Any) -> str:
    resolved = str(value or "").strip()
    if not resolved or resolved == "<nil>":
        return ""
    return resolved


def _session_key(session_id: str) -> str:
    return f"{_CHAT_SESSION_KEY_PREFIX}{session_id}"


def _default_session_state() -> dict[str, Any]:
    return {"summary": "", "messages": []}


def _get_session_redis_client():
    global _session_redis_client

    if redis is None:
        return None
    if _session_redis_client is not None:
        return _session_redis_client

    redis_url = os.getenv("REDIS_URL", "redis://127.0.0.1:6379")
    try:
        _session_redis_client = redis.from_url(redis_url, decode_responses=True)
    except Exception as exc:
        logger.warning("LLM session redis init failed: %s", repr(exc), exc_info=True)
        _session_redis_client = None
    return _session_redis_client


def _clone_session_state(state: dict[str, Any]) -> dict[str, Any]:
    return {
        "summary": str(state.get("summary") or ""),
        "messages": [
            {
                "role": str(msg.get("role") or "").strip(),
                "content": str(msg.get("content") or ""),
            }
            for msg in (state.get("messages") or [])
            if isinstance(msg, dict)
        ],
    }


def _prune_memory_sessions(now: float | None = None) -> None:
    cutoff = now or time.time()
    expired = [sid for sid, item in _session_memory.items() if float(item.get("expires_at", 0)) <= cutoff]
    for sid in expired:
        _session_memory.pop(sid, None)


def _load_session_state(session_id: str) -> dict[str, Any] | None:
    if not session_id:
        return None

    client = _get_session_redis_client()
    if client is not None:
        try:
            raw = client.get(_session_key(session_id))
            if raw:
                state = json.loads(raw)
                if isinstance(state, dict):
                    return _clone_session_state(state)
        except Exception as exc:
            logger.warning("LLM session read failed: %s", repr(exc), exc_info=True)

    with _session_store_lock:
        _prune_memory_sessions()
        item = _session_memory.get(session_id)
        if not item:
            return None
        return _clone_session_state(item.get("state") or _default_session_state())


def _update_session_state(
    session_id: str,
    update_fn: Callable[[dict[str, Any]], dict[str, Any] | None],
) -> None:
    if not session_id:
        return

    client = _get_session_redis_client()
    watch_error = getattr(redis, "WatchError", None) if redis is not None else None
    if client is not None:
        for _ in range(5):
            try:
                with client.pipeline() as pipe:
                    pipe.watch(_session_key(session_id))
                    raw = pipe.get(_session_key(session_id))
                    current = _default_session_state()
                    if raw:
                        decoded = json.loads(raw)
                        if isinstance(decoded, dict):
                            current = _clone_session_state(decoded)
                    updated = update_fn(_clone_session_state(current)) or current
                    payload = _clone_session_state(updated)
                    pipe.multi()
                    pipe.set(_session_key(session_id), json.dumps(payload, ensure_ascii=False), ex=_CHAT_SESSION_TTL_SECONDS)
                    pipe.execute()
                    return
            except Exception as exc:
                if watch_error is not None and isinstance(exc, watch_error):
                    continue
                logger.warning("LLM session atomic update failed: %s", repr(exc), exc_info=True)
                break

    with _session_store_lock:
        _prune_memory_sessions()
        item = _session_memory.get(session_id)
        current = _clone_session_state((item or {}).get("state") or _default_session_state())
        updated = update_fn(_clone_session_state(current)) or current
        _session_memory[session_id] = {
            "expires_at": time.time() + _CHAT_SESSION_TTL_SECONDS,
            "state": _clone_session_state(updated),
        }


def _save_session_state(session_id: str, state: dict[str, Any]) -> None:
    if not session_id:
        return

    payload = _clone_session_state(state)
    client = _get_session_redis_client()
    if client is not None:
        try:
            client.set(_session_key(session_id), json.dumps(payload, ensure_ascii=False), ex=_CHAT_SESSION_TTL_SECONDS)
            return
        except Exception as exc:
            logger.warning("LLM session write failed: %s", repr(exc), exc_info=True)

    with _session_store_lock:
        _prune_memory_sessions()
        _session_memory[session_id] = {
            "expires_at": time.time() + _CHAT_SESSION_TTL_SECONDS,
            "state": payload,
        }


def _compact_message_content(content: str) -> str:
    compact = " ".join(str(content or "").split())
    if len(compact) > _CHAT_SESSION_EXCERPT_RUNES:
        return compact[:_CHAT_SESSION_EXCERPT_RUNES] + "..."
    return compact


def _total_message_runes(messages: list[dict[str, str]]) -> int:
    return sum(len(msg.get("content", "")) for msg in messages)


def _extend_session_summary(existing: str, archived: list[dict[str, str]]) -> str:
    lines: list[str] = []
    if existing.strip():
        lines.append(existing.strip())
    for msg in archived:
        role = str(msg.get("role") or "user").strip() or "user"
        lines.append(f"{role}: {_compact_message_content(msg.get('content', ''))}")
    return "\n".join(lines).strip()


def _compact_session_state(state: dict[str, Any]) -> dict[str, Any]:
    summary = str(state.get("summary") or "")
    messages = [
        {
            "role": str(msg.get("role") or "").strip(),
            "content": str(msg.get("content") or ""),
        }
        for msg in (state.get("messages") or [])
        if isinstance(msg, dict)
    ]

    while len(messages) > _CHAT_SESSION_MAX_MESSAGES or _total_message_runes(messages) + len(summary) > _CHAT_SESSION_MAX_RUNES:
        if len(messages) <= _CHAT_SESSION_KEEP_RECENT:
            break
        archive_count = len(messages) - _CHAT_SESSION_KEEP_RECENT
        archived = messages[:archive_count]
        messages = messages[archive_count:]
        summary = _extend_session_summary(summary, archived)

    if len(summary) > _CHAT_SESSION_SUMMARY_RUNES:
        summary = summary[-_CHAT_SESSION_SUMMARY_RUNES:]

    state["summary"] = summary
    state["messages"] = messages
    return state


def _message_role_and_text(message: Any) -> tuple[str, str]:
    if isinstance(message, dict):
        role = str(message.get("role") or "").strip()
        content = _extract_text_content(message.get("content"))
    else:
        role = str(getattr(message, "type", "") or getattr(message, "role", "")).strip()
        content = _extract_text_content(getattr(message, "content", ""))

    role = {
        "human": "user",
        "ai": "assistant",
        "system": "system",
    }.get(role, role or "user")
    return role, content


def _message_style(messages: list[Any]) -> str:
    if not messages:
        return "dict"
    first = messages[0]
    if isinstance(first, dict):
        return "dict"
    return "langchain"


def _make_message(role: str, content: str, style: str) -> Any:
    if style == "dict":
        return {"role": role, "content": content}

    try:
        from langchain_core.messages import AIMessage, HumanMessage, SystemMessage

        if role == "system":
            return SystemMessage(content=content)
        if role == "assistant":
            return AIMessage(content=content)
        return HumanMessage(content=content)
    except Exception:
        return {"role": role, "content": content}


def _session_context_text(state: dict[str, Any]) -> str:
    parts: list[str] = []
    summary = str(state.get("summary") or "").strip()
    if summary:
        parts.append("以下是当前同一任务 session 的压缩历史摘要，请保持连续性并避免重复展开已完成内容：\n" + summary)

    recent = []
    for msg in (state.get("messages") or []):
        if not isinstance(msg, dict):
            continue
        role = str(msg.get("role") or "user").strip() or "user"
        content = str(msg.get("content") or "").strip()
        if content:
            recent.append(f"{role}: {content}")
    if recent:
        parts.append("以下是当前同一任务 session 最近保留的对话，请在此基础上继续：\n" + "\n".join(recent))

    return "\n\n".join(parts).strip()


def _prepare_session_messages(messages: list[Any], session_id: str, *, use_session_history: bool) -> list[Any]:
    if not use_session_history or not session_id or not isinstance(messages, list):
        return messages

    state = _load_session_state(session_id)
    if not state:
        return messages

    context_text = _session_context_text(_compact_session_state(state))
    if not context_text:
        return messages

    style = _message_style(messages)
    system_messages: list[Any] = []
    other_messages: list[Any] = []
    for msg in messages:
        role, _ = _message_role_and_text(msg)
        if role == "system":
            system_messages.append(msg)
        else:
            other_messages.append(msg)

    prepared = list(system_messages)
    prepared.append(_make_message("system", context_text, style))
    prepared.extend(other_messages)
    return prepared


def _persist_session_messages(session_id: str, messages: list[Any], assistant_reply: str, *, use_session_history: bool) -> None:
    if not use_session_history or not session_id or not isinstance(messages, list):
        return

    def _append_messages(current: dict[str, Any]) -> dict[str, Any]:
        state = _clone_session_state(current or _default_session_state())
        for msg in messages:
            role, content = _message_role_and_text(msg)
            if role == "system" or not content:
                continue
            state.setdefault("messages", []).append({"role": role, "content": content})
        reply = str(assistant_reply or "").strip()
        if reply:
            state.setdefault("messages", []).append({"role": "assistant", "content": reply})
        return _compact_session_state(state)

    _update_session_state(session_id, _append_messages)


def _resolve_session_id(cfg: dict | None, config: dict | None = None) -> str:
    if isinstance(config, dict):
        configurable = config.get("configurable") or {}
        if isinstance(configurable, dict):
            resolved = _normalize_session_id(configurable.get("session_id"))
            if resolved:
                return resolved
        metadata = config.get("metadata") or {}
        if isinstance(metadata, dict):
            resolved = _normalize_session_id(metadata.get("session_id"))
            if resolved:
                return resolved
    return _normalize_session_id((cfg or {}).get("session_id"))


def _response_messages_payload(messages: list[Any]) -> list[dict[str, Any]]:
    payload: list[dict[str, Any]] = []
    for msg in messages:
        role, content = _message_role_and_text(msg)
        if not content:
            continue
        item_type = "input_text"
        if role == "assistant":
            item_type = "output_text"
        payload.append(
            {
                "role": role if role in {"system", "user", "assistant"} else "user",
                "content": [{"type": item_type, "text": content}],
            }
        )
    return payload


def invoke_text_sync(
    prompt: str,
    cfg: dict,
    *,
    system_prompt: str,
    session_id: str | None = None,
    task_name: str | None = None,
    extra_metadata: dict[str, Any] | None = None,
    use_session_history: bool = True,
) -> tuple[str, dict[str, Any]]:
    """Synchronous counterpart to ainvoke_text for planner-style call sites."""
    invoke_cfg = dict(cfg or {})
    if session_id and not invoke_cfg.get("session_id"):
        invoke_cfg["session_id"] = session_id

    config = build_invoke_config(
        invoke_cfg,
        session_id=session_id,
        task_name=task_name,
        extra_metadata=extra_metadata,
    ) or None
    resolved_session = _resolve_session_id(invoke_cfg, config)
    current_messages = [
        {"role": "system", "content": system_prompt},
        {"role": "user", "content": prompt},
    ]

    api_style = _LEGACY.get(str(invoke_cfg.get("api_style") or ""), str(invoke_cfg.get("api_style") or ""))
    if api_style.endswith("/responses"):
        api_key = invoke_cfg.get("api_key") or os.getenv("OPENAI_API_KEY", "")
        if not api_key:
            raise ValueError("no API key configured")

        base_url = (invoke_cfg.get("base_url") or "https://api.openai.com/v1").rstrip("/")
        base_url = re.sub(r"/chat/completions$", "", base_url, flags=re.IGNORECASE)
        base_url = re.sub(r"/responses$", "", base_url, flags=re.IGNORECASE)
        endpoint = f"{base_url}/responses"
        model = invoke_cfg.get("model", "gpt-4o")
        rpm_limit = int(invoke_cfg.get("rpm_limit", 0) or 0)

        if rpm_limit > 0:
            _rate_limit_sync(f"{base_url}|{model}", rpm_limit)

        headers = {
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        }
        if resolved_session:
            headers["X-NovelBuilder-Session"] = resolved_session[:128]

        prepared_messages = _prepare_session_messages(current_messages, resolved_session, use_session_history=use_session_history)
        payload: dict[str, Any] = {
            "model": model,
            "input": _response_messages_payload(prepared_messages),
            "store": False,
        }
        if not invoke_cfg.get("omit_max_tokens", False):
            payload["max_output_tokens"] = int(invoke_cfg.get("max_tokens", 4096))

        timeout_seconds = float(invoke_cfg.get("timeout", 600) or 600)
        http_timeout = httpx.Timeout(connect=30.0, read=timeout_seconds, write=30.0, pool=30.0)
        with httpx.Client(timeout=http_timeout) as client:
            resp = client.post(endpoint, headers=headers, json=payload)
            resp.raise_for_status()
            data = json.loads(resp.text)

        output = data.get("output") or []
        content = output[0].get("content") if output and isinstance(output[0], dict) else []
        text = _extract_text_content(content)
        _persist_session_messages(resolved_session, current_messages, text, use_session_history=use_session_history)
        return text, {
            "status": data.get("status", "unknown"),
            "model": data.get("model", model),
            "session_id": resolved_session,
        }

    llm = build_llm(
        invoke_cfg,
        default_temperature=float(invoke_cfg.get("temperature", 0.7) or 0.7),
        default_max_tokens=int(invoke_cfg.get("max_tokens", 4096) or 4096),
    )
    prepared_messages = _prepare_session_messages(current_messages, resolved_session, use_session_history=use_session_history)
    response = llm.invoke(prepared_messages, config=config)

    response_meta = dict(getattr(response, "response_metadata", {}) or {})
    additional = dict(getattr(response, "additional_kwargs", {}) or {})
    reasoning = additional.get("reasoning_content") or additional.get("thinking")
    if reasoning:
        response_meta["reasoning_content"] = reasoning
    text = _extract_text_content(getattr(response, "content", ""))
    _persist_session_messages(resolved_session, current_messages, text, use_session_history=use_session_history)
    response_meta.setdefault("session_id", resolved_session)
    return text, response_meta


async def ainvoke_text(
    prompt: str,
    cfg: dict,
    *,
    system_prompt: str,
    session_id: str | None = None,
    task_name: str | None = None,
    extra_metadata: dict[str, Any] | None = None,
    use_session_history: bool = True,
) -> tuple[str, dict[str, Any]]:
    """Invoke the configured model through the shared routing layer and return raw text plus metadata."""
    invoke_cfg = dict(cfg or {})
    if session_id and not invoke_cfg.get("session_id"):
        invoke_cfg["session_id"] = session_id

    config = build_invoke_config(
        invoke_cfg,
        session_id=session_id,
        task_name=task_name,
        extra_metadata=extra_metadata,
    ) or None
    resolved_session = _resolve_session_id(invoke_cfg, config)
    current_messages = [
        {"role": "system", "content": system_prompt},
        {"role": "user", "content": prompt},
    ]

    api_style = _LEGACY.get(str(invoke_cfg.get("api_style") or ""), str(invoke_cfg.get("api_style") or ""))

    if api_style.endswith("/responses"):
        api_key = invoke_cfg.get("api_key") or os.getenv("OPENAI_API_KEY", "")
        if not api_key:
            raise ValueError("no API key configured")

        base_url = (invoke_cfg.get("base_url") or "https://api.openai.com/v1").rstrip("/")
        base_url = re.sub(r"/chat/completions$", "", base_url, flags=re.IGNORECASE)
        base_url = re.sub(r"/responses$", "", base_url, flags=re.IGNORECASE)
        endpoint = f"{base_url}/responses"
        model = invoke_cfg.get("model", "gpt-4o")
        rpm_limit = int(invoke_cfg.get("rpm_limit", 0) or 0)

        if rpm_limit > 0:
            await _rate_limit_async(f"{base_url}|{model}", rpm_limit)

        headers = {
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        }
        if resolved_session:
            headers["X-NovelBuilder-Session"] = resolved_session[:128]

        prepared_messages = _prepare_session_messages(current_messages, resolved_session, use_session_history=use_session_history)
        payload: dict[str, Any] = {
            "model": model,
            "input": _response_messages_payload(prepared_messages),
            "store": False,
        }
        if not invoke_cfg.get("omit_max_tokens", False):
            payload["max_output_tokens"] = int(invoke_cfg.get("max_tokens", 4096))

        timeout_seconds = float(invoke_cfg.get("timeout", 600) or 600)
        http_timeout = httpx.Timeout(connect=30.0, read=timeout_seconds, write=30.0, pool=30.0)

        async with httpx.AsyncClient(timeout=http_timeout) as client:
            resp = await client.post(endpoint, headers=headers, json=payload)
            resp.raise_for_status()
            data = json.loads(resp.text)

        output = data.get("output") or []
        content = output[0].get("content") if output and isinstance(output[0], dict) else []
        text = _extract_text_content(content)
        _persist_session_messages(resolved_session, current_messages, text, use_session_history=use_session_history)
        return text, {
            "status": data.get("status", "unknown"),
            "model": data.get("model", model),
            "session_id": resolved_session,
        }

    from langchain_core.messages import HumanMessage, SystemMessage

    llm = build_llm(
        invoke_cfg,
        default_temperature=float(invoke_cfg.get("temperature", 0.7) or 0.7),
        default_max_tokens=int(invoke_cfg.get("max_tokens", 4096) or 4096),
    )
    prepared_messages = _prepare_session_messages(
        [
            SystemMessage(content=system_prompt),
            HumanMessage(content=prompt),
        ],
        resolved_session,
        use_session_history=use_session_history,
    )
    response = await llm.ainvoke(
        prepared_messages,
        config=config,
    )

    response_meta = dict(getattr(response, "response_metadata", {}) or {})
    additional = dict(getattr(response, "additional_kwargs", {}) or {})
    reasoning = additional.get("reasoning_content") or additional.get("thinking")
    if reasoning:
        response_meta["reasoning_content"] = reasoning
    text = _extract_text_content(getattr(response, "content", ""))
    _persist_session_messages(resolved_session, current_messages, text, use_session_history=use_session_history)
    response_meta.setdefault("session_id", resolved_session)
    return text, response_meta
