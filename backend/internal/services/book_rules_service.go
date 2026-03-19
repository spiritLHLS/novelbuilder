package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// BookRulesService manages the book_rules table (style guide + anti-AI rules).
type BookRulesService struct {
	db         *pgxpool.Pool
	sidecarURL string
	httpClient *http.Client
	logger     *zap.Logger
}

func NewBookRulesService(db *pgxpool.Pool, sidecarURL string, logger *zap.Logger) *BookRulesService {
	return &BookRulesService{
		db:         db,
		sidecarURL: sidecarURL,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		logger:     logger,
	}
}

func (s *BookRulesService) Get(ctx context.Context, projectID string) (*models.BookRules, error) {
	var r models.BookRules
	var antiAIJSON, bannedJSON []byte
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, rules_content, style_guide, anti_ai_wordlist, banned_patterns, created_at, updated_at
		 FROM book_rules WHERE project_id = $1`, projectID).
		Scan(&r.ID, &r.ProjectID, &r.RulesContent, &r.StyleGuide,
			&antiAIJSON, &bannedJSON, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.AntiAIWordlist = antiAIJSON
	r.BannedPatterns = bannedJSON
	return &r, nil
}

func (s *BookRulesService) Upsert(ctx context.Context, projectID string, req models.UpdateBookRulesRequest) (*models.BookRules, error) {
	antiAIJSON := req.AntiAIWordlist
	if antiAIJSON == nil {
		antiAIJSON = json.RawMessage(`[]`)
	}
	bannedJSON := req.BannedPatterns
	if bannedJSON == nil {
		bannedJSON = json.RawMessage(`[]`)
	}

	var r models.BookRules
	now := time.Now()
	err := s.db.QueryRow(ctx,
		`INSERT INTO book_rules (project_id, rules_content, style_guide, anti_ai_wordlist, banned_patterns, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)
		 ON CONFLICT (project_id) DO UPDATE
		    SET rules_content    = EXCLUDED.rules_content,
		        style_guide      = EXCLUDED.style_guide,
		        anti_ai_wordlist = EXCLUDED.anti_ai_wordlist,
		        banned_patterns  = EXCLUDED.banned_patterns,
		        updated_at       = EXCLUDED.updated_at
		 RETURNING id, project_id, rules_content, style_guide, anti_ai_wordlist, banned_patterns, created_at, updated_at`,
		projectID, req.RulesContent, req.StyleGuide, antiAIJSON, bannedJSON, now).
		Scan(&r.ID, &r.ProjectID, &r.RulesContent, &r.StyleGuide,
			&r.AntiAIWordlist, &r.BannedPatterns, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GenerateFromBrief calls the sidecar /creative-brief endpoint and stores the result.
func (s *BookRulesService) GenerateFromBrief(
	ctx context.Context,
	projectID string,
	req models.CreativeBriefRequest,
	llmCfg map[string]interface{},
) (*models.CreativeBriefResult, error) {
	body := map[string]interface{}{
		"brief_text": req.BriefText,
		"genre":      req.Genre,
		"llm_config": llmCfg,
	}
	data, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.sidecarURL+"/creative-brief", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("creative-brief sidecar: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("creative-brief sidecar %d: %s", resp.StatusCode, string(raw))
	}

	var result models.CreativeBriefResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse brief result: %w", err)
	}
	return &result, nil
}

// AntiDetectRewrite calls the sidecar /anti-detect/rewrite endpoint.
func (s *BookRulesService) AntiDetectRewrite(
	ctx context.Context,
	chapterID string,
	text string,
	intensity string,
	rules *models.BookRules,
	llmCfg map[string]interface{},
) (*models.AntiDetectResult, error) {
	var antiAI []string
	var banned []string
	if rules != nil {
		json.Unmarshal(rules.AntiAIWordlist, &antiAI)
		json.Unmarshal(rules.BannedPatterns, &banned)
	}

	styleGuide := ""
	if rules != nil {
		styleGuide = rules.StyleGuide
	}

	body := map[string]interface{}{
		"chapter_id":       chapterID,
		"text":             text,
		"intensity":        intensity,
		"style_guide":      styleGuide,
		"anti_ai_wordlist": antiAI,
		"banned_patterns":  banned,
		"llm_config":       llmCfg,
	}
	data, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.sidecarURL+"/anti-detect/rewrite", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anti-detect sidecar: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("anti-detect sidecar %d: %s", resp.StatusCode, string(raw))
	}

	var result models.AntiDetectResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse anti-detect result: %w", err)
	}
	return &result, nil
}
