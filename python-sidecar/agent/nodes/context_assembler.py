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
import re
from typing import Any

from agent.state import AgentState, WorldContext, NarrativeContext

logger = logging.getLogger(__name__)


def _truncate_text(text: str, max_chars: int) -> str:
    text = " ".join(str(text).split())
    if max_chars <= 0:
        return ""
    if len(text) <= max_chars:
        return text
    return text[: max_chars - 1].rstrip() + "…"


def _extract_focus_terms(*texts: str) -> list[str]:
    raw_terms = re.findall(r"[\u4e00-\u9fffA-Za-z0-9]{2,12}", " ".join(texts))
    stop_terms = {
        "本章", "章节", "继续", "下一章", "当前", "要求", "描写", "情节", "角色", "写作",
        "请继续写下一章", "继续写下一章",
    }
    terms: list[str] = []
    for term in raw_terms:
        if term in stop_terms or term in terms:
            continue
        terms.append(term)
        if len(terms) >= 12:
            break
    return terms


def _focus_score(text: str, focus_terms: list[str]) -> int:
    lower = text.lower()
    return sum(lower.count(term.lower()) for term in focus_terms if term)


def _compress_ranked_items(
    items: list[str],
    *,
    limit: int,
    per_item_chars: int,
    focus_terms: list[str],
) -> list[str]:
    ranked: list[tuple[int, int, str]] = []
    seen: set[str] = set()
    for idx, item in enumerate(items):
        clean = _truncate_text(str(item).strip(), per_item_chars)
        if not clean or clean in seen:
            continue
        seen.add(clean)
        ranked.append((_focus_score(clean, focus_terms), idx, clean))
    ranked.sort(key=lambda it: (-it[0], it[1]))
    return [item for _, _, item in ranked[:limit]]


def _take_with_budget(parts: list[str], max_chars: int) -> str:
    if max_chars <= 0:
        return ""
    used = 0
    selected: list[str] = []
    for part in parts:
        if not part:
            continue
        remaining = max_chars - used
        if remaining <= 0:
            break
        clipped = part if len(part) <= remaining else _truncate_text(part, remaining)
        if not clipped:
            continue
        selected.append(clipped)
        used += len(clipped) + 2
    return "\n\n".join(selected)


def _prioritize_characters(chars: list[dict], focus_terms: list[str], limit: int) -> list[dict]:
    ranked: list[tuple[int, int, dict]] = []
    for idx, char in enumerate(chars):
        rel_names = " ".join(str(rel.get("target_name", "")) for rel in char.get("relations", []))
        text = f"{char.get('name', '')} {char.get('role', '')} {char.get('traits', '')} {rel_names}"
        role_boost = 3 if char.get("role") in {"protagonist", "mentor", "antagonist"} else 0
        relation_boost = min(len(char.get("relations", [])), 3)
        ranked.append((_focus_score(text, focus_terms) + role_boost + relation_boost, idx, char))
    ranked.sort(key=lambda it: (-it[0], it[1]))
    return [char for _, _, char in ranked[:limit]]


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
            f"• {c['name']}（{c.get('role', '')}）：{_truncate_text(c.get('traits', ''), 80)}。"
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
    user_prompt = state.get("user_prompt", "")
    style_profile = state.get("style_profile") or {}
    long_term = state.get("long_term_facts", [])
    llm_cfg = state.get("llm_config") or {}
    focus_terms = _extract_focus_terms(outline_hint or "", user_prompt or "")

    total_budget = int(llm_cfg.get("context_char_budget", 14000) or 14000)
    top_budget = max(2500, int(total_budget * 0.30))
    middle_budget = max(3500, int(total_budget * 0.38))
    bottom_budget = max(2800, total_budget - top_budget - middle_budget)

    # ── TRACK-1: World knowledge (anchor top) ────────────────────────────────
    top_parts: list[str] = []

    rules = _compress_ranked_items(
        world.get("constitution_rules", []),
        limit=8,
        per_item_chars=90,
        focus_terms=[],
    )
    if rules:
        top_parts.append("【世界宪法·不变规则】\n" + _fmt_list(rules))

    char_cores = _prioritize_characters(world.get("character_cores", []), focus_terms, limit=6)
    if char_cores:
        top_parts.append("【核心角色设定】\n" + _fmt_character_cores(char_cores))

    foresh = _compress_ranked_items(
        world.get("foreshadowing_active", []),
        limit=5,
        per_item_chars=70,
        focus_terms=focus_terms,
    )
    if foresh:
        top_parts.append("【待回收伏笔】\n" + _fmt_list(foresh))

    # Genre constraint (if available)
    genre = style_profile.get("genre", "")
    if genre:
        top_parts.append(f"【题材约束】本作品为{genre}题材，严禁出现不属于该题材的元素。")

    anchor_top = _take_with_budget(top_parts, top_budget)

    # ── TRACK-2: Narrative continuity (context middle) ────────────────────────
    mid_parts: list[str] = []

    # Volume arc summary (long-term coherence, compressed)
    volume_arc = _truncate_text(narrative.get("current_arc_summary", ""), 600)
    if volume_arc:
        mid_parts.append("【本卷剧情脉络（压缩摘要）】\n" + volume_arc)

    summaries = _compress_ranked_items(
        narrative.get("recent_chapter_summaries", []),
        limit=4,
        per_item_chars=420,
        focus_terms=focus_terms,
    )
    if summaries:
        mid_parts.append(
            "【近期章节摘要（最新优先）】\n"
            + "\n---\n".join(summaries)
        )

    # Long-term facts from graphiti
    if long_term:
        fact_lines = [
            f"• {e.get('name', '')}：{_truncate_text(e.get('properties', {}).get('fact', ''), 140)}"
            for e in long_term[:6]
            if e.get("properties", {}).get("fact")
        ]
        if fact_lines:
            mid_parts.append("【图记忆·相关事实】\n" + "\n".join(fact_lines))

    style_samples = _compress_ranked_items(
        narrative.get("style_samples", []),
        limit=2,
        per_item_chars=320,
        focus_terms=focus_terms,
    )
    if style_samples:
        mid_parts.append("【参考风格样本】\n" + "\n\n".join(style_samples))

    # Plot momentum for continuity (prevents drift)
    plot_momentum = _truncate_text(narrative.get("plot_momentum", ""), 220)
    if plot_momentum:
        mid_parts.append("【当前剧情动量】\n" + plot_momentum)

    context_middle = _take_with_budget(mid_parts, middle_budget)

    # ── ANCHOR BOTTOM: writing instruction + current task ────────────────────
    bottom_parts: list[str] = []

    if outline_hint:
        bottom_parts.append(f"【本章大纲】\n{_truncate_text(outline_hint, 1400)}")

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
        "- 场景描写必须承担功能：要么推进情节，要么制造压力，要么映射人物情绪，禁止空镜头式堆砌\n"
        "- 心理描写必须绑定触发事件，并落实为判断、对话或动作，禁止连续自怜、自省、抒情空转\n"
        "- 单段连续纯景物/心理描写不超过120字；超过时必须插入新的信息增量、关系变化或风险升级\n"
        "- 每个场景都必须产生有效推进：信息揭示、目标受阻、关系变化、冲突升级、代价落地至少其一\n"
        "- 章节必须在悬念/动作/对话高点处断章，禁止写总结段或展望段\n"
        "- 用中文写作，风格流畅，情节张力充足"
    )

    anchor_bottom = _take_with_budget(bottom_parts, bottom_budget)

    logger.debug(
        "Context assembled: top=%d chars, mid=%d chars, bottom=%d chars",
        len(anchor_top), len(context_middle), len(anchor_bottom),
    )

    return {
        "anchor_top": anchor_top,
        "context_middle": context_middle,
        "anchor_bottom": anchor_bottom,
    }
