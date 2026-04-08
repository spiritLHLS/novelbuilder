"""
Planner Node — decomposes the user task into concrete, ordered plan_steps.
Uses the LLM to produce a JSON list of steps.
"""
from __future__ import annotations

import json
import logging
import os
from typing import Any

from json_repair import repair_json
from agent.state import AgentState, PlanStep
from llm_utils import build_llm

logger = logging.getLogger(__name__)

_PLAN_SYSTEM = """你是一位专业的 AI 小说生成规划师。
用户会给你一个写作任务，请将其分解为 3-6 个具体、可执行的步骤。

返回严格的 JSON 数组，格式为：
[
  {"step": 1, "description": "具体步骤描述", "status": "pending"},
  ...
]
不要输出其他内容。"""


def planner_node(state: AgentState) -> dict[str, Any]:
    """Decompose the writing task into plan_steps.
    
    For generate_chapter tasks, skip the LLM call entirely and use the
    deterministic default plan. This saves tokens and latency since the
    plan is always the same 6 steps for chapter generation.
    """
    task_type = state.get("task_type", "generate_chapter")

    # Chapter generation always follows the same pipeline — no LLM needed.
    if task_type == "generate_chapter":
        steps = _default_plan(task_type)
        logger.info("Plan created (deterministic): %d steps for task=%s", len(steps), task_type)
        return {
            "plan_steps": steps,
            "current_step": 0,
            "retry_count": 0,
            "max_retries": state.get("max_retries", 2),
            "done": False,
        }

    # For other task types, use LLM planning
    user_prompt = state.get("user_prompt", "")
    chapter_num = state.get("chapter_num")
    outline_hint = state.get("outline_hint", "")

    llm_cfg = state.get("llm_config", {})
    llm = build_llm(llm_cfg, default_temperature=0.3, default_max_tokens=512)

    task_desc = f"任务类型: {task_type}\n用户提示: {user_prompt}"
    if chapter_num is not None:
        task_desc += f"\n目标章节: 第 {chapter_num} 章"
    if outline_hint:
        task_desc += f"\n章节梗概: {outline_hint}"

    try:
        resp = llm.invoke([
            {"role": "system", "content": _PLAN_SYSTEM},
            {"role": "user", "content": task_desc},
        ])
        raw = resp.content.strip()
        # strip markdown code blocks if present
        if raw.startswith("```"):
            raw = raw.split("```")[1]
            if raw.startswith("json"):
                raw = raw[4:]
        parsed = repair_json(raw)
        if not isinstance(parsed, list):
            logger.warning(
                "Planner: LLM response is not a JSON array after repair, falling back "
                "| task=%s | raw snippet: %.300s", task_type, raw
            )
            steps = _default_plan(task_type)
        else:
            steps = parsed
    except Exception as exc:
        logger.warning("Planner LLM failed, using default plan: %s", repr(exc), exc_info=True)
        steps = _default_plan(task_type)

    logger.info("Plan created: %d steps for task=%s", len(steps), task_type)
    return {
        "plan_steps": steps,
        "current_step": 0,
        "retry_count": 0,
        "max_retries": state.get("max_retries", 2),
        "done": False,
    }


def _default_plan(task_type: str) -> list[PlanStep]:
    if task_type == "generate_chapter":
        return [
            PlanStep(step=1, description="召回长期记忆与相关实体", status="pending"),
            PlanStep(step=2, description="检索世界知识与叙事上下文", status="pending"),
            PlanStep(step=3, description="组装 Re³ 双轨上下文", status="pending"),
            PlanStep(step=4, description="生成章节草稿", status="pending"),
            PlanStep(step=5, description="更新图记忆与向量索引", status="pending"),
            PlanStep(step=6, description="质量评估", status="pending"),
        ]
    return [
        PlanStep(step=1, description="分析内容", status="pending"),
        PlanStep(step=2, description="生成输出", status="pending"),
        PlanStep(step=3, description="质量检查", status="pending"),
    ]

