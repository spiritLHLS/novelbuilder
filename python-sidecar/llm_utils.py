"""Shared LLM builder: dispatches to the correct LangChain model class based on api_style."""
from __future__ import annotations

import os

# Map legacy api_style strings to the canonical path-based values.
_LEGACY: dict[str, str] = {
    "chat_completions": "/chat/completions",
    "claude":           "/messages",
    "responses":        "/responses",
}


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
        return ChatAnthropic(**kwargs)

    # ── Google Gemini ─────────────────────────────────────────────────────────
    if api_style == "gemini":
        from langchain_google_genai import ChatGoogleGenerativeAI

        kwargs = {"model": model, "google_api_key": api_key}
        if not omit_temperature:
            kwargs["temperature"] = float(cfg.get("temperature", default_temperature))
        if not omit_max_tokens:
            kwargs["max_output_tokens"] = int(cfg.get("max_tokens", default_max_tokens))
        return ChatGoogleGenerativeAI(**kwargs)

    # ── OpenAI / OpenAI-compatible (default) ──────────────────────────────────
    from langchain_openai import ChatOpenAI

    # For path-based api_style (e.g. "/chat/completions", "/v1/chat/completions"),
    # compute the SDK-compatible base URL by stripping the /chat/completions suffix.
    if base_url and api_style:
        full = base_url + api_style
        if full.endswith("/chat/completions"):
            base_url = full.removesuffix("/chat/completions")

    kwargs = {"api_key": api_key, "model": model, "streaming": streaming}
    if base_url:
        kwargs["base_url"] = base_url
    if not omit_temperature:
        kwargs["temperature"] = float(cfg.get("temperature", default_temperature))
    if not omit_max_tokens:
        kwargs["max_tokens"] = int(cfg.get("max_tokens", default_max_tokens))
    return ChatOpenAI(**kwargs)
