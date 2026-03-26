"""
Generator Node — calls the LLM with Lost-in-Middle arranged context and
produces a chapter draft.  Also generates a chapter summary for memory update.
"""
from __future__ import annotations

import logging
import os
from typing import Any

from agent.state import AgentState
from llm_utils import build_llm

logger = logging.getLogger(__name__)

_SYSTEM = (
    "你是一位专业的中文网络小说作家，善于构建情节张力、人物弧线和沉浸式场景描写。"
    "请严格按照给定的上下文信息进行创作，特别是要严格遵循【本章大纲】中的情节安排和顺序，不得违背世界规则和角色设定。"
    "\n\n【重要写作规范】"
    "\n1. 禁止使用分点作答格式（如：首先、其次、然后、最后、第一、第二等）"
    "\n2. 禁止使用学术化表述（如：一方面...另一方面、综上所述、总而言之、值得注意的是、"
    "不得不说、毋庸置疑、显而易见、不可否认、从某种程度上说等）"
    "\n3. 用自然流畅的叙事推进情节，避免说教式或总结式语气"
    "\n4. 直接展现场景和对话，而非分析和归纳"
)

_SUMMARY_SYSTEM = (
    "请用 3-5 句话概括以下章节内容，包括：主要事件、人物动态、情节推进、伏笔变化。"
    "直接输出摘要，不要任何前缀或格式标记。"
)


def _build_prompt(state: AgentState) -> str:
    """Assemble the full prompt using Lost-in-Middle layout."""
    anchor_top = state.get("anchor_top", "")
    context_middle = state.get("context_middle", "")
    anchor_bottom = state.get("anchor_bottom", "")
    user_prompt = state.get("user_prompt", "请继续写下一章。")
    chapter_num = state.get("chapter_num")

    parts: list[str] = []
    if anchor_top:
        parts.append(anchor_top)
    if context_middle:
        parts.append(context_middle)
    if anchor_bottom:
        parts.append(anchor_bottom)

    chapter_label = f"第 {chapter_num} 章" if chapter_num else "本章"
    parts.append(f"\n【写作任务】\n请创作{chapter_label}正文。{user_prompt}")

    return "\n\n" + ("\n\n" + "─" * 40 + "\n\n").join(parts)


async def generator_node(state: AgentState) -> dict[str, Any]:
    """Call LLM and generate the chapter draft."""
    llm_cfg = state.get("llm_config", {})
    llm = build_llm(llm_cfg, default_temperature=0.85, default_max_tokens=4096)

    prompt = _build_prompt(state)

    try:
        resp = await llm.ainvoke([
            {"role": "system", "content": _SYSTEM},
            {"role": "user", "content": prompt},
        ])
        draft = resp.content.strip()
    except Exception as exc:
        logger.error("Generator LLM call failed: %s", repr(exc), exc_info=True)
        return {"error": f"LLM generation failed: {exc}", "draft": ""}

    # Generate chapter summary for memory
    summary = await _generate_summary(draft, llm_cfg)

    logger.info("Draft generated: %d chars, summary: %d chars",
                len(draft), len(summary))
    return {
        "draft": draft,
        "chapter_summary": summary,
    }


async def _generate_summary(text: str, cfg: dict) -> str:
    if not text:
        return ""
    # Use a cheaper/faster model for summaries
    summary_cfg = {**cfg, "model": cfg.get("summary_model", cfg.get("model", "gpt-4o-mini")),
                   "max_tokens": 256, "temperature": 0.3}
    llm = build_llm(summary_cfg, default_temperature=0.3, default_max_tokens=256)
    try:
        resp = await llm.ainvoke([
            {"role": "system", "content": _SUMMARY_SYSTEM},
            {"role": "user", "content": text[:3000]},
        ])
        return resp.content.strip()
    except Exception as exc:
        logger.warning("Summary generation failed: %s", repr(exc), exc_info=True)
        # Fallback: first 200 chars
        return text[:200] + "…"
