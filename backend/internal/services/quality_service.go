package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// ============================================================
// Quality Check Service (4-role review chain)
// ============================================================

type QualityService struct {
	db     *pgxpool.Pool
	ai     *gateway.AIGateway
	logger *zap.Logger
}

func NewQualityService(db *pgxpool.Pool, ai *gateway.AIGateway, logger *zap.Logger) *QualityService {
	return &QualityService{db: db, ai: ai, logger: logger}
}

func (s *QualityService) RunFullCheck(ctx context.Context, chapterID string) (*models.QualityReport, error) {
	ctx = contextWithLLMSession(ctx, nil, fmt.Sprintf("quality_check:%s", chapterID))

	// Get chapter content
	var content, projectID string
	err := s.db.QueryRow(ctx,
		`SELECT content, project_id FROM chapters WHERE id = $1`, chapterID).Scan(&content, &projectID)
	if err != nil {
		return nil, err
	}

	report := &models.QualityReport{
		WorldConsistency: true,
		CharConsistency:  true,
		TimeConsistency:  true,
		Pass:             true,
	}

	// Run 4-role review chain in parallel conceptually, but sequentially for reliability
	// Role 1: Senior Editor (retention & pacing)
	editorIssues, _ := s.reviewAsEditor(ctx, content)
	report.Issues = append(report.Issues, editorIssues...)

	// Role 2: Loyal Reader (detect AI-ness)
	readerIssues, _ := s.reviewAsReader(ctx, content)
	report.Issues = append(report.Issues, readerIssues...)

	// Role 3: Logic Reviewer (consistency)
	logicIssues, _ := s.reviewAsLogicReviewer(ctx, content, projectID)
	report.Issues = append(report.Issues, logicIssues...)

	// Role 4: Anti-AI Expert (AI detection)
	aiIssues, aiScore := s.reviewAsAntiAIExpert(ctx, content)
	report.Issues = append(report.Issues, aiIssues...)
	report.AIScoreEstimate = aiScore
	report.AIProbability = clampProbability(aiScore / 100.0)
	report.EstimatedBurstiness = estimateBurstiness(content)
	report.Burstiness = report.EstimatedBurstiness
	report.EstimatedPerplexity = estimatePerplexity(content)

	report.Scores = map[string]float64{
		"editor":         scoreIssues(editorIssues),
		"reader":         scoreIssues(readerIssues),
		"logic_reviewer": scoreIssues(logicIssues),
		"anti_ai":        clampScore(10.0 - (aiScore / 12.5)),
	}

	// Calculate overall score on a 10-point scale so it matches the frontend thresholds.
	criticalCount := 0
	warningCount := 0
	for _, issue := range report.Issues {
		if issue.Severity == "critical" {
			criticalCount++
		} else if issue.Severity == "warning" {
			warningCount++
		}
	}
	avgRoleScore := (report.Scores["editor"] + report.Scores["reader"] + report.Scores["logic_reviewer"] + report.Scores["anti_ai"]) / 4.0
	report.OverallScore = clampScore(avgRoleScore - float64(criticalCount)*0.4 - float64(warningCount)*0.1)
	report.Pass = report.OverallScore >= 7.0 && report.AIProbability <= 0.4 && criticalCount == 0

	return report, s.SaveReport(ctx, chapterID, report)
}

func (s *QualityService) SaveReport(ctx context.Context, chapterID string, report *models.QualityReport) error {
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx,
		`UPDATE chapters SET quality_report = $1 WHERE id = $2`,
		reportJSON, chapterID)
	return err
}

func (s *QualityService) reviewAsEditor(ctx context.Context, content string) ([]models.QualityIssue, error) {
	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: `你是一位资深网文编辑，从读者留存和阅读体验角度审核章节质量。

请检查以下方面并以JSON数组返回问题列表：
1. 节奏是否拖沓或过快
2. 爽点是否充足（每千字至少1个小爽点）
3. 章末钩子是否有力（读者是否想看下一章）
4. 描写是否过度或不足
5. 对话是否自然有趣

返回格式：[{"type": "pacing|hook|description|dialogue", "severity": "critical|warning|info", "location": "第X段", "message": "问题描述", "suggestion": "改进建议"}]
只返回JSON数组，不要其他文字。`},
			{Role: "user", Content: content},
		},
		TaskType:    "review_chain",
		MaxTokens:   2000,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, err
	}
	return parseIssues(resp.Content), nil
}

func (s *QualityService) reviewAsReader(ctx context.Context, content string) ([]models.QualityIssue, error) {
	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: `你是一位资深网文读者，善于识别AI生成的文本"味道"。

请检查以下AI味特征：
1. 对话是否每句都完整规范（人类对话有省略、打断、答非所问）
2. 情绪是否直白表达（"他感到难过"是AI味，用行为/感官暗示是人味）
3. 叙事是否过于线性（缺少插叙、倒叙、闪回）
4. 句子长度是否过于均匀（人类写作有极短句和极长句交替）
5. 主语是否全程在场（人类中文写作常省略主语）
6. 是否使用了"他心想"/"想到这里"等AI常用过渡
7. 章节结尾是否有AI式总结/展望/升华段（"他知道这只是开始""更大的挑战还在后面"等）
8. 一章内是否塞入了过多事件（超过3件主要事件=信息过载=AI感强烈）

返回格式：[{"type": "ai_smell", "severity": "critical|warning|info", "location": "第X段/第X句", "message": "问题描述", "suggestion": "具体改写方向"}]
只返回JSON数组。`},
			{Role: "user", Content: content},
		},
		TaskType:    "review_chain",
		MaxTokens:   2000,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, err
	}
	return parseIssues(resp.Content), nil
}

func (s *QualityService) reviewAsLogicReviewer(ctx context.Context, content, projectID string) ([]models.QualityIssue, error) {
	// Get world bible for consistency check
	var worldContent string
	s.db.QueryRow(ctx,
		`SELECT content::text FROM world_bibles WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&worldContent)

	// Get character profiles
	var charInfo []string
	if rows, err := s.db.Query(ctx,
		`SELECT name, profile::text FROM characters WHERE project_id = $1`, projectID); err != nil {
		s.logger.Warn("logic reviewer: failed to load characters", zap.Error(err))
	} else {
		for rows.Next() {
			var name, profile string
			if rows.Scan(&name, &profile) == nil {
				charInfo = append(charInfo, fmt.Sprintf("%s: %s", name, profile))
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			s.logger.Warn("logic reviewer: rows error", zap.Error(err))
		}
	}

	systemPrompt := fmt.Sprintf(`你是一位逻辑审稿人，专门检查小说的世界观、人物、时间线一致性。

世界观设定参考：
%s

角色信息：
%s

请检查以下方面：
1. 角色行为是否符合其性格设定
2. 世界观规则是否被违反
3. 时间线是否有矛盾
4. 物品/能力/状态是否前后一致

返回格式：[{"type": "world|character|timeline|logic", "severity": "critical|warning|info", "location": "第X段", "message": "问题描述", "suggestion": "修正建议"}]
只返回JSON数组。`, worldContent, strings.Join(charInfo, "\n"))

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: content},
		},
		TaskType:    "review_chain",
		MaxTokens:   2000,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, err
	}
	return parseIssues(resp.Content), nil
}

func (s *QualityService) reviewAsAntiAIExpert(ctx context.Context, content string) ([]models.QualityIssue, float64) {
	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: `你是一位反AI检测专家，精通各种AI生成文本的检测方法。专注中文网络小说的AI痕迹检测。

请从以下维度分析这段文本的AI特征：
1. **困惑度评估**：词汇选择是否过于"安全"（高概率词多=AI味重）
2. **爆发度评估**：句子长度的变异系数（CV）是否过低（AI生成句长均匀）
3. **逻辑指纹**：是否存在"问题→分析→结论"的线性逻辑结构
4. **对话特征**：对话是否每句完整（AI）vs有省略打断（人类）
5. **AI高频词检测**：统计"不禁""微微""缓缓""淡淡""默默"出现次数，每个词超过1次即扣分
6. **AI句式检测**："一股XXX涌上心头""心中暗道""嘴角勾起一抹弧度""眼中闪过一丝XXX"等
7. **章节结尾检测**：结尾是否有总结段/展望段/预告式升华（"他知道这只是开始""更大的风暴即将来临"）
8. **视角一致性**：是否存在POV角色不可能知道的信息泄露
9. **标记段落**：标出AI特征最明显的具体段落

返回格式（必须严格JSON）：
{
  "ai_score": 0-100的数字(越高越像AI),
  "issues": [{"type": "ai_detection", "severity": "critical|warning|info", "location": "第X段第Y句", "message": "具体AI特征描述", "suggestion": "具体改写为XXX"}]
}
只返回JSON。`},
			{Role: "user", Content: content},
		},
		TaskType:    "review_chain",
		MaxTokens:   2000,
		Temperature: 0.3,
	})
	if err != nil {
		s.logger.Warn("AI detection: LLM call failed", zap.Error(err))
		return nil, 50
	}

	// Parse response
	respContent := resp.Content
	if idx := strings.Index(respContent, "{"); idx >= 0 {
		if endIdx := strings.LastIndex(respContent, "}"); endIdx >= 0 {
			respContent = respContent[idx : endIdx+1]
		}
	}

	var result struct {
		AIScore float64               `json:"ai_score"`
		Issues  []models.QualityIssue `json:"issues"`
	}
	if err := json.Unmarshal([]byte(respContent), &result); err != nil {
		snippet := respContent
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		s.logger.Warn("AI detection: failed to parse LLM response",
			zap.Error(err),
			zap.String("raw_snippet", snippet),
		)
		return nil, 50
	}

	return result.Issues, result.AIScore
}

// VocabFatigueReport analyzes word frequency across all chapters for vocabulary fatigue detection.
// Inspired by InkOS vocab fatigue detection.
func (s *QualityService) VocabFatigueReport(ctx context.Context, projectID string, topN int) (*models.VocabFatigueReport, error) {
	rows, err := s.db.Query(ctx,
		`SELECT content, chapter_num FROM chapters WHERE project_id = $1 AND status != 'rejected'
		 ORDER BY chapter_num`, projectID)
	if err != nil {
		return nil, fmt.Errorf("vocab fatigue: %w", err)
	}
	defer rows.Close()

	wordChapters := make(map[string]map[int]bool)
	wordTotal := make(map[string]int)
	totalChapters := 0
	chapterRe := regexp.MustCompile(`[\p{Han}]+|[a-zA-Z]+`)

	for rows.Next() {
		var content string
		var chapterNum int
		if err := rows.Scan(&content, &chapterNum); err != nil {
			continue
		}
		totalChapters++
		words := chapterRe.FindAllString(content, -1)
		seen := make(map[string]bool)
		for _, w := range words {
			// normalize: lowercase, skip non-English single chars and short words
			if len([]rune(w)) < 2 && !isChinese(w) {
				continue
			}
			w = strings.ToLower(w)
			wordTotal[w]++
			if !seen[w] {
				seen[w] = true
				if wordChapters[w] == nil {
					wordChapters[w] = make(map[int]bool)
				}
				wordChapters[w][chapterNum] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type stat struct {
		word  string
		total int
		chaps int
	}
	var stats []stat
	for w, total := range wordTotal {
		if total < 3 {
			continue // ignore rare words
		}
		stats = append(stats, stat{w, total, len(wordChapters[w])})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].total > stats[j].total
	})
	if len(stats) > topN {
		stats = stats[:topN]
	}

	result := make([]models.VocabFatigueStat, 0, len(stats))
	for _, s := range stats {
		freq := 0.0
		if totalChapters > 0 {
			freq = float64(s.total) / float64(totalChapters)
		}
		result = append(result, models.VocabFatigueStat{
			Word:                s.word,
			TotalCount:          s.total,
			ChaptersAppeared:    s.chaps,
			FrequencyPerChapter: freq,
		})
	}

	return &models.VocabFatigueReport{
		ProjectID:     projectID,
		TopWords:      result,
		TotalChapters: totalChapters,
		AnalyzedAt:    time.Now(),
	}, nil
}

func isChinese(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func parseIssues(content string) []models.QualityIssue {
	// Try to extract JSON array
	content = strings.TrimSpace(content)
	if idx := strings.Index(content, "["); idx >= 0 {
		if endIdx := strings.LastIndex(content, "]"); endIdx >= 0 {
			content = content[idx : endIdx+1]
		}
	}

	var issues []models.QualityIssue
	if err := json.Unmarshal([]byte(content), &issues); err != nil {
		return nil
	}
	return issues
}

func scoreIssues(issues []models.QualityIssue) float64 {
	score := 10.0
	for _, issue := range issues {
		switch issue.Severity {
		case "critical":
			score -= 2.5
		case "warning":
			score -= 1.0
		default:
			score -= 0.25
		}
	}
	return clampScore(score)
}

func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 10 {
		return 10
	}
	return math.Round(v*10) / 10
}

func clampProbability(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func estimatePerplexity(content string) float64 {
	words := regexp.MustCompile(`[\p{Han}]+|[a-zA-Z]+`).FindAllString(strings.ToLower(content), -1)
	if len(words) == 0 {
		return 0
	}
	unique := make(map[string]struct{}, len(words))
	for _, word := range words {
		unique[word] = struct{}{}
	}
	diversity := float64(len(unique)) / float64(len(words))
	return math.Round(diversity*1000) / 10
}

func estimateBurstiness(content string) float64 {
	parts := strings.FieldsFunc(content, func(r rune) bool {
		switch r {
		case '。', '！', '？', '；', '!', '?', ';', '\n':
			return true
		default:
			return false
		}
	})
	lengths := make([]float64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lengths = append(lengths, float64(len([]rune(part))))
	}
	if len(lengths) < 2 {
		return 0
	}
	mean := 0.0
	for _, v := range lengths {
		mean += v
	}
	mean /= float64(len(lengths))
	if mean == 0 {
		return 0
	}
	variance := 0.0
	for _, v := range lengths {
		delta := v - mean
		variance += delta * delta
	}
	variance /= float64(len(lengths))
	stddev := math.Sqrt(variance)
	return math.Round((stddev/mean)*1000) / 1000
}
