package services

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/novelbuilder/backend/internal/models"
)

type aiFingerprintSpec struct {
	Label      string
	Pattern    string
	Regex      bool
	Threshold  int
	Severity   string
	Suggestion string
}

var aiFingerprintSpecs = []aiFingerprintSpec{
	{
		Label:      "三件套情绪反应",
		Pattern:    `看了一眼|心跳漏了一拍|眼眶红了|眼眶一红`,
		Regex:      true,
		Threshold:  2,
		Severity:   "warning",
		Suggestion: "改成更具体的生理反应、动作停顿或误读，避免固定情绪模板。",
	},
	{
		Label:      "不是……是……模板",
		Pattern:    `不是[………\.]{1,3}是|不是[^。！？]{0,12}而是`,
		Regex:      true,
		Threshold:  2,
		Severity:   "warning",
		Suggestion: "用角色的判断变化、口误或具体行动转折替代排比式辨析。",
	},
	{
		Label:      "机械沉默停顿",
		Pattern:    `(?:沉默|安静)[了得]?[\d一二三四五六七八九十半两]*秒|愣了一下|顿了顿`,
		Regex:      true,
		Threshold:  3,
		Severity:   "warning",
		Suggestion: "用“一怔”、动作失误、对话断裂或环境反馈表达停顿。",
	},
	{
		Label:      "气味/嗡鸣套话",
		Pattern:    `空气中弥漫着.{0,16}(?:气味|味道|香气)|(?:空调|显示器|机械|冰箱|机器).{0,12}(?:嗡鸣|嗡嗡)`,
		Regex:      true,
		Threshold:  1,
		Severity:   "warning",
		Suggestion: "环境细节必须改变人物判断或行动，不要用通用空镜头开场。",
	},
	{
		Label:      "涌上心头/眼中闪过",
		Pattern:    `一股.{0,12}(?:涌上|涌入)心头|心中暗道|眼中闪过一丝|嘴角.{0,4}勾起.{0,6}弧度`,
		Regex:      true,
		Threshold:  1,
		Severity:   "warning",
		Suggestion: "用可观察的动作、短促念头或对话潜台词替代抽象情绪包装。",
	},
	{
		Label:      "默认中国生活细节",
		Pattern:    `咖啡|星巴克|糖醋排骨|炖排骨|烤肉店|日料店`,
		Regex:      true,
		Threshold:  4,
		Severity:   "info",
		Suggestion: "除非剧情需要，换成更贴近人物地域、收入、年龄和场景的具体物件。",
	},
	{
		Label:      "AI式收尾预告",
		Pattern:    `这只是.{0,8}开始|更大的(?:风暴|挑战|危机|考验).{0,12}(?:即将|正在)|命运的齿轮|新的篇章|未来.{0,16}(?:不会平静|还在等待)`,
		Regex:      true,
		Threshold:  1,
		Severity:   "critical",
		Suggestion: "直接断在动作、对话或信息揭露高点，不写总结、展望或升华。",
	},
}

func antiAIFingerprintPromptBlock(projectGenre string) string {
	var sb strings.Builder
	sb.WriteString("=== AI指纹规避清单（写作前置约束）===\n")
	sb.WriteString("本项目采用“先规避、再生成、后审计”的链路。写正文时不要依赖后处理去修补AI味，必须在第一稿中主动避开下列指纹：\n")
	sb.WriteString("- 情绪反应不要套用【看了一眼 / 心跳漏了一拍 / 眼眶红了】三件套；让情绪落到动作、误判、沉默后的选择或一句没说完的话。\n")
	sb.WriteString("- 避免高频【不是……是……】【不是X，而是Y】辨析句；需要转折时，用场景证据或人物反应自然显出来。\n")
	sb.WriteString("- 少写【沉默了X秒】【安静了X秒】【愣了一下】；可用“一怔”、手上动作出错、话锋断裂、视线躲开等具体变化。\n")
	sb.WriteString("- 禁止通用环境套话：【空气中弥漫着XX气味】【只剩空调/显示器/机器嗡鸣】。环境描写必须推动人物行动、暴露信息差或改变气氛压力。\n")
	sb.WriteString("- 不默认现代中国角色只喝咖啡、只吃糖醋/炖排骨、只去烤肉/日料；饮食、品牌、交通、付款方式、街区细节要服从人物阶层、地域和当时目的。\n")
	sb.WriteString("- 代词和句首要有变化：不要连续用【他/她+动词+了】；可穿插省略主语、动作短句、客体先行句、对话打断。\n")
	sb.WriteString("- 句长必须有突发度：短句、半句、长句交替；不要一整章都保持相似长度和相似段落节奏。\n")
	sb.WriteString("- 不要积极偏见。允许角色自然流露烦躁、疲惫、尴尬、后悔、嫉妒、厌恶、麻木等消极情绪，但必须由事件触发。\n")
	sb.WriteString("- 词汇不要只堆名词/动词/形容词。中文叙事需要足量虚词、语气词、转折助词和口语残缺，让句子像人在当场说出来。\n")
	if strings.Contains(projectGenre, "都市") || strings.Contains(projectGenre, "言情") || strings.Contains(projectGenre, "现实") {
		sb.WriteString("- 现代/都市场景要增加本土生活颗粒：奶茶店、小区门禁、外卖袋、地铁换乘、共享单车、便利店、彩票站、社区医院、物业群等；但只在人物会真实接触时使用。\n")
	}
	sb.WriteString("- 反AI不是刻意怪句。所有变化都要服务人物、因果、信息增量和阅读节奏。\n\n")
	return sb.String()
}

func detectAIFingerprintIssues(content string, limit int) ([]models.QualityIssue, float64) {
	issues := make([]models.QualityIssue, 0)
	penalty := 0.0

	for _, spec := range aiFingerprintSpecs {
		count := countFingerprintPattern(content, spec)
		if count < spec.Threshold {
			continue
		}
		issues = append(issues, models.QualityIssue{
			Type:       "ai_fingerprint",
			Severity:   spec.Severity,
			Location:   "全文",
			Message:    fmt.Sprintf("%s出现 %d 次，疑似AI文本固定指纹", spec.Label, count),
			Suggestion: spec.Suggestion,
		})
		switch spec.Severity {
		case "critical":
			penalty += 1.2
		case "warning":
			penalty += 0.55
		default:
			penalty += 0.2
		}
		if limit > 0 && len(issues) >= limit {
			return issues, penalty
		}
	}

	structuralIssues, structuralPenalty := detectStructuralFingerprints(content)
	issues = append(issues, structuralIssues...)
	penalty += structuralPenalty
	if limit > 0 && len(issues) > limit {
		issues = issues[:limit]
	}
	return issues, penalty
}

func countFingerprintPattern(content string, spec aiFingerprintSpec) int {
	if content == "" || spec.Pattern == "" {
		return 0
	}
	if !spec.Regex {
		return strings.Count(content, spec.Pattern)
	}
	re, err := regexp.Compile(spec.Pattern)
	if err != nil {
		return 0
	}
	return len(re.FindAllStringIndex(content, -1))
}

func detectStructuralFingerprints(content string) ([]models.QualityIssue, float64) {
	sentences := splitChineseSentences(content)
	if len(sentences) < 8 {
		return nil, 0
	}

	issues := make([]models.QualityIssue, 0, 3)
	penalty := 0.0

	pronounStarts := map[rune]int{}
	for _, sentence := range sentences {
		runes := []rune(strings.TrimSpace(sentence))
		if len(runes) == 0 {
			continue
		}
		switch runes[0] {
		case '他', '她', '我', '你':
			pronounStarts[runes[0]]++
		}
	}
	maxPronoun := 0
	var maxPronounRune rune
	for r, count := range pronounStarts {
		if count > maxPronoun {
			maxPronoun = count
			maxPronounRune = r
		}
	}
	if maxPronoun >= 6 && float64(maxPronoun)/float64(len(sentences)) > 0.45 {
		issues = append(issues, models.QualityIssue{
			Type:       "ai_fingerprint",
			Severity:   "warning",
			Location:   "全文",
			Message:    fmt.Sprintf("句首代词“%c”占比过高（%d/%d），句式显得单调", maxPronounRune, maxPronoun, len(sentences)),
			Suggestion: "混合使用省略主语、宾语前置、动作起句、对话起句和环境反馈起句。",
		})
		penalty += 0.45
	}

	if burst := sentenceLengthBurstiness(sentences); burst > 0 && burst < 0.28 {
		issues = append(issues, models.QualityIssue{
			Type:       "ai_fingerprint",
			Severity:   "warning",
			Location:   "全文",
			Message:    fmt.Sprintf("句长突发度偏低（%.2f），读感过于均匀", burst),
			Suggestion: "在紧张场景插入极短句、半句和打断句；在铺垫处允许少量长句形成节奏差。",
		})
		penalty += 0.4
	}

	if positiveBiasScore(content) >= 1 {
		issues = append(issues, models.QualityIssue{
			Type:       "ai_fingerprint",
			Severity:   "info",
			Location:   "全文",
			Message:    "正向情绪词明显多于负向情绪，可能存在AI式积极偏见",
			Suggestion: "让人物按处境自然暴露烦躁、迟疑、厌恶、疲惫或难堪等负面情绪。",
		})
		penalty += 0.2
	}

	return issues, penalty
}

func splitChineseSentences(content string) []string {
	fields := strings.FieldsFunc(content, func(r rune) bool {
		switch r {
		case '。', '！', '？', '；', '!', '?', ';', '\n':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len([]rune(field)) >= 4 {
			out = append(out, field)
		}
	}
	return out
}

func sentenceLengthBurstiness(sentences []string) float64 {
	if len(sentences) < 2 {
		return 0
	}
	lengths := make([]float64, 0, len(sentences))
	for _, sentence := range sentences {
		lengths = append(lengths, float64(len([]rune(sentence))))
	}
	mean := 0.0
	for _, length := range lengths {
		mean += length
	}
	mean /= float64(len(lengths))
	if mean == 0 {
		return 0
	}
	variance := 0.0
	for _, length := range lengths {
		delta := length - mean
		variance += delta * delta
	}
	variance /= float64(len(lengths))
	return math.Sqrt(variance) / mean
}

func positiveBiasScore(content string) int {
	positiveWords := []string{"温暖", "希望", "坚定", "释然", "欣慰", "从容", "美好", "微笑", "明亮", "感动"}
	negativeWords := []string{"烦躁", "厌恶", "疲惫", "尴尬", "难堪", "后悔", "嫉妒", "麻木", "不耐烦", "害怕", "慌", "恶心"}
	pos := 0
	neg := 0
	for _, word := range positiveWords {
		pos += strings.Count(content, word)
	}
	for _, word := range negativeWords {
		neg += strings.Count(content, word)
	}
	if pos >= 6 && neg == 0 {
		return 1
	}
	return 0
}
