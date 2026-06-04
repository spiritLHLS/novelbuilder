package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/novelbuilder/backend/internal/gateway"
	"go.uber.org/zap"
)

func (s *ChapterService) buildBookRulesPromptBlock(ctx context.Context, projectID string) string {
	var rulesContent, styleGuide string
	var antiAIJSON, bannedJSON json.RawMessage
	if err := s.db.QueryRow(ctx,
		`SELECT rules_content, style_guide, anti_ai_wordlist, banned_patterns
		 FROM book_rules WHERE project_id = $1`,
		projectID).Scan(&rulesContent, &styleGuide, &antiAIJSON, &bannedJSON); err != nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("=== 项目级创作规则 ===\n")
	if strings.TrimSpace(rulesContent) != "" {
		sb.WriteString("【故事规则】\n")
		sb.WriteString(strings.TrimSpace(rulesContent))
		sb.WriteString("\n")
	}
	if strings.TrimSpace(styleGuide) != "" {
		sb.WriteString("【风格指南】\n")
		sb.WriteString(strings.TrimSpace(styleGuide))
		sb.WriteString("\n")
	}
	if words := decodeStringList(antiAIJSON); len(words) > 0 {
		sb.WriteString("【项目禁用/慎用AI词】")
		sb.WriteString(strings.Join(words, "、"))
		sb.WriteString("\n")
	}
	if patterns := decodeStringList(bannedJSON); len(patterns) > 0 {
		sb.WriteString("【项目禁用句式/模式】")
		sb.WriteString(strings.Join(patterns, "；"))
		sb.WriteString("\n")
	}
	sb.WriteString("以上项目规则优先级高于通用风格样本，正文必须遵守。\n\n")
	return sb.String()
}

func decodeStringList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var items []string
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// humanizeContent calls the Python sidecar /humanize endpoint to run the
// 8-step humanization pipeline on the generated text.
// intensity: 0.0-1.0 (0 = no change, 1 = maximum humanization).
func (s *ChapterService) humanizeContent(ctx context.Context, text string, intensity float64) (string, error) {
	if s.sidecarURL == "" {
		return text, nil // no sidecar configured
	}

	body, _ := json.Marshal(map[string]interface{}{
		"text":      text,
		"intensity": intensity,
	})
	raw, err := doRetriableJSONRequest(ctx, s.httpClient, s.logger, "POST /humanize", func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.sidecarURL+"/humanize", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return text, fmt.Errorf("humanizer unreachable: %w", err)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return text, fmt.Errorf("decode humanizer response: %w", err)
	}
	if result.Text == "" {
		return text, nil
	}
	return result.Text, nil
}

func (s *ChapterService) generateSummary(ctx context.Context, content string) string {
	// Truncate for summary generation to avoid token overflow
	// Use more content for better context capture (3000 -> 4000)
	truncated := content
	if utf8.RuneCountInString(truncated) > 4000 {
		runes := []rune(truncated)
		truncated = string(runes[:4000])
	}

	// Enhanced summary prompt for better cross-chapter continuity
	systemPrompt := `你是长篇小说编辑。生成章节摘要时必须记录：
1. 核心剧情：本章推进的1-3件事（具体动作、对话要点）
2. 角色状态：情绪变化、关系变化、位置移动
3. 悬念/伏笔：未解决的问题、埋下的线索
4. 结尾方式：最后一幕的场景和断章点（对话/动作/悬念）
5. 语言风格：句式特征（长短句比例、是否有方言/俚语、叙述节奏快慢）

摘要控制在300-400字。重点是为下一章提供承接依据，而非给读者看的提要。`

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task:      "summarization",
		MaxTokens: 800, // Increased from 500 for more detailed summary
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: truncated},
		},
	})
	if err != nil {
		s.logger.Warn("summary generation failed", zap.Error(err))
		// Fallback: take the first 300 characters (increased from 200)
		runes := []rune(content)
		if len(runes) > 300 {
			return string(runes[:300]) + "..."
		}
		return content
	}
	return resp.Content
}

func (s *ChapterService) generateTitle(ctx context.Context, content string, chapterNum int) string {
	truncated := content
	if utf8.RuneCountInString(truncated) > 1000 {
		runes := []rune(truncated)
		truncated = string(runes[:1000])
	}

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task: "summarization",
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: "请根据以下章节内容生成一个简洁有力的章节标题（不超过10个字）。只输出标题，不要其他内容。"},
			{Role: "user", Content: truncated},
		},
		MaxTokens: 50,
	})
	if err != nil {
		return fmt.Sprintf("第%d章", chapterNum)
	}
	title := strings.TrimSpace(resp.Content)
	if title == "" {
		return fmt.Sprintf("第%d章", chapterNum)
	}
	return title
}
