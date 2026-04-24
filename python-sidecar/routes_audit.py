from __future__ import annotations
import asyncio
import json
import logging
import os
import re
import math
import uuid
from typing import Optional

import httpx
import psycopg2
import psycopg2.extras
from fastapi import APIRouter, BackgroundTasks, HTTPException
from fastapi.responses import StreamingResponse
from pydantic import BaseModel

from json_repair import repair_json
from llm_utils import ainvoke_text, build_invoke_config

logger = logging.getLogger("python-agent")


# ── Pydantic request models (defined locally to avoid circular imports with main.py) ─
class AuditChapterRequest(BaseModel):
    chapter_id: str
    project_id: str
    chapter_text: str
    chapter_num: int = 1
    context: dict = {}
    llm_config: dict = {}

class AntiDetectRequest(BaseModel):
    chapter_id: str
    text: str
    intensity: str = "medium"
    style_guide: str = ""
    anti_ai_wordlist: list[str] = []
    banned_patterns: list[str] = []
    llm_config: dict = {}

class CreativeBriefRequest(BaseModel):
    brief_text: str
    genre: str = "现代都市"
    llm_config: dict = {}

class ImportChaptersRequest(BaseModel):
    project_id: str
    import_id: str
    source_text: str
    split_pattern: str = r"第.{1,4}[章节回]"
    fanfic_mode: Optional[str] = None
    llm_config: dict = {}

class NarrativeReviseRequest(BaseModel):
    chapter_id: str
    chapter_text: str
    failing_dimensions: list[str] = []
    top_issues: list[str] = []
    llm_config: dict = {}


def get_db():
    """Get a connection from the shared pool in main.py."""
    from main import get_db as _get_db
    return _get_db()

def put_db(conn):
    from main import put_db as _put_db
    _put_db(conn)

def get_qdrant():
    from vector_store.qdrant_store import QdrantStore
    return QdrantStore.get_instance()

def _get_analyzers():
    """Lazy-import analyzer singletons from main to avoid duplicate instantiation."""
    from main import (style_analyzer, narrative_analyzer, atmosphere_analyzer,
                      plot_extractor, humanizer, metrics_estimator)
    return style_analyzer, narrative_analyzer, atmosphere_analyzer, plot_extractor, humanizer, metrics_estimator


router = APIRouter()


def _prepare_llm_call(
    llm_config: dict,
    *,
    task_name: str,
    stable_key: str | None = None,
    extra_metadata: dict | None = None,
) -> tuple[dict, dict]:
    """Clone llm_config, attach a stable session_id, and build a shared invoke config."""
    cfg = dict(llm_config or {})
    session_id = str(cfg.get("session_id") or "").strip()
    if not session_id:
        if stable_key:
            session_id = f"{task_name}:{stable_key}"
        else:
            session_id = f"{task_name}:{uuid.uuid4()}"
        cfg["session_id"] = session_id
    return cfg, build_invoke_config(
        cfg,
        session_id=session_id,
        task_name=task_name,
        extra_metadata=extra_metadata,
    )


def _coerce_positive_int(value: object, default: int) -> int:
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        return default
    return parsed if parsed > 0 else default


def _llm_input_char_budget(
    llm_cfg: dict,
    *,
    default_max_tokens: int,
    min_chars: int,
    max_chars: int,
) -> int:
    max_tokens = _coerce_positive_int((llm_cfg or {}).get("max_tokens"), default_max_tokens)
    estimated = int(max_tokens * 1.8)
    return max(min_chars, min(max_chars, estimated))


def _truncate_for_llm(
    text: str,
    llm_cfg: dict,
    *,
    default_max_tokens: int,
    min_chars: int,
    max_chars: int,
    overhead_chars: int = 0,
) -> str:
    budget = _llm_input_char_budget(
        llm_cfg,
        default_max_tokens=default_max_tokens,
        min_chars=min_chars,
        max_chars=max_chars,
    )
    allowed = max(min_chars // 2, budget - max(overhead_chars, 0))
    return text[:allowed]

# 33-DIMENSION CHAPTER AUDIT
# ═══════════════════════════════════════════════════════════════════════════════

_AUDIT_SYSTEM = """你是一位专业的中文网络小说审核员，请对下面的章节草稿进行严格的多维度审计。

你必须严格按照以下33个维度逐一评分，每个维度给出0.0-1.0的分数。

返回严格的JSON格式（不要输出其他内容）：
{
  "dimensions": [
    {"name": "character_memory", "score": 0.9, "issues": ""},
    {"name": "resource_continuity", "score": 0.8, "issues": ""},
    {"name": "foreshadowing_recovery", "score": 0.7, "issues": "伏笔X尚未回收；伏笔Y未解决"},
    {"name": "outline_deviation", "score": 0.9, "issues": ""},
    {"name": "narrative_pace", "score": 0.8, "issues": ""},
    {"name": "emotional_arc", "score": 0.8, "issues": ""},
    {"name": "world_rule_compliance", "score": 0.9, "issues": ""},
    {"name": "timeline_consistency", "score": 0.9, "issues": ""},
    {"name": "pov_consistency", "score": 0.9, "issues": ""},
    {"name": "dialogue_naturalness", "score": 0.8, "issues": ""},
    {"name": "scene_description_quality", "score": 0.7, "issues": ""},
    {"name": "conflict_escalation", "score": 0.8, "issues": ""},
    {"name": "character_motivation", "score": 0.9, "issues": ""},
    {"name": "tension_management", "score": 0.7, "issues": ""},
    {"name": "hook_strength", "score": 0.8, "issues": ""},
    {"name": "subplot_advancement", "score": 0.8, "issues": ""},
    {"name": "relationship_dynamics", "score": 0.9, "issues": ""},
    {"name": "power_system_consistency", "score": 0.9, "issues": ""},
    {"name": "geographic_consistency", "score": 0.9, "issues": ""},
    {"name": "prop_continuity", "score": 0.9, "issues": ""},
    {"name": "language_variety", "score": 0.7, "issues": ""},
    {"name": "sentence_rhythm", "score": 0.7, "issues": ""},
    {"name": "vocabulary_richness", "score": 0.7, "issues": ""},
    {"name": "cliche_density", "score": 0.8, "issues": ""},
    {"name": "ai_pattern_detection", "score": 0.8, "issues": ""},
    {"name": "repetitive_sentence_structure", "score": 0.8, "issues": ""},
    {"name": "excessive_summarization", "score": 0.9, "issues": ""},
    {"name": "high_freq_ai_words", "score": 0.9, "issues": ""},
    {"name": "show_vs_tell", "score": 0.7, "issues": ""},
    {"name": "sensory_detail", "score": 0.7, "issues": ""},
    {"name": "inner_monologue_quality", "score": 0.8, "issues": ""},
    {"name": "chapter_length_adequacy", "score": 0.9, "issues": ""},
    {"name": "ending_hook", "score": 0.8, "issues": ""}
  ],
  "overall_score": 0.82,
  "ai_probability": 0.3,
  "top_issues": "问题1；问题2"
}

规则：score为0.0-1.0的小数，issues为空字符串或以"；"分隔的问题描述，top_issues同样用"；"分隔。
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

    # genre_compliance — detect genre-breaking elements
    genre = context.get("genre", "")
    if genre:
        _GENRE_FORBIDDEN_AUDIT = {
            "西幻": ["修炼", "丹药", "灵石", "宗门", "渡劫", "飞升", "灵气", "元神", "金丹", "内功"],
            "玄幻": ["精灵族", "矮人", "兽人", "骑士团", "魔杖", "咒语", "手机", "电脑", "枪械"],
            "末世": ["修炼", "丹药", "灵石", "宗门", "精灵", "矮人", "魔法阵", "咒语"],
            "科幻": ["修炼", "丹药", "灵石", "功法", "飞升", "魔法", "咒语", "魔杖"],
            "都市": ["修炼飞升", "魔法", "精灵", "星际", "末世灾变"],
        }
        _SYS_BREAK = [
            "系统面板", "系统提示", "任务面板", "经验值", "技能树", "技能点",
            "属性面板", "等级提升", "升级提示", "技能冷却",
        ]
        forbidden = _GENRE_FORBIDDEN_AUDIT.get(genre, [])
        genre_hits = [w for w in forbidden if w in text]
        sys_hits = [w for w in _SYS_BREAK if w in text] if genre != "游戏" else []
        all_genre_hits = genre_hits + sys_hits
        genre_score = max(0.0, 1.0 - len(all_genre_hits) * 0.12)
        dimensions["genre_compliance"] = {
            "score": genre_score,
            "passed": len(all_genre_hits) == 0,
            "issues": [f"题材违规（{genre}）：出现 {', '.join(all_genre_hits[:6])}"] if all_genre_hits else [],
        }
    else:
        dimensions["genre_compliance"] = {"score": 1.0, "passed": True, "issues": []}

    return dimensions


@router.post("/audit/chapter")
async def audit_chapter(req: AuditChapterRequest):
    """
    33-dimension chapter audit.
    Phase 1: fast heuristic (no LLM).
    Phase 2: LLM deep eval (if llm_config provided).
    """
    heuristic_dims = _heuristic_audit(req.chapter_text, req.context)

    llm_dims: dict = {}
    ai_probability = 0.0
    top_issues: list = []

    if req.llm_config.get("api_key"):
        try:
            from langchain.schema import SystemMessage, HumanMessage

            llm_cfg, _ = _prepare_llm_call(
                req.llm_config,
                task_name="audit_chapter",
                stable_key=req.chapter_id,
                extra_metadata={"project_id": req.project_id, "chapter_num": req.chapter_num},
            )
            llm_cfg.setdefault("temperature", 0.2)
            llm_cfg.setdefault("max_tokens", 3000)

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

            chapter_excerpt = _truncate_for_llm(
                req.chapter_text,
                llm_cfg,
                default_max_tokens=3000,
                min_chars=2200,
                max_chars=6000,
                overhead_chars=len(context_str) + 64,
            )

            human_content = (
                f"{context_str}\n\n"
                f"【章节正文（第{req.chapter_num}章）】\n{chapter_excerpt}"
            )

            raw, _ = await ainvoke_text(
                human_content,
                llm_cfg,
                system_prompt=_AUDIT_SYSTEM,
                session_id=llm_cfg.get("session_id") or None,
                task_name="audit_chapter",
                extra_metadata={"project_id": req.project_id, "chapter_num": req.chapter_num},
            )
            raw = raw.strip()
            # Strip markdown code fences if present
            if raw.startswith("```"):
                raw = re.sub(r"^```[a-z]*\n?", "", raw)
                raw = re.sub(r"\n?```$", "", raw)

            data = repair_json(raw)
            if not data:
                logger.warning(
                    "audit_chapter: repair_json returned empty, raw snippet: %.500s", raw
                )
                data = {}
            # Convert flat array format to the map format expected by Go
            raw_dims = data.get("dimensions", [])
            llm_dims = {}
            for d in (raw_dims if isinstance(raw_dims, list) else []):
                name = d.get("name", "")
                if name:
                    issues_str = d.get("issues", "")
                    issues_list = [i.strip() for i in issues_str.split("；") if i.strip()] if issues_str else []
                    score = float(d.get("score", 0.8))
                    llm_dims[name] = {"score": score, "passed": score >= 0.65, "issues": issues_list}
            ai_probability = data.get("ai_probability", 0.0)
            top_issues_raw = data.get("top_issues", "")
            if isinstance(top_issues_raw, str):
                top_issues = [i.strip() for i in top_issues_raw.split("；") if i.strip()]
            elif isinstance(top_issues_raw, list):
                top_issues = top_issues_raw
            else:
                top_issues = []

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
        "ending_hook", "genre_compliance",
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
8. 【严禁分点作答】绝不使用分点列举格式（如：首先、其次、然后、最后、第一、第二等）
9. 【严禁学术化表述】绝不使用学术化过渡词（如：一方面...另一方面、综上所述、值得注意的是、毋庸置疑、从某种程度上说等）

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
    if not req.llm_config.get("api_key"):
        raise HTTPException(status_code=400, detail="llm_config.api_key is required for anti-detect rewrite")

    # Measure AI probability before
    _, _, _, _, _, metrics_estimator = _get_analyzers()
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

        llm_cfg, _ = _prepare_llm_call(
            req.llm_config,
            task_name="anti_detect_rewrite",
            stable_key=req.chapter_id,
            extra_metadata={"chapter_id": req.chapter_id, "intensity": req.intensity},
        )
        llm_cfg.setdefault("temperature", 0.85)
        llm_cfg.setdefault("max_tokens", 8192)

        rewrite_excerpt = _truncate_for_llm(
            req.text,
            llm_cfg,
            default_max_tokens=8192,
            min_chars=3000,
            max_chars=8000,
            overhead_chars=32,
        )

        raw, _ = await ainvoke_text(
            f"请改写以下章节文本：\n\n{rewrite_excerpt}",
            llm_cfg,
            system_prompt=system_prompt,
            session_id=llm_cfg.get("session_id") or None,
            task_name="anti_detect_rewrite",
            extra_metadata={"chapter_id": req.chapter_id, "intensity": req.intensity},
        )
        raw = raw.strip()
        if raw.startswith("```"):
            raw = re.sub(r"^```[a-z]*\n?", "", raw)
            raw = re.sub(r"\n?```$", "", raw)

        data = repair_json(raw)
        if data and data.get("rewritten_text"):
            rewritten = data["rewritten_text"]
            changes_val = data.get("changes_made", [])
            changes = changes_val if isinstance(changes_val, list) else ["格式解析部分失败"]
        else:
            logger.warning(
                "Anti-detect rewrite: JSON repair failed or missing rewritten_text, using raw LLM output "
                "| raw snippet: %.500s", raw
            )
            rewritten = raw
            changes = ["格式解析失败，使用原始输出"]
    except Exception as exc:
        logger.error("Anti-detect rewrite failed: %s", repr(exc), exc_info=True)
        raise HTTPException(status_code=500, detail=f"Anti-detect rewrite failed: {exc}")

    _, _, _, _, _, me = _get_analyzers()
    metrics_after = me.estimate(rewritten)
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
6. 严禁使用分点作答格式（首先、其次、然后、最后等）
7. 严禁使用学术化表述（一方面...另一方面、综上所述、总而言之等）

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

        llm_cfg, _ = _prepare_llm_call(
            req.llm_config,
            task_name="narrative_revise",
            stable_key=req.chapter_id,
            extra_metadata={"chapter_id": req.chapter_id, "issue_count": len(req.top_issues)},
        )
        llm_cfg.setdefault("temperature", 0.5)
        llm_cfg.setdefault("max_tokens", 8192)

        chapter_excerpt = _truncate_for_llm(
            req.chapter_text,
            llm_cfg,
            default_max_tokens=8192,
            min_chars=3000,
            max_chars=8000,
            overhead_chars=len(dims_note) + len(issues_note) + 64,
        )

        user_content = (
            f"请根据以下审核问题修改章节内容："
            f"{dims_note}{issues_note}"
            f"\n\n《章节内容》:\n{chapter_excerpt}"
        )

        raw, _ = await ainvoke_text(
            user_content,
            llm_cfg,
            system_prompt=_NARRATIVE_REVISE_SYSTEM,
            session_id=llm_cfg.get("session_id") or None,
            task_name="narrative_revise",
            extra_metadata={"chapter_id": req.chapter_id, "issue_count": len(req.top_issues)},
        )

        data = _repair_json(raw)
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

返回严格的JSON格式（world_bible内所有字段必须为字符串，多项内容用"；"分隔）：
{
  "world_bible": {
    "world_overview": "世界观综合描述文字",
    "power_system": "力量体系描述（文字描述，不使用嵌套对象）",
    "main_characters": "角色1（主角）：背景简介；角色2（配角）：背景简介",
    "key_locations": "地点1；地点2；地点3",
    "core_conflicts": "核心冲突1；核心冲突2",
    "world_rules": "规则1；规则2；规则3",
    "prohibited": "禁止内容1；禁止内容2"
  },
  "rules_content": "创作规则的详细描述文本",
  "style_guide": "风格指南描述文本",
  "anti_ai_wordlist": ["词1", "词2", "词3"],
  "banned_patterns": ["句式1", "句式2"]
}

确保world_bible详尽、rules_content具体可操作、style_guide有针对性。禁止输出JSON以外的内容。"""


@router.post("/creative-brief")
async def generate_creative_brief(req: CreativeBriefRequest):
    """Generate story_bible + book_rules from a creative brief document."""
    if not req.llm_config.get("api_key"):
        raise HTTPException(status_code=400, detail="llm_config.api_key is required")

    try:
        from langchain.schema import SystemMessage, HumanMessage

        llm_cfg, _ = _prepare_llm_call(
            req.llm_config,
            task_name="creative_brief",
            extra_metadata={"genre": req.genre},
        )
        llm_cfg.setdefault("temperature", 0.7)
        llm_cfg.setdefault("max_tokens", 8192)

        brief_excerpt = _truncate_for_llm(
            req.brief_text,
            llm_cfg,
            default_max_tokens=8192,
            min_chars=2500,
            max_chars=6000,
            overhead_chars=len(req.genre) + 16,
        )

        human_content = f"【题材】{req.genre}\n\n【创作简报】\n{brief_excerpt}"
        raw, _ = await ainvoke_text(
            human_content,
            llm_cfg,
            system_prompt=_BRIEF_SYSTEM,
            session_id=llm_cfg.get("session_id") or None,
            task_name="creative_brief",
            extra_metadata={"genre": req.genre},
        )
        raw = raw.strip()
        if raw.startswith("```"):
            raw = re.sub(r"^```[a-z]*\n?", "", raw)
            raw = re.sub(r"\n?```$", "", raw)

        result = repair_json(raw)
        if not result:
            logger.error(
                "generate_creative_brief: repair_json returned empty | raw snippet: %.500s", raw
            )
            raise HTTPException(status_code=502, detail="LLM returned invalid JSON for creative brief")
        return result

    except HTTPException:
        raise
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
{"characters":[{"name":"","role_type":"protagonist|antagonist|supporting","profile":{"description":"","traits":"特点1，特点2"}}],
"foreshadowings":[{"content":"","embed_method":"explicit|implicit","priority":5}],
"glossary":[{"term":"","definition":"","category":"place|item|concept|power"}]}
profile.traits为逗号分隔的字符串。只提取明确出现的元素，不要推测。"""
            llm_cfg, _ = _prepare_llm_call(
                req.llm_config,
                task_name="import_chapters_reverse_engineer",
                stable_key=req.import_id,
                extra_metadata={"project_id": req.project_id, "fanfic_mode": req.fanfic_mode or ""},
            )
            llm_cfg.setdefault("temperature", 0.2)
            llm_cfg.setdefault("max_tokens", 4096)
            raw, _ = await ainvoke_text(
                f"分析以下章节：\n\n{sample_text}",
                llm_cfg,
                system_prompt=_RE_SYSTEM,
                session_id=llm_cfg.get("session_id") or None,
                task_name="import_chapters_reverse_engineer",
                extra_metadata={"project_id": req.project_id, "fanfic_mode": req.fanfic_mode or ""},
            )
            reverse_engineered = _repair_json(raw)
        except Exception as exc:
            logger.warning("reverse engineering LLM call failed: %s", repr(exc), exc_info=True)

    return {
        "import_id": req.import_id,
        "chapters": chapters,
        "total_chapters": len(chapters),
        "reverse_engineered": reverse_engineered,
    }


# ═══════════════════════════════════════════════════════════════════════════════
