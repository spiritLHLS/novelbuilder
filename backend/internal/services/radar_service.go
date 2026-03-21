package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/gateway"
	"go.uber.org/zap"
)

// RadarService uses LLM to analyze genre trends and produce recommendations.
type RadarService struct {
	db        *pgxpool.Pool
	aiGateway *gateway.AIGateway
	logger    *zap.Logger
}

func NewRadarService(db *pgxpool.Pool, aiGateway *gateway.AIGateway, logger *zap.Logger) *RadarService {
	return &RadarService{db: db, aiGateway: aiGateway, logger: logger}
}

type RadarScanRequest struct {
	Genre    string `json:"genre"`
	Platform string `json:"platform"` // general / qidian / tomato / jjwxc / wattpad
	Focus    string `json:"focus"`    // optional user instruction
}

type RadarScanResult struct {
	ID        string          `json:"id"`
	ProjectID *string         `json:"project_id"`
	Genre     string          `json:"genre"`
	Platform  string          `json:"platform"`
	Result    json.RawMessage `json:"result"`
	CreatedAt time.Time       `json:"created_at"`
}

// Trend is an individual hot trend item inside the result payload.
type Trend struct {
	Title       string   `json:"title"`
	Score       float64  `json:"score"` // 0-1 popularity estimate
	Keywords    []string `json:"keywords"`
	Description string   `json:"description"`
}

// RadarResult is the structured payload stored in radar_scan_results.result.
type RadarResult struct {
	Trends           []Trend  `json:"trends"`
	ReaderPainPoints []string `json:"reader_pain_points"`
	StyleGuide       string   `json:"style_guide"`
	OpportunityNote  string   `json:"opportunity_note"`
	AvoidPatterns    []string `json:"avoid_patterns"`
}

func (s *RadarService) Scan(ctx context.Context, projectID *string, req RadarScanRequest) (*RadarScanResult, error) {
	platform := req.Platform
	if platform == "" {
		platform = "general"
	}
	genre := req.Genre
	if genre == "" {
		genre = "玄幻"
	}

	prompt := fmt.Sprintf(`你是一名资深网文市场分析师，专注于%s类型的%s平台趋势。
请分析当前该细分市场的热点，输出严格 JSON，格式如下（不要输出任何 markdown 代码块，直接输出裸 JSON）：

{
  "trends": [
    {"title":"...","score":0.9,"keywords":["..."],"description":"..."}
  ],
  "reader_pain_points": ["..."],
  "style_guide": "...",
  "opportunity_note": "...",
  "avoid_patterns": ["..."]
}

%s

要求：
- trends 列至少 5 条热点，按 score 降序
- reader_pain_points 列举最让读者弃书的 5 个原因
- style_guide 用一段话描述当前最受欢迎的行文风格
- opportunity_note 描述当前市场的蓝海机会
- avoid_patterns 列举当前平台最令编辑/读者反感的 5 种套路`, genre, platform, req.Focus)

	raw, err := s.aiGateway.Chat(ctx, gateway.ChatRequest{
		Task: "radar_scan",
		Messages: []gateway.ChatMessage{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("radar llm: %w", err)
	}
	rawContent := raw.Content

	// Validate JSON
	var rr RadarResult
	if err := json.Unmarshal([]byte(rawContent), &rr); err != nil {
		snippet := rawContent
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		s.logger.Warn("radar scan: LLM response is not valid JSON, storing raw text",
			zap.Error(err),
			zap.String("raw_snippet", snippet),
		)
		// Return as-is in a wrapper so front-end can still display it
		rr = RadarResult{OpportunityNote: rawContent}
	}
	resultJSON, _ := json.Marshal(rr)

	id := uuid.New().String()
	var row RadarScanResult
	err = s.db.QueryRow(ctx,
		`INSERT INTO radar_scan_results (id, project_id, genre, platform, result)
		 VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, project_id, genre, platform, result, created_at`,
		id, projectID, genre, platform, resultJSON).
		Scan(&row.ID, &row.ProjectID, &row.Genre, &row.Platform, &row.Result, &row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("save radar result: %w", err)
	}
	return &row, nil
}

// ListRecent returns the last N scan results for a project.
func (s *RadarService) ListRecent(ctx context.Context, projectID *string, limit int) ([]RadarScanResult, error) {
	if limit <= 0 {
		limit = 10
	}
	var (
		queryStr string
		args     []any
	)
	if projectID != nil {
		queryStr = `SELECT id, project_id, genre, platform, result, created_at
		            FROM radar_scan_results
		            WHERE project_id = $1
		            ORDER BY created_at DESC LIMIT $2`
		args = []any{*projectID, limit}
	} else {
		queryStr = `SELECT id, project_id, genre, platform, result, created_at
		            FROM radar_scan_results
		            ORDER BY created_at DESC LIMIT $1`
		args = []any{limit}
	}
	rows, err := s.db.Query(ctx, queryStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RadarScanResult
	for rows.Next() {
		var r RadarScanResult
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Genre, &r.Platform, &r.Result, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
