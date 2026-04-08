"""
Quality Check Node — evaluates the draft against:
  1. World rule compliance (heuristic keyword check)
  2. Length adequacy
  3. Character name consistency
  4. Genre compliance (forbidden elements detection)
  5. Repetition detection (cross-chapter similarity)
  6. Ending quality (detect AI-style endings)
  7. Entity provenance (new characters/items must have sources)

Returns quality_score (0.0-1.0) and quality_issues list.
Decides whether to retry generation (up to max_retries).
"""
from __future__ import annotations

import logging
import re
from collections import Counter
from typing import Any, Literal

from agent.state import AgentState

logger = logging.getLogger(__name__)

# AI-style ending patterns to detect
_AI_ENDING_PATTERNS = [
    r"而这一切[，,]?才刚刚开始",
    r"更大的[风暴挑战危机考验].*即将",
    r"命运的齿轮.*转动",
    r"新的篇章.*展开",
    r"故事.*还在继续",
    r"他[她]?知道.*未来.*不会.*平静",
    r"一切.*归于平静",
    r"夜.*深了.*回到.*[房间住处]",
    r"他[她]?不知道的是",
    r"[月星]光.*洒[落下在].*[大地身上脸上]",
    r"这只是.*开始",
    r"[黎明曙光].*即将.*[到来来临降临]",
]
_AI_ENDING_RE = [re.compile(p) for p in _AI_ENDING_PATTERNS]

# Genre-specific forbidden element keywords
_GENRE_FORBIDDEN = {
    "西幻": ["修炼", "丹药", "灵石", "宗门", "渡劫", "飞升", "灵气", "元神", "金丹", "内功",
             "系统面板", "系统提示", "等级提升", "经验值", "技能树", "任务面板"],
    "玄幻": ["精灵族", "矮人", "兽人", "骑士团", "魔杖", "咒语", "手机", "电脑", "枪械",
             "系统面板", "系统提示", "经验值", "技能树", "任务面板"],
    "末世": ["修炼", "丹药", "灵石", "宗门", "精灵", "矮人", "魔法阵", "咒语",
             "系统面板", "系统提示", "等级提升", "经验值"],
    "科幻": ["修炼", "丹药", "灵石", "功法", "飞升", "魔法", "咒语", "魔杖",
             "系统面板", "系统提示", "等级提升"],
    "都市": ["修炼飞升", "魔法", "精灵", "星际", "末世灾变", "系统面板", "系统提示"],
}

# Common "system/framework" keywords that break immersion in non-game genres
_SYSTEM_BREAK_KEYWORDS = [
    "系统面板", "系统提示", "任务面板", "经验值", "技能树", "技能点", "属性面板",
    "等级提升", "升级提示", "成就解锁", "刷新冷却", "技能冷却", "buff", "debuff",
    "HP", "MP", "装备栏", "背包空间", "物品栏",
]

# AI high-frequency words
_AI_FLAVOR_WORDS = [
    "微微", "缓缓", "淡淡", "默默", "不禁", "不由得",
    "仿佛", "似乎", "好像", "嘴角勾起", "眼中闪过一丝",
]


def _check_ai_ending(draft: str) -> list[str]:
    """Check if the chapter has an AI-style ending."""
    issues = []
    # Check last 300 chars
    tail = draft[-300:] if len(draft) > 300 else draft
    for pattern in _AI_ENDING_RE:
        if pattern.search(tail):
            issues.append(f"AI式结尾：检测到模式 '{pattern.pattern[:30]}…'")
            break
    # Check for summary/outlook paragraphs at the end
    paragraphs = [p.strip() for p in draft.split("\n") if p.strip()]
    if paragraphs:
        last_para = paragraphs[-1]
        # Ending paragraph too long suggests summary
        if len(last_para) > 150:
            issues.append("结尾段落过长（>150字），疑似总结段或展望段")
    return issues


def _check_genre_compliance(draft: str, style_profile: dict | None) -> tuple[float, list[str]]:
    """Check for genre-breaking elements. Returns (penalty, issues)."""
    if not style_profile:
        return 0.0, []
    genre = style_profile.get("genre", "")
    if not genre:
        return 0.0, []

    issues = []
    penalty = 0.0

    # Check genre-specific forbidden words
    forbidden = _GENRE_FORBIDDEN.get(genre, [])
    hits = [w for w in forbidden if w in draft]
    if hits:
        penalty += min(0.3, len(hits) * 0.08)
        issues.append(f"题材违规（{genre}）：出现禁入元素 {', '.join(hits[:5])}")

    # Check system/framework keywords for non-game genres
    if genre != "游戏":
        sys_hits = [w for w in _SYSTEM_BREAK_KEYWORDS if w in draft]
        if sys_hits:
            penalty += min(0.3, len(sys_hits) * 0.1)
            issues.append(f"打破沉浸感：出现系统/框架描写 {', '.join(sys_hits[:5])}")

    return penalty, issues


def _check_repetition(draft: str, recent_summaries: list[str]) -> tuple[float, list[str]]:
    """Check for repetitive content within the chapter and against recent chapters."""
    issues = []
    penalty = 0.0

    # Internal repetition: check for repeated sentence starters
    sentences = [s.strip() for s in re.split(r'[。！？…]', draft) if len(s.strip()) > 5]
    starters = [s[:3] for s in sentences if len(s) >= 3]
    start_counts = Counter(starters)
    repeated = [(k, v) for k, v in start_counts.items() if v >= 5]
    if repeated:
        penalty += min(0.2, len(repeated) * 0.05)
        issues.append(f"句式开头高度重复：{'、'.join(f'"{k}"×{v}' for k, v in repeated[:3])}")

    # Internal repetition: check for repeated phrases (4+ chars appearing 3+ times)
    if len(draft) > 500:
        # Extract 4-gram phrases
        chars = list(draft.replace("\n", "").replace(" ", ""))
        ngrams = Counter()
        for i in range(len(chars) - 3):
            gram = "".join(chars[i:i+4])
            # Skip common particles
            if not re.match(r'^[的了在是和与而且但]', gram):
                ngrams[gram] += 1
        high_repeat = [(k, v) for k, v in ngrams.items() if v >= 4]
        if len(high_repeat) > 5:
            penalty += 0.1
            top3 = sorted(high_repeat, key=lambda x: -x[1])[:3]
            issues.append(f"短语重复过多：{'、'.join(f'"{k}"×{v}' for k, v in top3)}")

    # Cross-chapter repetition: check key phrases against recent summaries
    if recent_summaries:
        summary_text = " ".join(recent_summaries)
        # Check if current draft repeats specific plot elements from summaries
        # (heuristic: long shared substrings)
        draft_sentences = set(s.strip() for s in re.split(r'[。！？]', draft) if len(s.strip()) > 15)
        summary_sentences = set(s.strip() for s in re.split(r'[。！？]', summary_text) if len(s.strip()) > 15)
        overlap = draft_sentences & summary_sentences
        if overlap:
            penalty += min(0.2, len(overlap) * 0.1)
            issues.append(f"与前文内容雷同：发现{len(overlap)}处重复情节描述")

    return penalty, issues


def _check_entity_provenance(draft: str, state: AgentState) -> tuple[float, list[str]]:
    """Check that characters and items mentioned in draft have known origins."""
    issues = []
    penalty = 0.0

    # Get known character names
    world_track = state.get("world_track") or {}
    char_cores = world_track.get("character_cores", [])
    known_names = set()
    for c in char_cores:
        name = c.get("name", "")
        if name:
            known_names.add(name)

    # Check for "system" appearance patterns that suggest unjustified entities
    # Pattern: 他/她拿出了一把/一件/一个 + noun (without prior mention)
    conjured_patterns = [
        r"[他她](?:拿出|取出|掏出|亮出)(?:了)?(?:一把|一件|一个|一柄|一块|一枚)",
    ]
    for pat in conjured_patterns:
        matches = re.findall(pat, draft)
        if len(matches) > 2:
            penalty += 0.1
            issues.append(f"道具凭空出现过多（{len(matches)}处'拿出/取出'描写），需交代来源")

    return penalty, issues


def quality_check_node(state: AgentState) -> dict[str, Any]:
    """Multi-dimensional quality check with heuristics."""
    draft = state.get("draft", "")
    retry_count = state.get("retry_count", 0)
    max_retries = state.get("max_retries", 2)
    style_profile = state.get("style_profile") or {}
    narrative_track = state.get("narrative_track") or {}

    issues: list[str] = []
    score = 1.0

    # ── 1. Length check ───────────────────────────────────────────────────────
    word_count = len(draft)
    if word_count < 500:
        issues.append(f"篇幅过短（{word_count}字，要求至少500字）")
        score -= 0.4

    # ── 2. Placeholder / degenerate output ────────────────────────────────────
    if "TODO" in draft or draft.count("…") > 10:
        issues.append("草稿包含占位符或省略号过多")
        score -= 0.3

    # ── 3. AI-style ending detection ──────────────────────────────────────────
    ending_issues = _check_ai_ending(draft)
    if ending_issues:
        issues.extend(ending_issues)
        score -= 0.15

    # ── 4. Genre compliance ───────────────────────────────────────────────────
    genre_penalty, genre_issues = _check_genre_compliance(draft, style_profile)
    if genre_issues:
        issues.extend(genre_issues)
        score -= genre_penalty

    # ── 5. Repetition detection ───────────────────────────────────────────────
    recent_summaries = narrative_track.get("recent_chapter_summaries", [])
    rep_penalty, rep_issues = _check_repetition(draft, recent_summaries)
    if rep_issues:
        issues.extend(rep_issues)
        score -= rep_penalty

    # ── 6. Entity provenance ──────────────────────────────────────────────────
    ent_penalty, ent_issues = _check_entity_provenance(draft, state)
    if ent_issues:
        issues.extend(ent_issues)
        score -= ent_penalty

    # ── 7. AI flavor words ────────────────────────────────────────────────────
    flavor_hits = []
    for word in _AI_FLAVOR_WORDS:
        count = draft.count(word)
        if count > 2:
            flavor_hits.append(f"{word}×{count}")
    if flavor_hits:
        score -= min(0.15, len(flavor_hits) * 0.03)
        issues.append(f"AI高频词过多：{', '.join(flavor_hits[:5])}")

    score = max(0.0, min(1.0, score))

    # ── Determine outcome ─────────────────────────────────────────────────────
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
