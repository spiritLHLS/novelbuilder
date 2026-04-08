"""
Context Assembler Node — Re³ Dual-Track + Lost-in-Middle arrangement.

Lost-in-Middle strategy:
  ANCHOR_TOP    → World constitution rules + character cores  (most critical)
  CONTEXT_MID   → Retrieved summaries, style samples, graph entities (secondary)
  ANCHOR_BOTTOM → Current chapter outline + writing style instruction  (most critical)

Research: LLMs attend most strongly to the start and end of the context window;
content buried in the middle suffers from reduced attention ("lost in the middle").
By anchoring the must-follow rules at top and the immediate task at bottom,
we maximise compliance with both world rules and generation instructions.
"""
from __future__ import annotations

import json
import logging
from typing import Any

from agent.state import AgentState, WorldContext, NarrativeContext

logger = logging.getLogger(__name__)


def _fmt_character_cores(chars: list[dict]) -> str:
    if not chars:
        return ""
    lines = []
    for c in chars[:8]:  # cap to avoid token explosion
        rel_str = ""
        if c.get("relations"):
            visible = [r for r in c["relations"] if r.get("target_name")][:3]
            rel_str = "；".join(
                f"{r['rel_type']}→{r['target_name']}" for r in visible
            )
        lines.append(
            f"• {c['name']}（{c.get('role', '')}）：{c.get('traits', '')}。"
            + (f" 关系：{rel_str}" if rel_str else "")
        )
    return "\n".join(lines)


def _fmt_list(items: list[str], prefix: str = "• ") -> str:
    return "\n".join(f"{prefix}{item}" for item in items if item)


def assemble_context_node(state: AgentState) -> dict[str, Any]:
    """
    Assemble Re³ dual-track context using Lost-in-Middle ordering.
    Populates anchor_top, context_middle, anchor_bottom.
    """
    world: WorldContext = state.get("world_track", {})
    narrative: NarrativeContext = state.get("narrative_track", {})
    outline_hint = state.get("outline_hint", "")
    style_profile = state.get("style_profile") or {}
    long_term = state.get("long_term_facts", [])

    # ── TRACK-1: World knowledge (anchor top) ────────────────────────────────
    top_parts: list[str] = []

    rules = world.get("constitution_rules", [])
    if rules:
        top_parts.append("【世界宪法·不变规则】\n" + _fmt_list(rules))

    char_cores = world.get("character_cores", [])
    if char_cores:
        top_parts.append("【核心角色设定】\n" + _fmt_character_cores(char_cores))

    foresh = world.get("foreshadowing_active", [])
    if foresh:
        top_parts.append("【待回收伏笔】\n" + _fmt_list(foresh))

    # Genre constraint (if available)
    genre = style_profile.get("genre", "")
    if genre:
        top_parts.append(f"【题材约束】本作品为{genre}题材，严禁出现不属于该题材的元素。")

    anchor_top = "\n\n".join(top_parts)

    # ── TRACK-2: Narrative continuity (context middle) ────────────────────────
    mid_parts: list[str] = []

    # Volume arc summary (long-term coherence, compressed)
    volume_arc = narrative.get("current_arc_summary", "")
    if volume_arc:
        mid_parts.append("【本卷剧情脉络（压缩摘要）】\n" + volume_arc)

    summaries = narrative.get("recent_chapter_summaries", [])
    if summaries:
        mid_parts.append(
            "【近期章节摘要（最新优先）】\n"
            + "\n---\n".join(summaries[:5])
        )

    # Long-term facts from graphiti
    if long_term:
        fact_lines = [
            f"• {e.get('name', '')}：{e.get('properties', {}).get('fact', '')}"
            for e in long_term[:6]
            if e.get("properties", {}).get("fact")
        ]
        if fact_lines:
            mid_parts.append("【图记忆·相关事实】\n" + "\n".join(fact_lines))

    style_samples = narrative.get("style_samples", [])
    if style_samples:
        mid_parts.append("【参考风格样本】\n" + "\n\n".join(style_samples[:2]))

    # Plot momentum for continuity (prevents drift)
    plot_momentum = narrative.get("plot_momentum", "")
    if plot_momentum:
        mid_parts.append("【当前剧情动量】\n" + plot_momentum)

    context_middle = "\n\n".join(mid_parts)

    # ── ANCHOR BOTTOM: writing instruction + current task ────────────────────
    bottom_parts: list[str] = []

    if outline_hint:
        bottom_parts.append(f"【本章大纲】\n{outline_hint}")

    # Style instruction
    if style_profile:
        rhythm = style_profile.get("rhythm", "")
        pov = style_profile.get("point_of_view", "")
        if rhythm or pov:
            style_line = f"叙事节奏：{rhythm}　视角：{pov}"
            bottom_parts.append(f"【写作风格要求】\n{style_line}")

    bottom_parts.append(
        "【生成要求】\n"
        "- **严格按照【本章大纲】编排情节，不得偏离大纲内容和顺序**\n"
        "- 严格遵守世界宪法中的不变规则\n"
        "- 人物行为必须符合角色设定\n"
        "- 如有待回收伏笔，在合适处自然植入\n"
        "- 内容连贯，与近期章节摘要无矛盾\n"
        "- 新角色/道具/能力首次出场必须交代来源，禁止凭空出现\n"
        "- 角色只能使用角色设定中已记录的能力，禁止突然拥有新能力\n"
        "- 章节必须在悬念/动作/对话高点处断章，禁止写总结段或展望段\n"
        "- 用中文写作，风格流畅，情节张力充足"
    )

    anchor_bottom = "\n\n".join(bottom_parts)

    logger.debug(
        "Context assembled: top=%d chars, mid=%d chars, bottom=%d chars",
        len(anchor_top), len(context_middle), len(anchor_bottom),
    )

    return {
        "anchor_top": anchor_top,
        "context_middle": context_middle,
        "anchor_bottom": anchor_bottom,
    }
