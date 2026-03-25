"""
9步人性化管线
Step 1: 逻辑指纹打断 (Logic Fingerprint Breaking)
Step 2: 主语省略 (Subject Omission)
Step 3: 对话压缩 (Dialogue Compression)
Step 4: 情感替换 (Emotion Replacement)
Step 5: 感官注入 (Sensory Injection)
Step 6: 自由间接引语 (Free Indirect Discourse)
Step 7: 突发度优化 (Burstiness Optimization)
Step 8: AI结尾清除 (AI Ending Strip)
Step 9: 叙事顺序检查 (Narrative Sequence Check)
"""
import re
import random
import math
from typing import Dict, Any, Optional, List
from collections import Counter

import jieba


class HumanizationPipeline:
    """9步人性化管线"""

    def process(
        self,
        text: str,
        style_fingerprint: Optional[Dict] = None,
        intensity: float = 0.7,
    ) -> Dict[str, Any]:
        """
        执行完整的8步人性化管线
        intensity: 人性化强度 0.0-1.0
        """
        steps_applied = []
        current_text = text

        # Step 1: 逻辑指纹打断
        current_text, step1_info = self._step1_break_logic_fingerprint(current_text, intensity)
        steps_applied.append({"step": 1, "name": "逻辑指纹打断", **step1_info})

        # Step 2: 主语省略
        current_text, step2_info = self._step2_subject_omission(current_text, intensity)
        steps_applied.append({"step": 2, "name": "主语省略", **step2_info})

        # Step 3: 对话压缩
        current_text, step3_info = self._step3_dialogue_compression(current_text, intensity)
        steps_applied.append({"step": 3, "name": "对话压缩", **step3_info})

        # Step 4: 情感替换
        current_text, step4_info = self._step4_emotion_replacement(current_text, intensity)
        steps_applied.append({"step": 4, "name": "情感替换", **step4_info})

        # Step 5: 感官注入
        current_text, step5_info = self._step5_sensory_injection(current_text, intensity, style_fingerprint)
        steps_applied.append({"step": 5, "name": "感官注入", **step5_info})

        # Step 6: 自由间接引语
        current_text, step6_info = self._step6_free_indirect_discourse(current_text, intensity)
        steps_applied.append({"step": 6, "name": "自由间接引语", **step6_info})

        # Step 7: 突发度优化
        current_text, step7_info = self._step7_burstiness_optimization(current_text, intensity)
        steps_applied.append({"step": 7, "name": "突发度优化", **step7_info})

        # Step 8: AI结尾清除
        current_text, step8_info = self._step8_strip_ai_ending(current_text)
        steps_applied.append({"step": 8, "name": "AI结尾清除", **step8_info})

        # Step 9: 叙事顺序检查
        current_text, step9_info = self._step9_narrative_sequence_check(current_text)
        steps_applied.append({"step": 9, "name": "叙事顺序检查", **step9_info})

        return {
            "original": text,
            "humanized": current_text,
            "steps": steps_applied,
            "change_ratio": round(1 - _similarity(text, current_text), 4),
        }

    def _step1_break_logic_fingerprint(self, text: str, intensity: float) -> tuple:
        """
        Step 1: 逻辑指纹打断
        AI生成文本常有过于规整的逻辑连接词使用模式，需要打乱。
        - 替换机械化连接词
        - 打断"首先...其次...最后..."等列举模式
        - 减少"然而"、"因此"等过度使用的转折词
        """
        changes = 0

        # 过度逻辑连接词替换表
        replacements = {
            "首先，": ["", "先是", "起初"],
            "其次，": ["", "再者", "接着"],
            "最后，": ["末了", "到头来", ""],
            "总之，": ["", "说到底", "归根结底"],
            "因此，": ["所以", "这样一来", "于是"],
            "然而，": ["可", "谁知", "偏偏"],
            "此外，": ["另外", "还有", ""],
            "同时，": ["", "一面", "与此同时"],
            "值得注意的是，": ["", "有意思的是"],
            "不仅如此，": ["不止这些", ""],
            "换句话说，": ["也就是", "说白了"],
            "事实上，": ["其实", "实际上", ""],
            "毫无疑问，": ["", "自然"],
            "显而易见，": ["明摆着", ""],
            "综上所述，": ["", "这么看来"],
        }

        # AI高频词/短语替换（网文反AI痕迹）
        ai_phrase_replacements = {
            "不禁": ["忍不住", ""],
            "缓缓": ["慢慢", ""],
            "淡淡": [""],
            "微微": ["略", "稍稍", ""],
            "默默": ["悄悄", ""],
            "嘴角勾起一抹弧度": ["嘴角一翘", "撇了撇嘴笑了", "嘴角弯了弯"],
            "一股暖流涌上心头": ["心里热乎乎的", "胸口暖了一下"],
            "一股寒意涌上心头": ["打了个寒颤", "后脊梁一凉"],
            "眼中闪过一丝": ["眼神一动", "目光微变"],
            "心中涌起一股": ["心头一紧", "胸口一闷"],
            "如同": ["像", "好比"],
            "宛如": ["像是", "好似"],
            "仿佛": ["好像", ""],
            "不由自主": ["不自觉", "鬼使神差"],
            "竟然": ["居然", "倒是"],
        }
        for original, alternatives in ai_phrase_replacements.items():
            count = text.count(original)
            if count > 0:
                # 每个AI高频词最多保留1次出现
                while text.count(original) > 1 and random.random() < 0.9:
                    replacement = random.choice(alternatives)
                    text = text.replace(original, replacement, 1)
                    changes += 1
                # 唯一一次出现也有概率替换
                if original in text and random.random() < intensity * 0.5:
                    replacement = random.choice(alternatives)
                    text = text.replace(original, replacement, 1)
                    changes += 1

        for original, alternatives in replacements.items():
            if original in text and random.random() < intensity:
                replacement = random.choice(alternatives)
                text = text.replace(original, replacement, 1)
                changes += 1

        # 打断重复句式模式 (如连续三个"他XXX了")
        pattern = r'(他|她)\w{1,4}了[。，]'
        matches = list(re.finditer(pattern, text))
        if len(matches) >= 3:
            # 在第二个重复处进行句式变换
            for i in range(1, len(matches) - 1):
                if random.random() < intensity * 0.5:
                    match = matches[i]
                    original_segment = match.group()
                    # 尝试变换句式
                    modified = original_segment.replace("了", "着")
                    text = text[:match.start()] + modified + text[match.end():]
                    changes += 1
                    break  # 只改一处防止索引偏移

        return text, {"changes": changes}

    def _step2_subject_omission(self, text: str, intensity: float) -> tuple:
        """
        Step 2: 主语省略
        中文文学作品常省略主语，AI生成文本则倾向于每句都写主语。
        在连续描述同一主语行为时随机省略部分主语。
        """
        changes = 0
        sentences = text.split("。")
        result = []
        prev_subject = ""

        for i, sent in enumerate(sentences):
            sent = sent.strip()
            if not sent:
                result.append(sent)
                continue

            # 检测句首主语
            subject_match = re.match(r'^(他|她|它|我|你|我们|他们|她们)([，,]?)', sent)
            if subject_match:
                current_subject = subject_match.group(1)
                if (current_subject == prev_subject
                        and i > 0
                        and random.random() < intensity * 0.6):
                    # 省略重复主语
                    sent = sent[len(subject_match.group()):]
                    changes += 1
                prev_subject = current_subject
            else:
                prev_subject = ""

            result.append(sent)

        return "。".join(result), {"changes": changes}

    def _step3_dialogue_compression(self, text: str, intensity: float) -> tuple:
        """
        Step 3: 对话压缩
        AI生成的对话偏长且说教，需要压缩使其更口语化。
        - 缩短过长对话
        - 增加语气词
        - 添加对话中断
        """
        changes = 0

        # 找到引号内的对话
        def compress_dialogue(match):
            nonlocal changes
            dialogue = match.group(1)

            if len(dialogue) < 20 or random.random() > intensity:
                return f'\u201c{dialogue}\u201d'

            changes += 1

            # 删除对话中的过度解释
            dialogue = re.sub(r'我认为|我觉得|我想说的是', '', dialogue)

            # 添加口语化语气词
            fillers = ["嗯，", "那个…", "就是说，", "你看，"]
            if random.random() < intensity * 0.3:
                dialogue = random.choice(fillers) + dialogue

            # 如果对话过长，在中间添加动作打断
            if len(dialogue) > 60:
                mid = len(dialogue) // 2
                # 找最近的逗号或句号
                break_pos = dialogue.find("，", mid - 10)
                if break_pos > 0:
                    actions = [
                        '\u201c他顿了顿，\u201d',
                        '\u201c她停了停，想了想才继续，\u201d',
                        '\u201c他摇了摇头，\u201d',
                    ]
                    action = random.choice(actions)
                    dialogue = dialogue[:break_pos + 1] + action + dialogue[break_pos + 1:]

            return f'\u201c{dialogue}\u201d'

        text = re.sub(r'"([^"]*)"', compress_dialogue, text)

        return text, {"changes": changes}

    def _step4_emotion_replacement(self, text: str, intensity: float) -> tuple:
        """
        Step 4: 情感替换
        将直接的情感描述替换为更具文学性的间接表达。
        "他感到悲伤" → 用身体反应或环境映射替代
        """
        changes = 0

        emotion_replacements = {
            "他感到悲伤": [
                "嗓子眼像堵了什么东西",
                "眼前那些字迹渐渐模糊了",
                "胸口闷闷的，像压了块石头",
            ],
            "她感到高兴": [
                "嘴角不自觉地翘了起来",
                "脚步都变得轻快了几分",
                "连呼吸都带着笑意",
            ],
            "他感到愤怒": [
                "太阳穴突突地跳",
                "拳头不知什么时候攥紧了",
                "牙齿咬得咯吱作响",
            ],
            "她感到害怕": [
                "后脊背窜过一阵凉意",
                "手心渗出冷汗",
                "心跳声在耳膜里砰砰作响",
            ],
            "他感到紧张": [
                "喉结不自觉地滚动了一下",
                "手指不自觉地绞在一起",
                "额角渗出细密的汗珠",
            ],
            "她感到失望": [
                "光从眼神里一点一点褪去",
                "肩膀微微垮了下来",
                "手慢慢从半空中放了下来",
            ],
        }

        for emotion, alternatives in emotion_replacements.items():
            if emotion in text and random.random() < intensity:
                replacement = random.choice(alternatives)
                text = text.replace(emotion, replacement, 1)
                changes += 1

        # 通用模式：将"感到X"替换为身体反应
        generic_pattern = r'(?:他|她)(?:感到|觉得|感觉)(\w{2})'
        match = re.search(generic_pattern, text)
        if match and random.random() < intensity * 0.3:
            emotion_word = match.group(1)
            generic_replacements = {
                "疲惫": "每一块骨头都在叫嚣着酸痛",
                "焦急": "不断看向窗外",
                "孤独": "房间里安静得只能听见钟摆的声音",
                "期待": "目光不由自主地投向门口的方向",
            }
            if emotion_word in generic_replacements:
                text = text[:match.start()] + generic_replacements[emotion_word] + text[match.end():]
                changes += 1

        return text, {"changes": changes}

    def _step5_sensory_injection(
        self,
        text: str,
        intensity: float,
        style_fingerprint: Optional[dict] = None,
    ) -> tuple:
        """
        Step 5: 感官注入
        在叙述段落中插入感官细节，基于风格指纹调整注入的种类权重。
        """
        changes = 0

        # 根据风格指纹确定偏好感官
        weight_visual = 0.3
        weight_auditory = 0.25
        weight_tactile = 0.2
        weight_olfactory = 0.15
        weight_gustatory = 0.1

        if style_fingerprint and "sensory_profile" in style_fingerprint:
            profile = style_fingerprint["sensory_profile"]
            if "visual" in profile:
                weight_visual = profile["visual"].get("ratio", weight_visual)
            if "auditory" in profile:
                weight_auditory = profile["auditory"].get("ratio", weight_auditory)

        sensory_insertions = {
            "visual": [
                "光线从窗棂间挤过来，切出一道一道的斜纹",
                "影子在墙上拉得老长",
                "天边燃起一抹橘红",
            ],
            "auditory": [
                "远处传来几声犬吠",
                "树叶在风中窸窸窣窣地响",
                "钟楼的钟声沉沉地荡开",
            ],
            "tactile": [
                "寒气从脚底一路往上窜",
                "衣料磨蹭在皮肤上，微微发痒",
                "手指触到冰凉的石壁",
            ],
            "olfactory": [
                "空气中飘来隐约的花香",
                "潮湿的泥土气息扑面而来",
                "炊烟的味道从远处飘来",
            ],
            "gustatory": [
                "嘴里泛起一股苦涩",
                "唾液不自觉地分泌出来",
            ],
        }

        # 在段落结束处插入感官描写
        paragraphs = text.split("\n")
        new_paragraphs = []

        for para in paragraphs:
            new_paragraphs.append(para)
            # 在较长的叙事段落后有概率插入感官描写
            if (len(para) > 80
                    and random.random() < intensity * 0.2
                    and not any(q in para for q in '""')):
                # 按权重选择感官类型
                r = random.random()
                if r < weight_visual:
                    sense = "visual"
                elif r < weight_visual + weight_auditory:
                    sense = "auditory"
                elif r < weight_visual + weight_auditory + weight_tactile:
                    sense = "tactile"
                elif r < weight_visual + weight_auditory + weight_tactile + weight_olfactory:
                    sense = "olfactory"
                else:
                    sense = "gustatory"

                insertion = random.choice(sensory_insertions[sense])
                # 在段末句号前插入
                if para.endswith("。"):
                    new_paragraphs[-1] = para[:-1] + "。" + insertion + "。"
                else:
                    new_paragraphs[-1] = para + insertion + "。"
                changes += 1

        return "\n".join(new_paragraphs), {"changes": changes}

    def _step6_free_indirect_discourse(self, text: str, intensity: float) -> tuple:
        """
        Step 6: 自由间接引语
        将部分直接思想描述转为自由间接引语形式。
        "他想，这太危险了" → "太危险了。"
        "她心想，自己不该来" → "不该来的。"
        """
        changes = 0

        # 模式: 他/她想/心想，"..."。→ 自由间接引语
        pattern = r'(?:他|她)(?:想|心想|暗想|寻思)[，：:]\s*["""]?([^。""]*)[。"""]?'

        def replace_thought(match):
            nonlocal changes
            if random.random() > intensity * 0.5:
                return match.group()

            thought = match.group(1).strip()
            if not thought:
                return match.group()

            changes += 1
            # 转为自由间接引语: 保留内容，删除"他想"标记
            # 可能添加语气词
            endings = ["。", "……", "——", "罢了。", "吗？"]
            ending = random.choice(endings) if random.random() < 0.3 else "。"

            return thought + ending

        text = re.sub(pattern, replace_thought, text)

        return text, {"changes": changes}

    def _step7_burstiness_optimization(self, text: str, intensity: float) -> tuple:
        """
        Step 7: 突发度优化
        调整句长变化，增加真人写作的"突发度"特征。
        AI文本句长趋于均匀，真人文本句长变化更大。
        交替使用长短句，偶尔穿插极短句（2-5字）。
        """
        changes = 0
        sentences = re.split(r'(?<=[。！？])', text)

        result = []
        prev_length = 0

        for i, sent in enumerate(sentences):
            sent = sent.strip()
            if not sent:
                continue

            current_length = len(sent)

            # 如果连续两个中等长度句子，考虑在后面插入短句
            if (prev_length > 15 and current_length > 15
                    and random.random() < intensity * 0.15):
                short_phrases = [
                    "真熟。", "不对。", "有了。", "来了。",
                    "笑了。", "走吧。", "难说。", "算了。",
                    "妙极。", "荒唐。", "可惜。", "罢了。",
                ]
                result.append(sent)
                # Only insert if the previous sentence is a narrative sentence
                if not any(q in sent for q in '""'):
                    result.append(random.choice(short_phrases))
                    changes += 1
            # 如果当前句子太长(>60字)，尝试在逗号处拆分
            elif current_length > 60 and random.random() < intensity * 0.4:
                comma_positions = [m.start() for m in re.finditer('，', sent)]
                if comma_positions:
                    # 在中间位置的逗号处拆分
                    mid_pos = len(comma_positions) // 2
                    break_pos = comma_positions[mid_pos]
                    first_part = sent[:break_pos] + "。"
                    second_part = sent[break_pos + 1:]  # 跳过逗号
                    result.append(first_part)
                    result.append(second_part)
                    changes += 1
                else:
                    result.append(sent)
            else:
                result.append(sent)

            prev_length = current_length

        return "".join(result), {"changes": changes}

    def _step8_strip_ai_ending(self, text: str) -> tuple:
        """
        Step 8: AI结尾清除（增强版）
        AI生成的章节常以总结段、展望段、升华段结尾，这在网文中非常不自然。
        检测并移除章节末尾的AI式总结/展望段落。
        更激进的策略：检查最后4段，发现AI味立即删除。
        """
        stripped = 0

        # AI典型结尾段落特征（扩充模式库）
        ai_ending_signals = [
            # 时间预告类
            r'(?:而)?这(?:一切|一天|一刻|一晚|个夜晚|一夜).*(?:才刚刚开始|不过是.*开始|只是.*序章|远未结束)',
            r'(?:他|她|他们)(?:不知道|并不知道|还不知道|没有意识到).*(?:即将|等待着|正在等着|将要)',
            r'(?:命运|故事|一切|旅程).*(?:才刚刚|远未|从未|刚刚).*(?:开始|结束|落幕|拉开序幕)',
            r'(?:新的|更大的|真正的|下一个).*(?:挑战|冒险|考验|风暴|篇章|战斗).*(?:即将|正在|已经在|悄然)',
            
            # 情绪总结类
            r'夜.*(?:很深|更深了|渐深|深了|沉了).*(?:而|但|可|然而).*(?:还在|才刚|仍在|依然)',
            r'(?:月光|月色|星光|夜色).*(?:见证|笼罩|洒落).*(?:这一切|一切|这段)',
            r'(?:一切|世界|周遭).*(?:归于|陷入|恢复|重归).*(?:平静|宁静|寂静|沉寂)',
            
            # 预知未来类
            r'(?:他|她)知道.*(?:从此|以后|未来|今后|此后).*(?:将会|不会再|再也不|不再)',
            r'(?:他|她)(?:意识到|明白|清楚).*(?:这只是|不过是).*(?:开始|序幕)',
            r'殊不知',
            r'(?:他|她)(?:并不知道|还不知道).*(?:在.*背后|在.*深处|在某个角落)',
            
            # 悬念预告类  
            r'(?:一场|一个)(?:更大|更深|更可怕|更复杂)的.*(?:正在|已经在|悄悄地|即将)',
            r'(?:危机|阴谋|变数|风暴).*(?:正在|已经在|即将).*(?:酝酿|发酵|来临|逼近)',
            r'(?:而|但|然而).*(?:在.*处|在.*中|在.*里).*(?:一场|一个|一道).*(?:正在|已经)',
            
            # 氛围渲染类（AI喜欢用环境收尾）
            r'夜风.*(?:吹过|拂过|掠过|带走|卷起)',
            r'(?:窗外|远处|天边).*(?:传来|响起|升起).*(?:依旧|仍然|还在)',
            
            # 心理升华类
            r'(?:命运的|历史的|时代的).*(?:车轮|齿轮|巨轮).*(?:滚滚|缓缓|悄然).*(?:向前|转动|碾过)',
            r'(?:这|那).*(?:注定|必将|终将).*(?:是|成为).*(?:一个|一段|一场)',
        ]

        # 检查最后4个段落（从2段增加到4段，更激进）
        paragraphs = text.rstrip().split("\n")
        while paragraphs and not paragraphs[-1].strip():
            paragraphs.pop()

        if len(paragraphs) < 3:
            return text, {"stripped": 0}

        # 从最后一段开始往前检查，最多检查最后4段
        removed = 0
        for _ in range(4):  # Changed from 2 to 4
            if len(paragraphs) < 3:
                break
            last_para = paragraphs[-1].strip()
            if not last_para:
                paragraphs.pop()
                continue

            is_ai_ending = False
            
            # 检测AI结尾模式
            for pattern in ai_ending_signals:
                if re.search(pattern, last_para):
                    is_ai_ending = True
                    break
            
            # 额外检测：如果最后一段是纯景物描写且包含"依旧/仍然/还在"，也删除
            if not is_ai_ending and removed == 0:  # 只对最后一段做此检查
                if (re.search(r'依旧|仍然|还在|继续', last_para) and 
                    not re.search(r'"|"', last_para) and  # 不包含对话
                    len(last_para) < 50):  # 短段落
                    is_ai_ending = True

            if is_ai_ending:
                paragraphs.pop()
                removed += 1
            else:
                break

        if removed > 0:
            text = "\n".join(paragraphs)
            stripped = removed

        return text, {"stripped": stripped}

    def _step9_narrative_sequence_check(self, text: str) -> tuple:
        """
        Step 8: 叙事顺序检查
        检查时间顺序一致性，检测不合理的叙事跳跃。
        这是验证步骤，主要报告问题而非修改文本。
        """
        issues = []

        # 检测时间矛盾
        time_pairs = [
            ("之前", "之后"),
            ("过去", "未来"),
            ("昨天", "明天"),
        ]

        sentences = re.split(r'[。！？]+', text)
        sentences = [s.strip() for s in sentences if s.strip()]

        # 检测叙事主体突然切换
        prev_subject = None
        subject_switches = 0
        for sent in sentences:
            subject_match = re.match(r'^([\u4e00-\u9fa5]{2,4})', sent)
            if subject_match:
                current = subject_match.group(1)
                if prev_subject and current != prev_subject and current not in ["他", "她", "它", "我"]:
                    subject_switches += 1
                prev_subject = current

        if subject_switches > len(sentences) * 0.3:
            issues.append({
                "type": "frequent_subject_switch",
                "description": f"叙事主体切换过于频繁（{subject_switches}次/{len(sentences)}句）",
                "severity": "warning",
            })

        # 检测重复用词
        words = list(jieba.cut(text))
        content_words = [w for w in words if len(w) >= 2]
        word_freq = Counter(content_words)
        high_freq = [(w, c) for w, c in word_freq.most_common(10)
                     if c > len(sentences) * 0.1 and len(w) >= 2]
        if high_freq:
            issues.append({
                "type": "repetitive_vocabulary",
                "description": f"高频重复词: {', '.join(f'{w}({c}次)' for w, c in high_freq[:5])}",
                "severity": "info",
            })

        return text, {"issues": issues, "passed": len(issues) == 0}


def _similarity(text1: str, text2: str) -> float:
    """简单Jaccard相似度"""
    set1 = set(text1)
    set2 = set(text2)
    intersection = set1 & set2
    union = set1 | set2
    if not union:
        return 1.0
    return len(intersection) / len(union)
