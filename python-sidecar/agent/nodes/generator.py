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
    "\n5. 场景描写必须服务于剧情推进、冲突施压或情绪映射，禁止为了文采而堆砌空镜和辞藻"
    "\n6. 心理描写必须先有触发源，再有情绪波动，最后落到判断、对话或动作，禁止空转式内心独白"
    "\n7. 每一个场景至少产生一种推进：信息揭示、风险升级、关系变化、目标受阻、代价落地"
    "\n8. 如需写环境、氛围、感官细节，必须让读者看见它如何改变人物的决定或下一步行动"
    "\n\n【强制断章规范】"
    "\n章节必须在动作、对话或悬念的高点处戛然而止。"
    "\n严禁：总结段、展望段、升华段、情绪收束段、预告性句式（如'他知道...未来...'）。"
    "\n最后一段不超过2句话，必须是未完成的动作/对话/悬念。"
    "\n\n【角色与道具出场约束】"
    "\n- 正文中出现的有名角色必须来自【核心角色设定】或本章大纲"
    "\n- 新角色闪现必须在近期后续交代其背景来源"
    "\n- 武器/法宝/道具首次出场必须有来源说明"
    "\n- 角色只能使用设定中记载的能力，禁止凭空出现新能力"
    "\n- 一章最多1次实力提升，获得过程不少于200字"
    "\n\n【描写失控熔断规则】"
    "\n- 连续两段以上纯景物描写时，第三段必须转入人物目标、障碍、发现或互动"
    "\n- 连续两段以上纯心理描写时，必须插入外部刺激或人物行动，禁止假大空抒情"
    "\n- 任何漂亮句子若不推动情节、情绪或关系，宁可删除，不要保留"
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

    # On retry, inject quality feedback from the previous attempt
    quality_issues = state.get("quality_issues", [])
    if quality_issues and state.get("retry_count", 0) > 0:
        feedback = "\n".join(f"- {str(issue)[:160]}" for issue in quality_issues[:6])
        parts.append(f"\n【上次质量检查反馈（请在本次创作中修正）】\n{feedback}")

    return "\n\n" + ("\n\n" + "─" * 40 + "\n\n").join(parts)


async def generator_node(state: AgentState) -> dict[str, Any]:
    """Call LLM and generate the chapter draft."""
    llm_cfg = state.get("llm_config", {})
    llm = build_llm(llm_cfg, default_temperature=0.72, default_max_tokens=4096)

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
            {"role": "user", "content": text[:2400]},
        ])
        return resp.content.strip()
    except Exception as exc:
        logger.warning("Summary generation failed: %s", repr(exc), exc_info=True)
        # Fallback: first 200 chars
        return text[:200] + "…"
