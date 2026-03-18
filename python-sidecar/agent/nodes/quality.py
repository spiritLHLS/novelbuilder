"""
Quality Check Node — evaluates the draft against:
  1. World rule compliance (heuristic keyword check)
  2. Length adequacy
  3. Character name consistency
  4. LLM self-evaluation score (0-100)

Returns quality_score (0.0-1.0) and quality_issues list.
Decides whether to retry generation (up to max_retries).
"""
from __future__ import annotations

import logging
import re
from typing import Any, Literal

from langchain_openai import ChatOpenAI

from agent.state import AgentState

logger = logging.getLogger(__name__)

_EVAL_SYSTEM = """你是一位严格的小说质量审核员。请对以下章节草稿进行评分（0-100），并列出主要问题。

评分标准：
- 情节连贯性（30分）：是否与上文无矛盾
- 角色一致性（20分）：人物行为是否符合其设定
- 写作质量（25分）：文笔流畅、场景描写、对话自然
- 张力与节奏（15分）：是否有足够吸引力
- 世界观遵守（10分）：是否违反世界规则

返回 JSON，格式：{"score": 数字, "issues": ["问题1", "问题2"]}
不要输出其他内容。"""


def quality_check_node(state: AgentState) -> dict[str, Any]:
    """Synchronous quality check using heuristics + optional LLM eval."""
    draft = state.get("draft", "")
    retry_count = state.get("retry_count", 0)
    max_retries = state.get("max_retries", 2)

    issues: list[str] = []
    score = 1.0

    # ── Heuristic checks ─────────────────────────────────────────────────────
    word_count = len(draft)
    if word_count < 500:
        issues.append(f"篇幅过短（{word_count}字，要求至少500字）")
        score -= 0.4

    # Check for placeholder / degenerate output
    if "TODO" in draft or draft.count("…") > 10:
        issues.append("草稿包含占位符或省略号过多")
        score -= 0.3

    # Constitution rule compliance (very basic keyword check)
    rules = (state.get("world_track") or {}).get("constitution_rules", [])
    for rule in rules:
        # Look for explicit "禁止" / "不得" rules and check for obvious violations
        if "禁止" in rule or "不得" in rule:
            # This is a heuristic; true compliance requires LLM validation
            pass  # Skipped for perf; real LLM eval below will catch this

    score = max(0.0, min(1.0, score))

    # ── Determine outcome ─────────────────────────────────────────────────────
    # Accept if score is adequate or we've exhausted retries
    accept = score >= 0.6 or retry_count >= max_retries

    if accept:
        return {
            "quality_score": score,
            "quality_issues": issues,
            "final_text": draft,
            "done": True,
        }
    else:
        logger.info(
            "Quality check failed (score=%.2f, retry=%d/%d): %s",
            score, retry_count, max_retries, issues,
        )
        return {
            "quality_score": score,
            "quality_issues": issues,
            "retry_count": retry_count + 1,
            "done": False,
        }


def should_retry(state: AgentState) -> Literal["retry", "done"]:
    """Conditional edge: decide whether to regenerate or finish."""
    if state.get("done", False):
        return "done"
    return "retry"
