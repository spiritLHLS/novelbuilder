from __future__ import annotations
import asyncio
import json
import logging
import os
import re

import httpx
import psycopg2
import psycopg2.extras
from fastapi import APIRouter, BackgroundTasks, HTTPException
from fastapi.responses import StreamingResponse
from pydantic import BaseModel
from typing import Optional

from analyzers.style_analyzer import StyleAnalyzer
from analyzers.narrative_analyzer import NarrativeAnalyzer
from analyzers.atmosphere_analyzer import AtmosphereAnalyzer
from analyzers.plot_extractor import PlotExtractor
from humanizer.pipeline import HumanizationPipeline
from humanizer.metrics import PerplexityBurstinessEstimator
from json_repair import repair_json
from llm_utils import build_llm

logger = logging.getLogger("python-agent")


def get_db():
    import psycopg2, os
    return psycopg2.connect(
        host=os.getenv("DB_HOST", "127.0.0.1"),
        port=int(os.getenv("DB_PORT", "5432")),
        dbname=os.getenv("DB_NAME", "novelbuilder"),
        user=os.getenv("DB_USER", "novelbuilder"),
        password=os.getenv("DB_PASSWORD", "novelbuilder"),
        options="-c client_encoding=UTF8",
    )


def get_qdrant():
    from vector_store.qdrant_store import QdrantStore
    return QdrantStore.get_instance()


style_analyzer = StyleAnalyzer()
narrative_analyzer = NarrativeAnalyzer()
atmosphere_analyzer = AtmosphereAnalyzer()
plot_extractor = PlotExtractor()
humanizer = HumanizationPipeline()
metrics_estimator = PerplexityBurstinessEstimator()

import math
import uuid


router = APIRouter()

# 33-DIMENSION CHAPTER AUDIT
# ═══════════════════════════════════════════════════════════════════════════════

_AUDIT_SYSTEM = """你是一位专业的中文网络小说审核员，请对下面的章节草稿进行严格的多维度审计。

你必须严格按照以下33个维度逐一评分，每个维度给出0.0-1.0的分数和具体问题列表。
当问题列表为空时，passed=true；否则passed=false。

返回严格的JSON格式（不要输出其他内容）：
{
  "dimensions": {
    "character_memory": {"score": 0.9, "passed": true, "issues": []},
    "resource_continuity": {"score": 0.8, "passed": true, "issues": []},
    "foreshadowing_recovery": {"score": 0.7, "passed": false, "issues": ["伏笔X尚未回收"]},
    "outline_deviation": {...},
    "narrative_pace": {...},
    "emotional_arc": {...},
    "world_rule_compliance": {...},
    "timeline_consistency": {...},
    "pov_consistency": {...},
    "dialogue_naturalness": {...},
    "scene_description_quality": {...},
    "conflict_escalation": {...},
    "character_motivation": {...},
    "tension_management": {...},
    "hook_strength": {...},
    "subplot_advancement": {...},
    "relationship_dynamics": {...},
    "power_system_consistency": {...},
    "geographic_consistency": {...},
    "prop_continuity": {...},
    "language_variety": {...},
    "sentence_rhythm": {...},
    "vocabulary_richness": {...},
    "cliche_density": {...},
    "ai_pattern_detection": {...},
    "repetitive_sentence_structure": {...},
    "excessive_summarization": {...},
    "high_freq_ai_words": {...},
    "show_vs_tell": {...},
    "sensory_detail": {...},
    "inner_monologue_quality": {...},
    "chapter_length_adequacy": {...},
    "ending_hook": {...}
  },
  "overall_score": 0.82,
  "passed": true,
  "ai_probability": 0.3,
  "top_issues": ["问题1", "问题2"]
}

禁止输出任何JSON以外的内容。"""

# High-frequency AI-flavor words to detect
_AI_FLAVOR_WORDS = [
    "首先","其次","然后","最后","总的来说","综上所述","不得不说","值得注意的是",
    "总而言之","不仅如此","与此同时","尽管如此","事实上","毋庸置疑","显而易见",
    "另一方面","从某种程度上说","在某种意义上","不可否认","可以肯定的是",
    "正如","诚然","固然","况且","何况","由此可见","据此","基于此",
]


def _repair_json(raw: str) -> dict:
    """Wrapper around centralized repair_json for backward compatibility.
    
    Handles truncated/malformed LLM JSON responses with intelligent repair.
    """
    return repair_json(raw)


def _heuristic_audit(text: str, context: dict) -> dict:
    """Fast heuristic pass — runs without LLM, fills in obvious issues."""
    dimensions: dict = {}
    
    # chapter_length_adequacy
    wc = len(text)
    dims_length = {"score": 1.0, "passed": True, "issues": []}
    if wc < 1000:
        dims_length = {"score": 0.3, "passed": False, "issues": [f"篇幅过短（{wc}字，建议至少1500字）"]}
    elif wc < 2000:
        dims_length = {"score": 0.7, "passed": True, "issues": [f"篇幅偏短（{wc}字）"]}
    dimensions["chapter_length_adequacy"] = dims_length

    # ai_pattern_detection — check AI-flavor words
    ai_hits = [w for w in _AI_FLAVOR_WORDS if w in text]
    ai_score = max(0.0, 1.0 - len(ai_hits) * 0.08)
    dimensions["ai_pattern_detection"] = {
        "score": ai_score,
        "passed": len(ai_hits) < 5,
        "issues": [f"检测到AI味高频词：{', '.join(ai_hits[:8])}"] if ai_hits else [],
    }

    # high_freq_ai_words
    dimensions["high_freq_ai_words"] = {
        "score": ai_score,
        "passed": len(ai_hits) < 3,
        "issues": [f"高频AI惯用词（{len(ai_hits)}处）"] if len(ai_hits) >= 3 else [],
    }

    # repetitive_sentence_structure — detect repetitive sentence starters
    sentences = [s.strip() for s in re.split(r'[。！？…]', text) if len(s.strip()) > 5]
    starters = [s[:3] for s in sentences if len(s) >= 3]
    from collections import Counter
    start_counts = Counter(starters)
    repeated_starters = [(k, v) for k, v in start_counts.items() if v >= 4]
    dimensions["repetitive_sentence_structure"] = {
        "score": max(0.0, 1.0 - len(repeated_starters) * 0.15),
        "passed": len(repeated_starters) == 0,
        "issues": [f'句式开头重复过多：\u201c{k}\u201d出现{v}次' for k, v in repeated_starters[:3]],
    }

    # excessive_summarization — detect summary-style sentences
    summary_cues = ["总的来说", "总而言之", "综上", "简单来说", "说到底"]
    summary_hits = [c for c in summary_cues if c in text]
    dimensions["excessive_summarization"] = {
        "score": 1.0 - len(summary_hits) * 0.2,
        "passed": len(summary_hits) == 0,
        "issues": [f"过度总结式表达：{', '.join(summary_hits)}"] if summary_hits else [],
    }

    # cliche_density
    cliches = ["泪如雨下", "心如刀割", "血脉喷张", "怒火中烧", "虎躯一震", "眼神一凛",
               "眼冒金星", "不禁", "不由得", "忍不住", "顿时", "瞬间", "猛然"]
    cliche_hits = [c for c in cliches if text.count(c) >= 2]
    dimensions["cliche_density"] = {
        "score": max(0.0, 1.0 - len(cliche_hits) * 0.1),
        "passed": len(cliche_hits) < 4,
        "issues": [f"陈词滥调过多：{', '.join(cliche_hits[:5])}"] if len(cliche_hits) >= 4 else [],
    }

    return dimensions


@router.post("/audit/chapter")
async def audit_chapter(req: AuditChapterRequest):
    """
    33-dimension chapter audit.
    Phase 1: fast heuristic (no LLM).
    Phase 2: LLM deep eval (if llm_config provided).
    """
    import json
    
    heuristic_dims = _heuristic_audit(req.chapter_text, req.context)

    llm_dims: dict = {}
    ai_probability = 0.0
    top_issues: list = []

    if req.llm_config.get("api_key"):
        try:
            from langchain.schema import SystemMessage, HumanMessage

            llm = build_llm(req.llm_config, default_temperature=0.2, default_max_tokens=3000)

            context_str = ""
            if req.context.get("outline_hint"):
                context_str += f"\n【本章大纲】{req.context['outline_hint']}"
            if req.context.get("book_rules"):
                context_str += f"\n【创作规则】{req.context['book_rules'][:500]}"
            if req.context.get("previous_summaries"):
                summaries = req.context["previous_summaries"][-3:]
                context_str += f"\n【前情摘要】{'；'.join(summaries)}"
            if req.context.get("characters"):
                context_str += f"\n【主要角色】{str(req.context['characters'])[:400]}"
            if req.context.get("foreshadowings"):
                context_str += f"\n【待回收伏笔】{str(req.context['foreshadowings'])[:300]}"

            human_content = (
                f"{context_str}\n\n"
                f"【章节正文（第{req.chapter_num}章）】\n{req.chapter_text[:6000]}"
            )

            response = await llm.ainvoke([
                SystemMessage(content=_AUDIT_SYSTEM),
                HumanMessage(content=human_content),
            ])

            raw = response.content.strip()
            # Strip markdown code fences if present
            if raw.startswith("```"):
                raw = re.sub(r"^```[a-z]*\n?", "", raw)
                raw = re.sub(r"\n?```$", "", raw)

            data = json.loads(raw)
            llm_dims = data.get("dimensions", {})
            ai_probability = data.get("ai_probability", 0.0)
            top_issues = data.get("top_issues", [])

        except Exception as exc:
            logger.warning("LLM audit failed, using heuristic only: %s", repr(exc), exc_info=True)

    # Merge: LLM dims override heuristic where available
    merged = {**heuristic_dims, **llm_dims}

    # Fill any missing dimensions with neutral scores
    all_dims = [
        "character_memory", "resource_continuity", "foreshadowing_recovery",
        "outline_deviation", "narrative_pace", "emotional_arc",
        "world_rule_compliance", "timeline_consistency", "pov_consistency",
        "dialogue_naturalness", "scene_description_quality", "conflict_escalation",
        "character_motivation", "tension_management", "hook_strength",
        "subplot_advancement", "relationship_dynamics", "power_system_consistency",
        "geographic_consistency", "prop_continuity", "language_variety",
        "sentence_rhythm", "vocabulary_richness", "cliche_density",
        "ai_pattern_detection", "repetitive_sentence_structure",
        "excessive_summarization", "high_freq_ai_words", "show_vs_tell",
        "sensory_detail", "inner_monologue_quality", "chapter_length_adequacy",
        "ending_hook",
    ]
    for d in all_dims:
        if d not in merged:
            merged[d] = {"score": 0.8, "passed": True, "issues": []}

    scores = [v["score"] for v in merged.values()]
    overall_score = round(sum(scores) / len(scores), 3) if scores else 0.0
    passed = overall_score >= 0.65

    all_issues = top_issues + [
        issue
        for dim in merged.values()
        for issue in dim.get("issues", [])
    ]

    return {
        "dimensions": merged,
        "overall_score": overall_score,
        "passed": passed,
        "ai_probability": ai_probability,
        "issues": list(dict.fromkeys(all_issues))[:20],  # deduplicate, cap at 20
    }


# ═══════════════════════════════════════════════════════════════════════════════
# ANTI-AI REWRITE (去AI味)
# ═══════════════════════════════════════════════════════════════════════════════

_ANTI_DETECT_SYSTEM = """你是一位专业的中文小说改写编辑，擅长将AI生成的文章改写成具有鲜明人类写作风格的作品。

改写原则：
1. 【词汇替换】替换AI高频词（首先/其次/总而言之/不得不说等），用更自然的表达
2. 【句式变化】打破重复句式结构，混用长短句，增加节奏感
3. 【减少总结】删除过度概括/总结性句子，改为具体的场景描写或细节
4. 【增加人味】加入口语化表达、主观感受、细节描写
5. 【情绪具体化】将抽象情感（"他感到悲伤"）改为具体行为/感官描写
6. 【禁用句式】避免：以"这"开头的总结句、大量"不仅...还"结构、过渡词堆叠
7. 【文风注入】{style_guide}

改写强度：{intensity}
- light: 仅替换最明显的AI痕迹词汇，保持原文结构
- medium: 句式重组 + 词汇替换 + 增加细节
- heavy: 全面重写，保留核心情节但大幅改变表达方式

严禁：
- 改变情节内容、角色名称、故事事实
- 添加原文没有的情节元素
- 删除关键情节

输出格式（严格JSON）：
{"rewritten_text": "...改写后全文...", "changes_made": ["改动说明1", "改动说明2"]}"""


@router.post("/anti-detect/rewrite")
async def anti_detect_rewrite(req: AntiDetectRequest):
    """Anti-AI rewrite: de-flavor AI-generated chapter text."""
    import json

    if not req.llm_config.get("api_key"):
        raise HTTPException(status_code=400, detail="llm_config.api_key is required for anti-detect rewrite")

    # Measure AI probability before
    metrics_before = metrics_estimator.estimate(req.text)
    ai_prob_before = metrics_before.get("ai_probability", 0.0)

    try:
        from langchain.schema import SystemMessage, HumanMessage

        style_guide = req.style_guide or "保持与原作品一致的风格"
        wordlist_note = ""
        if req.anti_ai_wordlist:
            wordlist_note = f"\n【禁用词汇】以下词汇须替换：{', '.join(req.anti_ai_wordlist[:30])}"
        patterns_note = ""
        if req.banned_patterns:
            patterns_note = f"\n【禁用句式】{'; '.join(req.banned_patterns[:10])}"

        system_prompt = (
            _ANTI_DETECT_SYSTEM
            .replace("{style_guide}", style_guide)
            .replace("{intensity}", req.intensity)
            + wordlist_note + patterns_note
        )

        llm = build_llm(req.llm_config, default_temperature=0.85, default_max_tokens=8192)

        response = await llm.ainvoke([
            SystemMessage(content=system_prompt),
            HumanMessage(content=f"请改写以下章节文本：\n\n{req.text[:8000]}"),
        ])

        raw = response.content.strip()
        if raw.startswith("```"):
            raw = re.sub(r"^```[a-z]*\n?", "", raw)
            raw = re.sub(r"\n?```$", "", raw)

        data = json.loads(raw)
        rewritten = data.get("rewritten_text", req.text)
        changes = data.get("changes_made", [])

    except json.JSONDecodeError:
        # LLM didn't return JSON — treat entire response as rewritten text
        rewritten = response.content.strip()  # type: ignore[possibly-undefined]
        logger.warning(
            "Anti-detect LLM returned non-JSON, using raw output | raw_content: %.500s",
            rewritten,
        )
        changes = ["格式解析失败，使用原始输出"]
    except Exception as exc:
        logger.error("Anti-detect rewrite failed: %s", repr(exc), exc_info=True)
        raise HTTPException(status_code=500, detail=f"Anti-detect rewrite failed: {exc}")

    metrics_after = metrics_estimator.estimate(rewritten)
    ai_prob_after = metrics_after.get("ai_probability", 0.0)

    return {
        "original_text": req.text,
        "rewritten_text": rewritten,
        "changes_made": changes,
        "ai_prob_before": ai_prob_before,
        "ai_prob_after": ai_prob_after,
    }


# ═══════════════════════════════════════════════════════════════════════════════
# NARRATIVE REVISE (叙事修复)
# ═══════════════════════════════════════════════════════════════════════════════

_NARRATIVE_REVISE_SYSTEM = """你是一位专业的网文编辑，擅长根据审核报告精准修复章节中的叙事问题。

修改原则：
1. 只修改审核报告指出的具体问题，不过度改写
2. 保持情节连贯性，修复逻辑漏洞
3. 保持人物性格一致，根据人设调整对话和行为
4. 修正时间线矛盾，不改变核心情节走向
5. 保持原有文风

输出严格JSON格式：{"rewritten_text": "...修改后全文...", "changes_made": ["修改说明1", "修改说明2"]}"""


@router.post("/narrative-revise")
async def narrative_revise(req: NarrativeReviseRequest):
    """Targeted narrative revision based on audit report failing dimensions."""
    if not req.llm_config.get("api_key"):
        raise HTTPException(status_code=400, detail="llm_config.api_key is required for narrative revision")

    try:
        from langchain.schema import SystemMessage, HumanMessage

        dims_note = ""
        if req.failing_dimensions:
            dims_note = f"\n\n《审核失败维度》: {', '.join(req.failing_dimensions)}"
        issues_note = ""
        if req.top_issues:
            issues_note = "\n《具体问题》:\n" + "\n".join(f"- {issue}" for issue in req.top_issues[:10])

        llm = build_llm(req.llm_config, default_temperature=0.5, default_max_tokens=8192)

        user_content = (
            f"请根据以下审核问题修改章节内容："
            f"{dims_note}{issues_note}"
            f"\n\n《章节内容》:\n{req.chapter_text[:8000]}"
        )

        response = await llm.ainvoke([
            SystemMessage(content=_NARRATIVE_REVISE_SYSTEM),
            HumanMessage(content=user_content),
        ])

        data = _repair_json(response.content)
        rewritten = data.get("rewritten_text") or req.chapter_text
        changes = data.get("changes_made", [])
        if not isinstance(changes, list):
            changes = [str(changes)]

    except Exception as exc:
        logger.error("Narrative revision failed: %s", repr(exc), exc_info=True)
        raise HTTPException(status_code=500, detail=f"Narrative revision failed: {exc}")

    return {
        "original_text": req.chapter_text,
        "rewritten_text": rewritten,
        "changes_made": changes,
    }


# ═══════════════════════════════════════════════════════════════════════════════
# CREATIVE BRIEF → STORY BIBLE + BOOK RULES
# ═══════════════════════════════════════════════════════════════════════════════

_BRIEF_SYSTEM = """你是一位资深网文策划，擅长从零碎的创意简报生成完整的故事设定和写作规则。

根据用户提供的创作简报，生成：
1. 世界圣经（world_bible）：完整的世界观、核心人物、规则体系
2. 创作规则（rules_content）：故事的创作约束和基本规律
3. 风格指南（style_guide）：文风、叙事视角、节奏要求
4. AI禁用词列表（anti_ai_wordlist）：该书应该避免的AI味表达词汇
5. 禁用句式（banned_patterns）：该书风格中应避免的句式模式

返回严格的JSON格式：
{
  "world_bible": {
    "world_overview": "...",
    "power_system": {...},
    "main_characters": [...],
    "key_locations": [...],
    "core_conflicts": [...],
    "world_rules": [...],
    "prohibited": [...]
  },
  "rules_content": "...",
  "style_guide": "...",
  "anti_ai_wordlist": ["词1", "词2"],
  "banned_patterns": ["句式1", "句式2"]
}

确保world_bible详尽、rules_content具体可操作、style_guide有针对性。"""


@router.post("/creative-brief")
async def generate_creative_brief(req: CreativeBriefRequest):
    """Generate story_bible + book_rules from a creative brief document."""
    import json

    if not req.llm_config.get("api_key"):
        raise HTTPException(status_code=400, detail="llm_config.api_key is required")

    try:
        from langchain.schema import SystemMessage, HumanMessage

        llm = build_llm(req.llm_config, default_temperature=0.7, default_max_tokens=8192)

        human_content = f"【题材】{req.genre}\n\n【创作简报】\n{req.brief_text[:6000]}"
        response = await llm.ainvoke([
            SystemMessage(content=_BRIEF_SYSTEM),
            HumanMessage(content=human_content),
        ])

        raw = response.content.strip()
        if raw.startswith("```"):
            raw = re.sub(r"^```[a-z]*\n?", "", raw)
            raw = re.sub(r"\n?```$", "", raw)

        return json.loads(raw)

    except Exception as exc:
        logger.error("Creative brief generation failed: %s", repr(exc), exc_info=True)
        raise HTTPException(status_code=500, detail=f"Creative brief generation failed: {exc}")


# ═══════════════════════════════════════════════════════════════════════════════
# CHAPTER IMPORT
# ═══════════════════════════════════════════════════════════════════════════════


@router.post("/import-chapters/analyze")
async def import_chapters_analyze(req: ImportChaptersRequest):
    """
    Split source text into chapters.
    Returns chapters list + analysis dict.
    This is designed to be called as a background task.
    """
    # Step 1: Split into chapters
    try:
        pattern = req.split_pattern or r"第.{1,4}[章节回]"
        parts = re.split(f"({pattern})", req.source_text)
    except re.error:
        parts = re.split(r"(第.{1,4}[章节回])", req.source_text)

    chapters = []
    title_buf = ""
    for part in parts:
        match = re.match(req.split_pattern or r"第.{1,4}[章节回]", part)
        if match:
            title_buf = part.strip()
        else:
            content = part.strip()
            if content and len(content) > 50:
                chapters.append({
                    "title": title_buf or f"第{len(chapters)+1}章",
                    "content": content,
                    "chapter_num": len(chapters) + 1,
                })
                title_buf = ""

    if not chapters:
        # No chapter markers found — treat entire text as one chapter
        chapters = [{"title": "第1章", "content": req.source_text, "chapter_num": 1}]

    # LLM-based reverse engineering (only when llm_config.api_key is provided)
    reverse_engineered: dict = {}
    if req.llm_config.get("api_key") and chapters:
        try:
            from langchain.schema import SystemMessage, HumanMessage

            sample_text = "\n\n".join(
                ch["content"][:2000] for ch in chapters[:3]
            )
            _RE_SYSTEM = """从以下网文章节中提取世界构建元素，返回严格JSON（不要Markdown代码块）：
{"characters":[{"name":"","role_type":"protagonist|antagonist|supporting","profile":{"description":"","traits":[]}}],
"foreshadowings":[{"content":"","embed_method":"explicit|implicit","priority":5}],
"glossary":[{"term":"","definition":"","category":"place|item|concept|power"}]}
只提取明确出现的元素，不要推测。"""
            llm = build_llm(req.llm_config, default_temperature=0.2, default_max_tokens=4096)
            response = await llm.ainvoke([
                SystemMessage(content=_RE_SYSTEM),
                HumanMessage(content=f"分析以下章节：\n\n{sample_text}"),
            ])
            reverse_engineered = _repair_json(response.content)
        except Exception as exc:
            logger.warning("reverse engineering LLM call failed: %s", repr(exc), exc_info=True)

    return {
        "import_id": req.import_id,
        "chapters": chapters,
        "total_chapters": len(chapters),
        "reverse_engineered": reverse_engineered,
    }


# ═══════════════════════════════════════════════════════════════════════════════
