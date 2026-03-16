"""
Layer 4: 情节元素提取器
提取情节元素并送入隔离区(quarantine_zone)以保护原创性
"""
import re
import hashlib
from typing import Dict, Any, List


class PlotExtractor:
    """情节元素提取器"""

    # 情节类型关键词模式
    PLOT_PATTERNS = {
        "conflict": [
            r'[他她它][^。]*(?:对抗|反抗|抵抗|战斗|决斗)',
            r'[^。]*(?:矛盾|冲突|争执|对峙|对立)',
            r'[^。]*(?:敌人|仇人|对手|反派)',
        ],
        "turning_point": [
            r'[^。]*(?:忽然|突然|没想到|出乎意料|意想不到)',
            r'[^。]*(?:真相|秘密|发现|揭露|暴露)',
            r'[^。]*(?:转折|变化|改变|逆转)',
        ],
        "character_growth": [
            r'[^。]*(?:明白了|理解了|懂得了|领悟了|觉悟)',
            r'[^。]*(?:成长|蜕变|进化|突破|醒悟)',
            r'[^。]*(?:不再是|已经不是|变成了|成为了)',
        ],
        "relationship": [
            r'[^。]*(?:信任|背叛|友谊|爱情|仇恨)',
            r'[^。]*(?:结盟|分裂|重逢|离别|承诺)',
        ],
        "world_event": [
            r'[^。]*(?:战争|灾难|瘟疫|天灾|政变|革命)',
            r'[^。]*(?:王国|帝国|势力|门派|宗门)',
        ],
        "power_system": [
            r'[^。]*(?:修炼|突破|晋级|升级|进阶)',
            r'[^。]*(?:功法|法术|技能|心法|秘术)',
            r'[^。]*(?:境界|等级|阶段|层次)',
        ],
        "mystery": [
            r'[^。]*(?:谜团|疑问|线索|真相|悬念)',
            r'[^。]*(?:古老的|远古|上古|传说中)',
        ],
    }

    # 情节功能分类
    PLOT_FUNCTIONS = {
        "setup": ["介绍", "背景", "开始", "出场", "登场"],
        "rising_action": ["冲突", "困难", "挑战", "危机", "威胁"],
        "climax": ["高潮", "决战", "最终", "巅峰", "极致"],
        "falling_action": ["解决", "化解", "平息", "缓和"],
        "resolution": ["结局", "结束", "尾声", "和平", "重建"],
    }

    def extract(self, text: str) -> Dict[str, Any]:
        """提取情节元素"""
        sentences = re.split(r'[。！？]+', text)
        sentences = [s.strip() for s in sentences if s.strip()]

        elements = []
        element_types = {}

        # 按照情节模式提取
        for plot_type, patterns in self.PLOT_PATTERNS.items():
            for pattern in patterns:
                matches = re.findall(pattern, text)
                for match in matches:
                    clean_match = match.strip()
                    if len(clean_match) < 5 or len(clean_match) > 200:
                        continue

                    content_hash = hashlib.sha256(clean_match.encode()).hexdigest()[:16]

                    element = {
                        "type": plot_type,
                        "content": {
                            "text": clean_match,
                            "context": self._get_context(text, clean_match),
                        },
                        "hash": content_hash,
                        "function": self._classify_function(clean_match),
                    }

                    # 去重
                    if content_hash not in element_types:
                        elements.append(element)
                        element_types[content_hash] = True

        # 提取关键场景
        key_scenes = self._extract_key_scenes(sentences)

        # 提取角色关系网络
        relationship_network = self._extract_relationships(text)

        # 提取叙事弧线
        narrative_arc = self._analyze_narrative_arc(sentences)

        return {
            "elements": elements[:100],  # 限制数量
            "key_scenes": key_scenes,
            "relationship_network": relationship_network,
            "narrative_arc": narrative_arc,
            "element_type_distribution": self._count_types(elements),
        }

    def _get_context(self, text: str, match: str, context_chars: int = 100) -> str:
        """获取匹配文本的上下文"""
        idx = text.find(match)
        if idx == -1:
            return ""
        start = max(0, idx - context_chars)
        end = min(len(text), idx + len(match) + context_chars)
        return text[start:end]

    def _classify_function(self, text: str) -> str:
        """分类情节功能"""
        for function_name, keywords in self.PLOT_FUNCTIONS.items():
            for keyword in keywords:
                if keyword in text:
                    return function_name
        return "development"

    def _extract_key_scenes(self, sentences: List[str]) -> List[Dict[str, Any]]:
        """提取关键场景"""
        scene_markers = [
            ("battle", ["战斗", "交手", "出招", "攻击", "防御"]),
            ("dialogue", ["说道", "问道", "答道", "喊道", "低声"]),
            ("description", ["风景", "环境", "城市", "建筑", "大厅"]),
            ("emotional", ["哭", "笑", "怒", "泪", "叹"]),
            ("revelation", ["发现", "真相", "原来", "竟然", "居然"]),
        ]

        scenes = []
        for scene_type, keywords in scene_markers:
            count = 0
            for sent in sentences:
                if any(k in sent for k in keywords):
                    count += 1
                    if count <= 3:  # 每种类型最多3个样本
                        scenes.append({
                            "type": scene_type,
                            "sample": sent[:100],
                        })
        return scenes

    def _extract_relationships(self, text: str) -> List[Dict[str, str]]:
        """提取角色关系"""
        relationship_patterns = [
            (r'(\w{2,4})(?:和|与|跟)(\w{2,4})(?:是|的)(?:朋友|敌人|师徒|兄弟|姐妹|恋人|夫妻)', "explicit"),
            (r'(\w{2,4})(?:爱着|恨着|依赖着|追随着|保护着)(\w{2,4})', "emotional"),
        ]

        relationships = []
        for pattern, rel_type in relationship_patterns:
            matches = re.findall(pattern, text)
            for match in matches:
                if len(match) == 2:
                    relationships.append({
                        "source": match[0],
                        "target": match[1],
                        "type": rel_type,
                    })

        return relationships[:20]

    def _analyze_narrative_arc(self, sentences: List[str]) -> Dict[str, Any]:
        """分析叙事弧线"""
        if len(sentences) < 5:
            return {"arc_type": "insufficient_data"}

        # 将文本分为5个阶段分析情节强度
        segment_size = max(len(sentences) // 5, 1)
        segments = []
        intensity_keywords = ["冲突", "战斗", "危险", "紧张", "决战", "高潮", "关键", "致命"]

        for i in range(5):
            start = i * segment_size
            end = min((i + 1) * segment_size, len(sentences))
            segment_text = "".join(sentences[start:end])
            intensity = sum(segment_text.count(k) for k in intensity_keywords)
            segments.append({
                "phase": i + 1,
                "intensity": intensity,
            })

        # 判断弧线类型
        intensities = [s["intensity"] for s in segments]
        peak_idx = intensities.index(max(intensities))

        if peak_idx <= 1:
            arc_type = "front_loaded"
        elif peak_idx >= 3:
            arc_type = "back_loaded"
        elif peak_idx == 2:
            arc_type = "classic_arc"
        else:
            arc_type = "rising_action"

        return {
            "arc_type": arc_type,
            "segments": segments,
            "peak_position": peak_idx + 1,
        }

    def _count_types(self, elements: List[Dict]) -> Dict[str, int]:
        """统计元素类型分布"""
        counter = {}
        for elem in elements:
            t = elem.get("type", "unknown")
            counter[t] = counter.get(t, 0) + 1
        return counter
