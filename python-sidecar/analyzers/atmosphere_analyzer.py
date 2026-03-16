"""
Layer 3: 氛围萃取分析器
提取情绪基调、感官描写频率、环境意象库
"""
import re
from typing import Dict, Any, List
from collections import Counter

try:
    from snownlp import SnowNLP
    HAS_SNOWNLP = True
except ImportError:
    HAS_SNOWNLP = False


class AtmosphereAnalyzer:
    """氛围萃取分析器"""

    # 感官描写关键词
    VISUAL_WORDS = [
        "看到", "望见", "瞥见", "注视", "凝望", "俯瞰", "仰望",
        "金色", "银色", "血红", "漆黑", "苍白", "翠绿", "湛蓝",
        "光芒", "阴影", "黑暗", "明亮", "闪烁", "朦胧", "模糊",
        "颜色", "光线", "色彩", "轮廓", "形状",
    ]

    AUDITORY_WORDS = [
        "听到", "听见", "声音", "响声", "回声", "轰鸣", "低语",
        "呢喃", "咆哮", "呼啸", "叮当", "噼啪", "嘶嘶",
        "寂静", "安静", "喧嚣", "嘈杂", "沉默", "静谧",
        "歌声", "风声", "雨声", "脚步声", "心跳声",
    ]

    OLFACTORY_WORDS = [
        "闻到", "嗅到", "气味", "芳香", "恶臭", "清香",
        "花香", "血腥", "焦臭", "霉味", "泥土味",
        "馨香", "腐臭", "酒气", "烟味",
    ]

    TACTILE_WORDS = [
        "触摸", "抚摸", "感觉", "触感", "冰冷", "温热",
        "滚烫", "粗糙", "光滑", "柔软", "坚硬",
        "刺痛", "麻木", "颤抖", "瑟瑟", "发抖",
        "风吹", "潮湿", "干燥", "黏腻",
    ]

    GUSTATORY_WORDS = [
        "尝到", "品尝", "味道", "甜", "苦", "酸", "辣", "咸",
        "鲜", "涩", "腥", "香甜", "苦涩",
    ]

    # 情绪关键词
    EMOTION_POSITIVE = [
        "快乐", "幸福", "喜悦", "欢笑", "温馨", "希望", "感动",
        "满足", "轻松", "愉快", "兴奋", "期待", "热情", "甜蜜",
    ]

    EMOTION_NEGATIVE = [
        "悲伤", "痛苦", "绝望", "恐惧", "愤怒", "焦虑", "孤独",
        "忧愁", "惊恐", "沮丧", "失落", "压抑", "阴郁", "凄凉",
    ]

    EMOTION_TENSE = [
        "紧张", "危险", "威胁", "逼近", "追赶", "慌张", "急迫",
        "迫切", "战栗", "惶恐", "害怕", "震惊", "冲突", "对峙",
    ]

    # 环境意象
    NATURE_IMAGES = {
        "天气": ["阳光", "雨", "雪", "风", "雷", "雾", "云", "霜", "露"],
        "地形": ["山", "河", "海", "湖", "森林", "沙漠", "平原", "峡谷"],
        "植物": ["树", "花", "草", "叶", "根", "藤", "竹", "松", "柳"],
        "动物": ["鸟", "鱼", "蝶", "蛇", "狼", "鹰", "马", "龙", "凤"],
        "天体": ["月", "星", "日", "太阳", "月亮", "星辰", "银河"],
        "季节": ["春", "夏", "秋", "冬"],
    }

    def analyze(self, text: str) -> Dict[str, Any]:
        """执行氛围萃取分析"""
        sentences = re.split(r'[。！？]+', text)
        sentences = [s.strip() for s in sentences if s.strip()]

        return {
            "emotion_profile": self._analyze_emotions(text, sentences),
            "sensory_profile": self._analyze_sensory(text),
            "imagery_library": self._extract_imagery(text),
            "atmosphere_keywords": self._extract_atmosphere_keywords(text),
            "tone": self._determine_tone(text, sentences),
            "color_palette": self._extract_colors(text),
        }

    def _analyze_emotions(self, text: str, sentences: List[str]) -> Dict[str, Any]:
        """分析情绪剖面"""
        positive_count = sum(text.count(w) for w in self.EMOTION_POSITIVE)
        negative_count = sum(text.count(w) for w in self.EMOTION_NEGATIVE)
        tense_count = sum(text.count(w) for w in self.EMOTION_TENSE)
        total = max(positive_count + negative_count + tense_count, 1)

        # 使用SnowNLP进行情感分析
        sentiment_scores = []
        if HAS_SNOWNLP:
            for sent in sentences[:100]:  # 限制处理量
                try:
                    s = SnowNLP(sent)
                    sentiment_scores.append(s.sentiments)
                except Exception:
                    pass

        avg_sentiment = (sum(sentiment_scores) / len(sentiment_scores)) if sentiment_scores else 0.5

        # 情绪流变化 - 将文本分为10段
        emotion_flow = []
        segment_size = max(len(sentences) // 10, 1)
        for i in range(0, len(sentences), segment_size):
            segment = "".join(sentences[i:i + segment_size])
            pos = sum(segment.count(w) for w in self.EMOTION_POSITIVE)
            neg = sum(segment.count(w) for w in self.EMOTION_NEGATIVE)
            tens = sum(segment.count(w) for w in self.EMOTION_TENSE)
            seg_total = max(pos + neg + tens, 1)
            emotion_flow.append({
                "positive": round(pos / seg_total, 2),
                "negative": round(neg / seg_total, 2),
                "tense": round(tens / seg_total, 2),
            })

        return {
            "positive_ratio": round(positive_count / total, 4),
            "negative_ratio": round(negative_count / total, 4),
            "tense_ratio": round(tense_count / total, 4),
            "avg_sentiment": round(avg_sentiment, 4),
            "dominant_emotion": (
                "positive" if positive_count > negative_count and positive_count > tense_count
                else "tense" if tense_count > negative_count
                else "negative" if negative_count > 0
                else "neutral"
            ),
            "emotion_flow": emotion_flow[:10],
        }

    def _analyze_sensory(self, text: str) -> Dict[str, Any]:
        """分析感官描写频率"""
        visual = sum(text.count(w) for w in self.VISUAL_WORDS)
        auditory = sum(text.count(w) for w in self.AUDITORY_WORDS)
        olfactory = sum(text.count(w) for w in self.OLFACTORY_WORDS)
        tactile = sum(text.count(w) for w in self.TACTILE_WORDS)
        gustatory = sum(text.count(w) for w in self.GUSTATORY_WORDS)

        total = max(visual + auditory + olfactory + tactile + gustatory, 1)
        total_chars = max(len(text), 1)

        return {
            "visual": {
                "count": visual,
                "ratio": round(visual / total, 4),
                "density": round(visual / total_chars * 1000, 2),
            },
            "auditory": {
                "count": auditory,
                "ratio": round(auditory / total, 4),
                "density": round(auditory / total_chars * 1000, 2),
            },
            "olfactory": {
                "count": olfactory,
                "ratio": round(olfactory / total, 4),
                "density": round(olfactory / total_chars * 1000, 2),
            },
            "tactile": {
                "count": tactile,
                "ratio": round(tactile / total, 4),
                "density": round(tactile / total_chars * 1000, 2),
            },
            "gustatory": {
                "count": gustatory,
                "ratio": round(gustatory / total, 4),
                "density": round(gustatory / total_chars * 1000, 2),
            },
            "dominant_sense": max(
                [("visual", visual), ("auditory", auditory),
                 ("olfactory", olfactory), ("tactile", tactile),
                 ("gustatory", gustatory)],
                key=lambda x: x[1]
            )[0],
            "total_sensory_density": round((visual + auditory + olfactory + tactile + gustatory) / total_chars * 1000, 2),
        }

    def _extract_imagery(self, text: str) -> Dict[str, Any]:
        """提取环境意象库"""
        imagery = {}
        for category, words in self.NATURE_IMAGES.items():
            found = {}
            for word in words:
                count = text.count(word)
                if count > 0:
                    found[word] = count
            if found:
                imagery[category] = found

        return imagery

    def _extract_atmosphere_keywords(self, text: str) -> List[str]:
        """提取氛围关键词"""
        atmosphere_words = [
            "凄凉", "温馨", "诡异", "空灵", "肃杀", "祥和",
            "压抑", "明快", "沉重", "清新", "荒凉", "繁华",
            "神秘", "庄严", "清幽", "热烈", "浪漫", "忧伤",
            "恢弘", "苍茫", "磅礴", "宁静", "喧嚣", "孤寂",
        ]
        found = []
        for word in atmosphere_words:
            if word in text:
                found.append(word)
        return found

    def _determine_tone(self, text: str, sentences: List[str]) -> Dict[str, Any]:
        """确定整体基调"""
        # 基调分类
        tone_scores = {
            "dark": sum(text.count(w) for w in ["黑暗", "死亡", "鲜血", "恐惧", "诅咒", "阴暗", "腐烂"]),
            "light": sum(text.count(w) for w in ["阳光", "希望", "温暖", "光明", "笑容", "美好", "幸福"]),
            "mysterious": sum(text.count(w) for w in ["神秘", "奇怪", "诡异", "不可思议", "秘密", "未知"]),
            "epic": sum(text.count(w) for w in ["战争", "帝国", "力量", "命运", "英雄", "传说", "荣耀"]),
            "romantic": sum(text.count(w) for w in ["爱", "心动", "温柔", "拥抱", "亲吻", "思念", "深情"]),
            "melancholy": sum(text.count(w) for w in ["忧伤", "惆怅", "落寞", "感伤", "怀念", "叹息"]),
        }

        dominant_tone = max(tone_scores, key=tone_scores.get)
        total_score = max(sum(tone_scores.values()), 1)

        return {
            "dominant": dominant_tone,
            "scores": {k: round(v / total_score, 4) for k, v in tone_scores.items()},
            "intensity": round(total_score / max(len(sentences), 1), 4),
        }

    def _extract_colors(self, text: str) -> Dict[str, int]:
        """提取色彩调色板"""
        colors = {
            "红": ["红", "赤", "绯", "朱", "丹", "血红", "殷红", "猩红"],
            "黄": ["黄", "金", "琥珀", "橙", "落日"],
            "蓝": ["蓝", "碧", "青", "湛蓝", "蔚蓝"],
            "绿": ["绿", "翠", "碧绿", "墨绿", "苍翠"],
            "白": ["白", "银", "苍白", "雪白", "皎洁"],
            "黑": ["黑", "墨", "漆黑", "乌黑", "幽暗"],
            "紫": ["紫", "紫色", "紫罗兰"],
            "灰": ["灰", "灰色", "银灰", "铅灰"],
        }

        palette = {}
        for color_name, keywords in colors.items():
            count = sum(text.count(k) for k in keywords)
            if count > 0:
                palette[color_name] = count

        return palette
