"""
Layer 1: 风格指纹分析器
使用jieba分词统计、句长分布、标点频率构建量化风格指纹
"""
import re
import math
from collections import Counter
from typing import Dict, Any, List

import jieba
import jieba.posseg as pseg


class StyleAnalyzer:
    """风格指纹分析器 - 负责提取文本的量化风格特征"""

    # 中文标点符号集
    PUNCTUATIONS = set("，。！？；：、""''（）【】《》——…—")

    def analyze(self, text: str) -> Dict[str, Any]:
        """执行完整的风格指纹分析"""
        sentences = self._split_sentences(text)
        words = list(jieba.cut(text))
        pos_tags = list(pseg.cut(text))

        return {
            "lexical": self._lexical_analysis(words, text),
            "sentence": self._sentence_analysis(sentences),
            "punctuation": self._punctuation_analysis(text),
            "pos_distribution": self._pos_distribution(pos_tags),
            "rhetoric": self._rhetoric_detection(text, sentences),
            "rhythm": self._rhythm_analysis(sentences),
            "vocabulary_richness": self._vocabulary_richness(words),
        }

    def _split_sentences(self, text: str) -> List[str]:
        """按中文句末标点分句"""
        sentences = re.split(r'[。！？\n]+', text)
        return [s.strip() for s in sentences if s.strip()]

    def _lexical_analysis(self, words: List[str], text: str) -> Dict[str, Any]:
        """词汇层分析"""
        # 过滤掉标点和空白
        content_words = [w for w in words if w.strip() and w not in self.PUNCTUATIONS]
        word_lengths = [len(w) for w in content_words]

        if not word_lengths:
            return {"avg_word_length": 0, "unique_ratio": 0, "total_words": 0}

        word_freq = Counter(content_words)
        return {
            "total_words": len(content_words),
            "unique_words": len(word_freq),
            "unique_ratio": round(len(word_freq) / max(len(content_words), 1), 4),
            "avg_word_length": round(sum(word_lengths) / len(word_lengths), 2),
            "top_20_words": word_freq.most_common(20),
            "hapax_legomena_ratio": round(
                sum(1 for v in word_freq.values() if v == 1) / max(len(word_freq), 1), 4
            ),
        }

    def _sentence_analysis(self, sentences: List[str]) -> Dict[str, Any]:
        """句子层分析 - 句长分布"""
        if not sentences:
            return {"avg_length": 0, "std_length": 0, "distribution": {}}

        lengths = [len(s) for s in sentences]
        avg = sum(lengths) / len(lengths)
        variance = sum((l - avg) ** 2 for l in lengths) / len(lengths)
        std = math.sqrt(variance)

        # 句长分布桶: 短句(<10), 中句(10-25), 长句(25-50), 超长句(>50)
        buckets = {"short": 0, "medium": 0, "long": 0, "very_long": 0}
        for l in lengths:
            if l < 10:
                buckets["short"] += 1
            elif l < 25:
                buckets["medium"] += 1
            elif l < 50:
                buckets["long"] += 1
            else:
                buckets["very_long"] += 1

        total = len(lengths)
        distribution = {k: round(v / total, 4) for k, v in buckets.items()}

        return {
            "total_sentences": total,
            "avg_length": round(avg, 2),
            "std_length": round(std, 2),
            "min_length": min(lengths),
            "max_length": max(lengths),
            "distribution": distribution,
            "burstiness_index": round(std / max(avg, 1), 4),
        }

    def _punctuation_analysis(self, text: str) -> Dict[str, Any]:
        """标点符号频率分析"""
        if not text:
            return {}

        punct_counter = Counter()
        total_chars = len(text)

        for ch in text:
            if ch in self.PUNCTUATIONS:
                punct_counter[ch] += 1

        total_punct = sum(punct_counter.values())

        return {
            "total_punctuations": total_punct,
            "punctuation_density": round(total_punct / max(total_chars, 1), 4),
            "frequencies": {
                k: round(v / max(total_chars, 1), 6)
                for k, v in punct_counter.most_common()
            },
            "comma_period_ratio": round(
                punct_counter.get("，", 0) / max(punct_counter.get("。", 0), 1), 2
            ),
            "exclamation_ratio": round(
                punct_counter.get("！", 0) / max(total_punct, 1), 4
            ),
            "question_ratio": round(
                punct_counter.get("？", 0) / max(total_punct, 1), 4
            ),
            "ellipsis_ratio": round(
                punct_counter.get("…", 0) / max(total_punct, 1), 4
            ),
            "dash_ratio": round(
                (punct_counter.get("——", 0) + punct_counter.get("—", 0)) / max(total_punct, 1), 4
            ),
            "dialogue_quote_count": text.count('\u201c') + text.count('\u201d') + text.count('"'),
        }

    def _pos_distribution(self, pos_tags: list) -> Dict[str, Any]:
        """词性分布分析"""
        pos_counter = Counter()
        for word, flag in pos_tags:
            if word.strip() and word not in self.PUNCTUATIONS:
                # 归类大类: n(名词), v(动词), a(形容词), d(副词), p(介词), r(代词), etc.
                major_pos = flag[0] if flag else "x"
                pos_counter[major_pos] += 1

        total = sum(pos_counter.values())
        if total == 0:
            return {}

        return {
            "noun_ratio": round(pos_counter.get("n", 0) / total, 4),
            "verb_ratio": round(pos_counter.get("v", 0) / total, 4),
            "adj_ratio": round(pos_counter.get("a", 0) / total, 4),
            "adv_ratio": round(pos_counter.get("d", 0) / total, 4),
            "pronoun_ratio": round(pos_counter.get("r", 0) / total, 4),
            "full_distribution": {k: round(v / total, 4) for k, v in pos_counter.most_common()},
        }

    def _rhetoric_detection(self, text: str, sentences: List[str]) -> Dict[str, Any]:
        """修辞手法检测"""
        metaphor_patterns = [
            r'像[^。，]*一样',
            r'如同[^。，]*',
            r'仿佛[^。，]*',
            r'好像[^。，]*',
            r'宛如[^。，]*',
            r'犹如[^。，]*',
        ]

        parallel_count = 0
        rhyme_count = 0
        metaphor_count = 0

        for pattern in metaphor_patterns:
            metaphor_count += len(re.findall(pattern, text))

        # 排比检测: 连续3句以上相似开头
        for i in range(len(sentences) - 2):
            if (sentences[i][:2] == sentences[i + 1][:2] == sentences[i + 2][:2]
                    and len(sentences[i]) > 3):
                parallel_count += 1

        return {
            "metaphor_count": metaphor_count,
            "parallel_count": parallel_count,
            "metaphor_density": round(metaphor_count / max(len(sentences), 1), 4),
        }

    def _rhythm_analysis(self, sentences: List[str]) -> Dict[str, Any]:
        """节奏分析 - 通过句长变化模式检测"""
        if len(sentences) < 3:
            return {"pattern": "insufficient_data"}

        lengths = [len(s) for s in sentences]

        # 计算相邻句长差异
        diffs = [abs(lengths[i + 1] - lengths[i]) for i in range(len(lengths) - 1)]
        avg_diff = sum(diffs) / len(diffs) if diffs else 0

        # 节奏模式识别
        # 短-长-短 交替 = 有节奏感
        alternating_count = 0
        for i in range(len(lengths) - 2):
            if (lengths[i] < lengths[i + 1] > lengths[i + 2] or
                    lengths[i] > lengths[i + 1] < lengths[i + 2]):
                alternating_count += 1

        alternating_ratio = alternating_count / max(len(lengths) - 2, 1)

        return {
            "avg_length_change": round(avg_diff, 2),
            "alternating_ratio": round(alternating_ratio, 4),
            "rhythm_type": (
                "rhythmic" if alternating_ratio > 0.5
                else "flowing" if avg_diff < 10
                else "varied"
            ),
        }

    def _vocabulary_richness(self, words: List[str]) -> Dict[str, Any]:
        """词汇丰富度指标"""
        content_words = [w for w in words if w.strip() and w not in self.PUNCTUATIONS and len(w) > 1]
        if not content_words:
            return {"ttr": 0, "yules_k": 0}

        types = len(set(content_words))
        tokens = len(content_words)

        # Type-Token Ratio
        ttr = types / max(tokens, 1)

        # Yule's K measure
        freq_spectrum = Counter(Counter(content_words).values())
        m1 = tokens
        m2 = sum(i * i * freq_spectrum[i] for i in freq_spectrum)
        yules_k = 10000 * (m2 - m1) / max(m1 * m1, 1)

        return {
            "ttr": round(ttr, 4),
            "yules_k": round(yules_k, 2),
            "types": types,
            "tokens": tokens,
        }
