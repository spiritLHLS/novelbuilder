"""
困惑度(Perplexity)和突发度(Burstiness)估计器
用于检测AI味 - AI生成文本通常具有低困惑度和低突发度
"""
import re
import math
from typing import Dict, Any, List
from collections import Counter

import jieba


class PerplexityBurstinessEstimator:
    """困惑度/突发度估计器"""

    def estimate(self, text: str) -> Dict[str, Any]:
        """估计文本的困惑度和突发度指标"""
        sentences = re.split(r'[。！？]+', text)
        sentences = [s.strip() for s in sentences if s.strip()]

        if not sentences:
            return {
                "perplexity_estimate": 0,
                "burstiness": 0,
                "ai_probability": 0.5,
                "details": {},
            }

        # 1. 基于n-gram的简易困惑度估计
        perplexity = self._estimate_perplexity(text)

        # 2. 突发度计算
        burstiness = self._calculate_burstiness(sentences)

        # 3. 词汇多样性指标
        vocab_metrics = self._vocabulary_diversity(text)

        # 4. 句式多样性
        syntax_diversity = self._syntax_diversity(sentences)

        # 5. 综合AI检测评分
        ai_probability = self._estimate_ai_probability(
            perplexity, burstiness, vocab_metrics, syntax_diversity
        )

        return {
            "perplexity_estimate": round(perplexity, 2),
            "burstiness": round(burstiness, 4),
            "ai_probability": round(ai_probability, 4),
            "verdict": (
                "likely_ai" if ai_probability > 0.7
                else "uncertain" if ai_probability > 0.4
                else "likely_human"
            ),
            "details": {
                "vocabulary": vocab_metrics,
                "syntax_diversity": syntax_diversity,
            },
        }

    def _estimate_perplexity(self, text: str) -> float:
        """
        使用字符级n-gram估计困惑度
        低困惑度 → 文本更可预测 → 更可能是AI生成
        """
        n = 3  # trigram
        chars = list(text.replace(" ", "").replace("\n", ""))

        if len(chars) < n + 1:
            return 50.0

        # 构建n-gram频率表
        ngram_counts = Counter()
        prefix_counts = Counter()

        for i in range(len(chars) - n):
            ngram = tuple(chars[i:i + n + 1])
            prefix = tuple(chars[i:i + n])
            ngram_counts[ngram] += 1
            prefix_counts[prefix] += 1

        # 计算平均对数概率
        total_log_prob = 0
        count = 0

        for i in range(len(chars) - n):
            ngram = tuple(chars[i:i + n + 1])
            prefix = tuple(chars[i:i + n])

            prob = ngram_counts[ngram] / max(prefix_counts[prefix], 1)
            if prob > 0:
                total_log_prob += math.log2(prob)
                count += 1

        if count == 0:
            return 50.0

        avg_log_prob = total_log_prob / count
        perplexity = 2 ** (-avg_log_prob)

        return min(perplexity, 1000.0)

    def _calculate_burstiness(self, sentences: List[str]) -> float:
        """
        计算突发度
        突发度 = 句长标准差 / 句长均值
        真人写作的突发度通常 > 0.5，AI生成通常 < 0.3
        """
        if len(sentences) < 2:
            return 0.0

        lengths = [len(s) for s in sentences]
        mean = sum(lengths) / len(lengths)
        variance = sum((l - mean) ** 2 for l in lengths) / len(lengths)
        std = math.sqrt(variance)

        if mean == 0:
            return 0.0

        return std / mean

    def _vocabulary_diversity(self, text: str) -> Dict[str, float]:
        """计算词汇多样性指标"""
        words = list(jieba.cut(text))
        content_words = [w for w in words if len(w) >= 2 and not re.match(r'^[，。！？；：、""''（）【】]$', w)]

        if not content_words:
            return {"ttr": 0, "hapax_ratio": 0, "yules_k": 0}

        types = set(content_words)
        tokens = len(content_words)
        ttr = len(types) / max(tokens, 1)

        # Hapax legomena ratio (只出现一次的词比例)
        freq = Counter(content_words)
        hapax = sum(1 for v in freq.values() if v == 1)
        hapax_ratio = hapax / max(len(types), 1)

        # Yule's K
        freq_spectrum = Counter(freq.values())
        m1 = tokens
        m2 = sum(i * i * freq_spectrum[i] for i in freq_spectrum)
        yules_k = 10000 * (m2 - m1) / max(m1 * m1, 1)

        return {
            "ttr": round(ttr, 4),
            "hapax_ratio": round(hapax_ratio, 4),
            "yules_k": round(yules_k, 2),
        }

    def _syntax_diversity(self, sentences: List[str]) -> Dict[str, float]:
        """计算句式多样性"""
        if not sentences:
            return {"opening_diversity": 0, "ending_diversity": 0}

        # 句首多样性 - 统计不同的句首字/词
        openings = []
        for sent in sentences:
            if len(sent) >= 2:
                openings.append(sent[:2])

        opening_types = len(set(openings))
        opening_diversity = opening_types / max(len(openings), 1)

        # 句末模式多样性
        endings = []
        for sent in sentences:
            if len(sent) >= 2:
                last_char = sent[-1]
                if last_char in "了过着的":
                    endings.append(last_char)
                elif sent.endswith("呢"):
                    endings.append("呢")
                else:
                    endings.append("other")

        ending_types = len(set(endings))
        ending_diversity = ending_types / max(len(endings), 1)

        # 句长变化模式分析
        lengths = [len(s) for s in sentences]
        length_changes = []
        for i in range(len(lengths) - 1):
            if lengths[i + 1] > lengths[i] * 1.5:
                length_changes.append("expand")
            elif lengths[i + 1] < lengths[i] * 0.6:
                length_changes.append("contract")
            else:
                length_changes.append("stable")

        pattern_diversity = len(set(length_changes)) / max(len(length_changes), 1)

        return {
            "opening_diversity": round(opening_diversity, 4),
            "ending_diversity": round(ending_diversity, 4),
            "pattern_diversity": round(pattern_diversity, 4),
        }

    def _estimate_ai_probability(
        self,
        perplexity: float,
        burstiness: float,
        vocab_metrics: Dict[str, float],
        syntax_diversity: Dict[str, float],
    ) -> float:
        """
        综合评估AI生成概率
        多维度指标加权计算
        """
        score = 0.0

        # 困惑度维度 (AI文本通常困惑度较低)
        if perplexity < 10:
            score += 0.3
        elif perplexity < 30:
            score += 0.15
        else:
            score -= 0.1

        # 突发度维度 (AI文本突发度低)
        if burstiness < 0.2:
            score += 0.3
        elif burstiness < 0.4:
            score += 0.1
        elif burstiness > 0.6:
            score -= 0.15

        # 词汇多样性维度
        ttr = vocab_metrics.get("ttr", 0.5)
        if ttr > 0.7:
            score -= 0.1  # 高多样性倾向于人类
        elif ttr < 0.4:
            score += 0.1

        # 句式多样性维度
        opening_div = syntax_diversity.get("opening_diversity", 0.5)
        if opening_div < 0.3:
            score += 0.15  # 句首单调倾向于AI
        elif opening_div > 0.6:
            score -= 0.1

        # 归一化到0-1
        probability = max(0.0, min(1.0, 0.5 + score))

        return probability
