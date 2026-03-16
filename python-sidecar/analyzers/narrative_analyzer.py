"""
Layer 2: 叙事结构分析器
分析POV类型、时间线模式、场景节奏
"""
import re
from typing import Dict, Any, List
from collections import Counter


class NarrativeAnalyzer:
    """叙事结构分析器"""

    # POV关键词
    FIRST_PERSON_MARKERS = ["我", "我的", "我们", "我想", "我看", "我说", "我觉得"]
    SECOND_PERSON_MARKERS = ["你", "你的", "你们"]
    THIRD_PERSON_MARKERS = ["他", "她", "它", "他们", "她们", "他的", "她的"]

    # 时间标记词
    TIME_MARKERS = [
        "之前", "以前", "过去", "从前", "曾经", "那时", "当时",  # 过去
        "现在", "此刻", "此时", "眼下", "当下",  # 现在
        "之后", "以后", "将来", "未来", "接下来",  # 未来
        "忽然", "突然", "骤然", "霎时", "刹那", "瞬间",  # 瞬间
        "第二天", "次日", "翌日", "三天后", "一个月后",  # 时间跳跃
    ]

    # 场景转换标记
    SCENE_MARKERS = [
        "另一边", "与此同时", "此时此刻", "在另一个",
        "话说", "且说", "再说", "却说",
    ]

    def analyze(self, text: str) -> Dict[str, Any]:
        """执行叙事结构分析"""
        paragraphs = [p.strip() for p in text.split("\n") if p.strip()]
        sentences = re.split(r'[。！？]+', text)
        sentences = [s.strip() for s in sentences if s.strip()]

        return {
            "pov": self._analyze_pov(text, sentences),
            "timeline": self._analyze_timeline(text, sentences),
            "scene_structure": self._analyze_scenes(paragraphs),
            "pacing": self._analyze_pacing(paragraphs, sentences),
            "dialogue_ratio": self._analyze_dialogue(text),
            "narrative_mode": self._analyze_narrative_mode(text, sentences),
        }

    def _analyze_pov(self, text: str, sentences: List[str]) -> Dict[str, Any]:
        """分析叙事视角(POV)"""
        first_count = sum(text.count(m) for m in self.FIRST_PERSON_MARKERS)
        second_count = sum(text.count(m) for m in self.SECOND_PERSON_MARKERS)
        third_count = sum(text.count(m) for m in self.THIRD_PERSON_MARKERS)
        total = first_count + second_count + third_count

        if total == 0:
            pov_type = "objective"
        elif first_count > third_count and first_count > second_count:
            pov_type = "first_person"
        elif second_count > first_count and second_count > third_count:
            pov_type = "second_person"
        else:
            pov_type = "third_person"

        # 判断限制性/全知
        pov_subtype = pov_type
        if pov_type == "third_person":
            # 全知视角通常有更多不同人物的内心描写
            inner_markers = ["想到", "心想", "暗想", "心中", "知道", "明白", "感觉", "察觉"]
            inner_count = sum(text.count(m) for m in inner_markers)
            # 如果内心描写密度高,且切换多个角色,可能是全知视角
            if inner_count / max(len(sentences), 1) > 0.1:
                pov_subtype = "third_person_omniscient"
            else:
                pov_subtype = "third_person_limited"

        return {
            "type": pov_type,
            "subtype": pov_subtype,
            "confidence": round(max(first_count, second_count, third_count) / max(total, 1), 2),
            "counts": {
                "first_person": first_count,
                "second_person": second_count,
                "third_person": third_count,
            },
        }

    def _analyze_timeline(self, text: str, sentences: List[str]) -> Dict[str, Any]:
        """分析时间线模式"""
        time_markers_found = []
        for marker in self.TIME_MARKERS:
            positions = [m.start() for m in re.finditer(re.escape(marker), text)]
            for pos in positions:
                time_markers_found.append({
                    "marker": marker,
                    "position": pos,
                    "relative_position": round(pos / max(len(text), 1), 4),
                })

        # 检测闪回
        flashback_markers = ["记得那是", "想起了", "回忆起", "脑海中浮现", "往事"]
        flashback_count = sum(text.count(m) for m in flashback_markers)

        # 检测时间跳跃
        jump_markers = ["三天后", "一个月后", "半年后", "一年后", "几天后", "数日后", "翌日", "次日"]
        jump_count = sum(text.count(m) for m in jump_markers)

        # 判断时间线类型
        if flashback_count > 3:
            timeline_type = "non_linear"
        elif jump_count > 5:
            timeline_type = "episodic"
        else:
            timeline_type = "linear"

        return {
            "type": timeline_type,
            "time_markers_count": len(time_markers_found),
            "flashback_count": flashback_count,
            "time_jump_count": jump_count,
            "markers_sample": time_markers_found[:20],
        }

    def _analyze_scenes(self, paragraphs: List[str]) -> Dict[str, Any]:
        """分析场景结构"""
        scene_breaks = 0
        scenes = []
        current_scene_length = 0

        for i, para in enumerate(paragraphs):
            current_scene_length += len(para)

            # 检测场景转换
            is_scene_break = False
            for marker in self.SCENE_MARKERS:
                if marker in para:
                    is_scene_break = True
                    break

            # 空行也可能标示场景转换 (多个连续换行)
            if len(para) < 5 and re.match(r'^[*\-=·]+$', para):
                is_scene_break = True

            if is_scene_break:
                if current_scene_length > 0:
                    scenes.append(current_scene_length)
                current_scene_length = 0
                scene_breaks += 1

        if current_scene_length > 0:
            scenes.append(current_scene_length)

        return {
            "total_scenes": len(scenes) if scenes else 1,
            "scene_breaks": scene_breaks,
            "avg_scene_length": round(sum(scenes) / max(len(scenes), 1), 0) if scenes else len("".join(paragraphs)),
            "scene_lengths": scenes[:20],
        }

    def _analyze_pacing(self, paragraphs: List[str], sentences: List[str]) -> Dict[str, Any]:
        """分析叙事节奏"""
        # 段落长度分析
        para_lengths = [len(p) for p in paragraphs]
        if not para_lengths:
            return {"overall": "neutral"}

        avg_para = sum(para_lengths) / len(para_lengths)

        # 区分快节奏(短段落、短句多)和慢节奏(长段、长句多)
        short_para_ratio = sum(1 for l in para_lengths if l < 50) / len(para_lengths)
        long_para_ratio = sum(1 for l in para_lengths if l > 200) / len(para_lengths)

        sent_lengths = [len(s) for s in sentences]
        avg_sent = sum(sent_lengths) / max(len(sent_lengths), 1)

        if short_para_ratio > 0.6 and avg_sent < 15:
            pacing = "fast"
        elif long_para_ratio > 0.4 and avg_sent > 25:
            pacing = "slow"
        else:
            pacing = "moderate"

        # 节奏变化分析 - 将文本分为4个象限
        quarters = [para_lengths[i * len(para_lengths) // 4:(i + 1) * len(para_lengths) // 4]
                    for i in range(4)]
        quarter_avgs = [sum(q) / max(len(q), 1) for q in quarters]

        return {
            "overall": pacing,
            "avg_paragraph_length": round(avg_para, 0),
            "avg_sentence_length": round(avg_sent, 2),
            "short_paragraph_ratio": round(short_para_ratio, 4),
            "long_paragraph_ratio": round(long_para_ratio, 4),
            "quarter_pacing": [round(a, 0) for a in quarter_avgs],
        }

    def _analyze_dialogue(self, text: str) -> Dict[str, Any]:
        """分析对话占比"""
        # 匹配中文引号对话
        dialogues = re.findall(r'["""]([^"""]*)["""]', text)
        dialogue_chars = sum(len(d) for d in dialogues)
        total_chars = len(text)

        return {
            "dialogue_count": len(dialogues),
            "dialogue_char_ratio": round(dialogue_chars / max(total_chars, 1), 4),
            "avg_dialogue_length": round(dialogue_chars / max(len(dialogues), 1), 1),
            "density": (
                "dialogue_heavy" if dialogue_chars / max(total_chars, 1) > 0.4
                else "balanced" if dialogue_chars / max(total_chars, 1) > 0.15
                else "narrative_heavy"
            ),
        }

    def _analyze_narrative_mode(self, text: str, sentences: List[str]) -> Dict[str, Any]:
        """分析叙事模式 (叙述/描写/议论/抒情)"""
        # 描写标记词
        desc_markers = ["金色的", "美丽的", "高大的", "阴暗的", "明亮的", "温暖的",
                        "寒冷的", "柔和的", "刺眼的", "沉闷的"]
        # 议论标记词
        argue_markers = ["因此", "所以", "然而", "但是", "不过", "其实", "事实上",
                         "显然", "毕竟", "总之"]
        # 抒情标记词
        lyric_markers = ["啊", "哦", "呀", "唉", "多么", "何等", "怎能"]

        desc_count = sum(text.count(m) for m in desc_markers)
        argue_count = sum(text.count(m) for m in argue_markers)
        lyric_count = sum(text.count(m) for m in lyric_markers)
        total = max(desc_count + argue_count + lyric_count, 1)

        return {
            "description_ratio": round(desc_count / total, 4),
            "argumentation_ratio": round(argue_count / total, 4),
            "lyricism_ratio": round(lyric_count / total, 4),
            "dominant_mode": (
                "descriptive" if desc_count > argue_count and desc_count > lyric_count
                else "argumentative" if argue_count > lyric_count
                else "lyrical" if lyric_count > 0
                else "narrative"
            ),
        }
