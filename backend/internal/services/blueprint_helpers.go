package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/novelbuilder/backend/internal/models"
)

type rawJSONScanner struct {
	dst *json.RawMessage
}

func (s rawJSONScanner) Scan(src interface{}) error {
	if s.dst == nil {
		return nil
	}
	switch v := src.(type) {
	case nil:
		*s.dst = nil
	case []byte:
		*s.dst = append((*s.dst)[:0], v...)
	case string:
		*s.dst = append((*s.dst)[:0], v...)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		*s.dst = append((*s.dst)[:0], b...)
	}
	if len(*s.dst) == 0 {
		*s.dst = json.RawMessage(`null`)
	}
	return nil
}

// BlueprintExport represents the complete blueprint package for export/import.
type BlueprintExport struct {
	Blueprint       models.BookBlueprint   `json:"blueprint"`
	Volumes         []models.Volume        `json:"volumes"`
	ChapterOutlines []models.Outline       `json:"chapter_outlines"`
	Characters      []models.Character     `json:"characters"`
	Foreshadowings  []models.Foreshadowing `json:"foreshadowings"`
	WorldBible      *models.WorldBible     `json:"world_bible,omitempty"`
	ExportedAt      time.Time              `json:"exported_at"`
	Version         string                 `json:"version"` // Format version for compatibility
}

// min returns the smaller of two ints (stdlib min is Go 1.21+).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractBlueprintJSON strips markdown code fences, then extracts the outermost
// JSON object while correctly tracking string literals so that { } inside
// quoted values do not corrupt the depth counter.
func extractBlueprintJSON(s string) string {
	s = strings.TrimSpace(s)
	// Strip markdown code fences: ```json ... ``` or ``` ... ```
	if idx := strings.Index(s, "```"); idx != -1 {
		rest := s[idx+3:]
		if nl := strings.IndexByte(rest, '\n'); nl != -1 {
			rest = rest[nl+1:]
		}
		if end := strings.LastIndex(rest, "```"); end != -1 {
			rest = rest[:end]
		}
		s = strings.TrimSpace(rest)
	}

	start := strings.Index(s, "{")
	if start == -1 {
		return s
	}

	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inStr {
			escaped = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	// Truncated JSON — return what we have and let the caller attempt to parse.
	return s[start:]
}

// buildWorldBibleFieldsHint returns JSON field hint string appropriate for the genre.
func buildWorldBibleFieldsHint(genre string) string {
	switch genre {
	case "西幻":
		return `
    "world_view": "世界观总览（大陆/世界名称、历史纪元）",
    "era_background": "时代背景（当前纪元状况、主要历史事件）",
    "geography": "地理环境（大陆格局、主要地名、地形特色）",
    "races": "种族体系（精灵/矮人/兽人/人类等各族特征与关系）",
    "magic_system": "魔法体系（规则、代价、等级划分、施法方式）",
    "power_system": "职业体系（骑士/法师/游侠/牧师等职业设定）",
    "faction_structure": "阵营结构（王国/帝国/公会/神殿等势力划分）",
    "social_structure": "社会结构（贵族制度、平民生活、种族关系）",
    "religion_mythology": "神明与神话（主要神系、宗教信仰、神话传说）",
    "core_conflict": "核心冲突（黑暗势力/古老诅咒/种族矛盾等主要矛盾）"`
	case "玄幻":
		return `
    "world_view": "世界观概述",
    "era_background": "时代背景",
    "geography": "地理环境",
    "cultivation_system": "修炼体系（境界划分、修炼方式、天才资质标准）",
    "power_system": "力量体系（法则、禁忌、至高境界）",
    "social_structure": "社会结构（宗门/家族/皇朝势力格局）",
    "core_conflict": "核心冲突"`
	case "末世":
		return `
    "world_view": "末世背景（灾变类型、爆发时间、当前时间节点）",
    "era_background": "时代背景",
    "geography": "地理环境（安全区/危险区/资源点分布）",
    "threat_system": "威胁体系（变异生物/病毒/怪物等级划分）",
    "power_system": "力量体系（异能/进化/武装类型）",
    "social_structure": "社会结构（幸存者营地/组织/势力格局）",
    "resource_economy": "资源经济（稀缺资源、交易体系）",
    "core_conflict": "核心冲突"`
	default:
		return `
    "world_view": "世界观概述",
    "era_background": "时代背景",
    "geography": "地理环境",
    "social_structure": "社会结构",
    "power_system": "力量体系",
    "core_conflict": "核心冲突"`
	}
}

func writingLanguageInstruction(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "en", "en-us", "en_us", "english":
		return "All creative assets, outlines, character profiles, and final prose must be written in English. Keep names, idioms, punctuation, dialogue style, and cultural references internally consistent with English-language fiction. Do not mix Chinese prose unless the user explicitly asks for bilingual text."
	default:
		return "所有创作资产、大纲、角色档案与最终正文必须使用简体中文。人名、地名、术语、标点、对白口吻保持中文小说语境一致；除非用户明确要求双语文本，不要混入英文叙述。"
	}
}

// buildGenreConstraints returns genre-specific bullet-point constraints for the prompt.
func buildGenreConstraints(genre string, gt *models.GenreTemplate) string {
	var points []string

	switch genre {
	case "西幻":
		points = []string{
			"- 人名、地名、技能名须采用西式风格（可音译或创造），避免使用中文传统风格词汇",
			"- 魔法体系须有明确规则与代价，不能是\"万能魔法\"",
			"- master_outline 须体现英雄旅程阶段：「启程→考验→深渊→涅槃→归返」",
			"- 角色 profile 应包含种族、职业、技能特色",
			"- 【题材禁入元素】严禁出现以下不属于西幻的元素：科技/机械/电子设备/枪械/火箭/电脑/手机/网络/基因工程/纳米技术/人工智能/太空旅行/修炼境界/丹药/灵石/宗门体系/仙人/渡劫飞升/现代都市场景。一切力量来源必须是魔法、神力、血脉或自然元素，禁止出现科技驱动的力量体系。",
		}
	case "玄幻":
		points = []string{
			"- 修炼境界须清晰标注，成长不可一步登天",
			"- 战斗描写须结合力量体系，避免泛化",
			"- master_outline 须体现修炼突破的阶段感",
			"- 【题材禁入元素】严禁出现以下不属于玄幻的元素：枪械/火箭/电脑/手机/网络/基因工程/纳米技术/人工智能/太空旅行/西式骑士团/精灵矮人等西幻种族/现代都市场景/现代科技产品。一切力量来源必须是修炼、功法、天材地宝、血脉觉醒等修仙体系元素。",
		}
	case "末世":
		points = []string{
			"- 资源匮乏和紧张感须贯穿全文规划",
			"- 异能/进化逻辑须与世界设定自洽",
			"- master_outline 须体现生存→建立据点→反攻的递进结构",
			"- 【题材禁入元素】严禁出现以下不属于末世的元素：修炼境界/丹药/灵石/宗门体系/仙人/魔法/精灵矮人等奇幻种族/太空旅行/星际贸易。一切力量来源必须基于末世变异/异能觉醒/科技残留/生物进化等末世体系元素。",
		}
	case "科幻":
		points = []string{
			"- 科技设定须自洽，技术限制和副作用需与优势并存",
			"- 世界观须有宏观政治体系和微观生活细节",
			"- 【题材禁入元素】严禁出现以下不属于科幻的元素：修炼境界/丹药/灵石/宗门体系/仙人/魔法咒语/魔杖/精灵矮人等奇幻种族/武侠内功/剑气。一切力量来源必须基于科学技术/基因改造/机械增强/AI等科技体系元素。",
		}
	case "都市":
		points = []string{
			"- 社会规则、法律、商业逻辑须合理",
			"- 角色能力成长需符合现实逻辑",
			"- 【题材禁入元素】严禁出现以下不属于都市的元素：修炼飞升/魔法/精灵矮人/星际旅行/末世灾变（除非设定有超自然元素）。能力设定须以现实为基础。",
		}
	case "言情":
		points = []string{
			"- 感情发展须有事件驱动，不可无理由心动",
			"- 角色需有独立人格和成长弧线",
			"- 【题材禁入元素】禁止出现与感情主线无关的大量战斗/修炼/科技展示等喧宾夺主的内容。",
		}
	case "悬疑":
		points = []string{
			"- 核心谜题须在首卷前3章内抛出",
			"- 线索须公平分布，禁止突然出现从未提及的关键信息",
			"- 【题材禁入元素】严禁出现超自然力量/魔法/修炼等破坏推理逻辑的元素（除非设定为超自然悬疑）。",
		}
	}

	if gt != nil && len(points) == 0 {
		// Generic genre — no extra hard constraints beyond the template already in context.
		return ""
	}

	return strings.Join(points, "\n")
}

// buildGenreExclusionBlock returns the genre-specific forbidden element text for injection
// into chapter generation system prompts. This ensures the chapter author AI also enforces
// genre boundaries, not just the outline planner.
func buildGenreExclusionBlock(genre string) string {
	switch genre {
	case "西幻":
		return "【题材禁入元素 — 违反即为严重错误】本作品为西幻题材。严禁出现：科技产品（电脑/手机/枪械/机械装置）、修仙元素（丹药/灵石/宗门/渡劫飞升）、现代都市场景。一切力量来源必须基于魔法/神力/血脉/自然元素。"
	case "玄幻":
		return "【题材禁入元素 — 违反即为严重错误】本作品为玄幻题材。严禁出现：现代科技产品（电脑/手机/枪械）、西幻种族（精灵/矮人/兽人）、现代都市场景。一切力量来源必须基于修炼/功法/天材地宝/血脉觉醒。"
	case "末世":
		return "【题材禁入元素 — 违反即为严重错误】本作品为末世题材。严禁出现：修仙元素（丹药/灵石/宗门/飞升）、纯奇幻种族（精灵/矮人）、完好如初的现代社会秩序。一切设定需基于末世背景。"
	case "科幻":
		return "【题材禁入元素 — 违反即为严重错误】本作品为科幻题材。严禁出现：修仙元素（丹药/灵石/功法/渡劫）、纯魔法体系（咒语/魔杖/魔法阵）、中古奇幻种族。一切力量来源必须基于科技。"
	case "都市":
		return "【题材禁入元素 — 违反即为严重错误】本作品为都市题材。严禁出现：修炼飞升/魔法/奇幻种族/星际旅行/末世灾变等超出现实框架的元素（除非世界观设定明确允许）。"
	case "言情":
		return "【题材禁入元素】本作品为言情题材。战斗/修炼/科技等元素若有则必须为感情主线服务，不可喧宾夺主。"
	case "悬疑":
		return "【题材禁入元素】本作品为悬疑题材。严禁出现破坏推理逻辑的超自然力量（除非世界观设定为超自然悬疑）。"
	default:
		return ""
	}
}

// summariseJSON extracts a short summary string from a JSONB field for prompt context.
func summariseJSON(raw json.RawMessage, maxLen int) string {
	if len(raw) == 0 {
		return ""
	}
	// Try as a map first.
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err == nil {
		var parts []string
		for k, v := range m {
			str := fmt.Sprintf("%v", v)
			if len(str) > 80 {
				str = str[:80] + "…"
			}
			parts = append(parts, fmt.Sprintf("%s: %s", k, str))
			if len(strings.Join(parts, "; ")) > maxLen {
				break
			}
		}
		return strings.Join(parts, "; ")
	}
	// Try as a string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if len(s) > maxLen {
			return s[:maxLen] + "…"
		}
		return s
	}
	// Raw fallback.
	str := string(raw)
	if len(str) > maxLen {
		return str[:maxLen] + "…"
	}
	return str
}

// extractTextFromJSON extracts text from a JSON field that might be a string or {raw_content: "..."}.
func extractTextFromJSON(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		return str
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err == nil {
		if rawContent, ok := obj["raw_content"].(string); ok {
			// Try to parse raw_content as JSON
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(rawContent), &parsed); err == nil {
				// Extract the specific field if exists
				for _, val := range parsed {
					if s, ok := val.(string); ok && s != "" {
						return s
					}
				}
			}
			return rawContent
		}
	}
	return string(data)
}

// extractVolumeSection attempts to extract the outline text for a specific volume
// from the master outline string. The master outline typically uses markers like
// "第N卷" or the volume title to separate sections.
// Returns the volume-specific section, or empty string if extraction fails.
func extractVolumeSection(masterOutline string, volumeNum int, volumeTitle string) string {
	if masterOutline == "" {
		return ""
	}

	// Try multiple patterns to locate the volume section
	// Pattern 1: "第N卷" with Chinese colon or colon
	markers := []string{
		fmt.Sprintf("第%d卷", volumeNum),
		volumeTitle,
	}

	bestStart := -1
	for _, marker := range markers {
		idx := strings.Index(masterOutline, marker)
		if idx >= 0 && (bestStart < 0 || idx < bestStart) {
			bestStart = idx
		}
	}

	if bestStart < 0 {
		return "" // Could not locate volume section
	}

	// Find the end: next volume marker or end of string
	nextVolMarker := fmt.Sprintf("第%d卷", volumeNum+1)
	endIdx := strings.Index(masterOutline[bestStart+1:], nextVolMarker)
	if endIdx >= 0 {
		return strings.TrimSpace(masterOutline[bestStart : bestStart+1+endIdx])
	}

	// No next volume marker found — take the rest but cap at reasonable length
	section := masterOutline[bestStart:]
	if len(section) > 500 {
		// Try to find a natural break point
		if nl := strings.Index(section[400:], "\n"); nl >= 0 {
			section = section[:400+nl]
		} else {
			section = section[:500]
		}
	}
	return strings.TrimSpace(section)
}
