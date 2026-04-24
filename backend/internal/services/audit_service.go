package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// AuditService runs multi-dimension chapter audits via the Python sidecar
// and persists results to the audit_reports table.
type AuditService struct {
	db         *pgxpool.Pool
	sidecarURL string
	httpClient *http.Client
	logger     *zap.Logger
}

func NewAuditService(db *pgxpool.Pool, sidecarURL string, logger *zap.Logger) *AuditService {
	return &AuditService{
		db:         db,
		sidecarURL: sidecarURL,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		logger:     logger,
	}
}

// RunAudit calls the sidecar /audit/chapter endpoint and persists the report.
// The caller supplies llmCfg (api_key, model, base_url) extracted from the
// currently-configured auditor LLM profile (or default profile).
func (s *AuditService) RunAudit(
	ctx context.Context,
	chapter *models.Chapter,
	projectID string,
	llmCfg map[string]interface{},
	context_ map[string]interface{},
) (*models.AuditReport, error) {
	ctx = contextWithLLMSession(ctx, llmCfg, fmt.Sprintf("audit_chapter:%s", chapter.ID))
	llmCfg = ensureContextSessionConfig(ctx, llmCfg, fmt.Sprintf("audit_chapter:%s", chapter.ID))
	if sessionID := gateway.SessionIDFromContext(ctx); sessionID != "" {
		s.logger.Debug("audit session attached",
			zap.String("chapter_id", chapter.ID),
			zap.String("session_id", sessionID))
	}
	body := map[string]interface{}{
		"chapter_id":   chapter.ID,
		"project_id":   projectID,
		"chapter_text": chapter.Content,
		"chapter_num":  chapter.ChapterNum,
		"context":      context_,
		"llm_config":   llmCfg,
	}
	data, _ := json.Marshal(body)

	raw, err := doRetriableJSONRequest(ctx, s.httpClient, s.logger, "POST /audit/chapter", func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			s.sidecarURL+"/audit/chapter", bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("audit sidecar: %w", err)
	}

	var result struct {
		Dimensions    map[string]models.AuditDimension `json:"dimensions"`
		OverallScore  float64                          `json:"overall_score"`
		Passed        bool                             `json:"passed"`
		AIProbability float64                          `json:"ai_probability"`
		Issues        []string                         `json:"issues"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse audit result: %w", err)
	}

	// Persist
	dimsJSON, _ := json.Marshal(result.Dimensions)
	issuesJSON, _ := json.Marshal(result.Issues)

	var reportID string
	var createdAt time.Time
	err = s.db.QueryRow(ctx,
		`INSERT INTO audit_reports
			(chapter_id, project_id, dimensions, overall_score, passed, ai_probability, issues, revision_count)
		 VALUES ($1, $2, $3, $4, $5, $6, $7,
		   COALESCE((SELECT revision_count+1 FROM audit_reports
		             WHERE chapter_id = $1 ORDER BY created_at DESC LIMIT 1), 0))
		 RETURNING id, created_at`,
		chapter.ID, projectID, dimsJSON, result.OverallScore,
		result.Passed, result.AIProbability, issuesJSON,
	).Scan(&reportID, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("save audit report: %w", err)
	}

	// Also update chapters.quality_report and genre compliance with audit summary
	var genreComplianceScore float64 = 1.0
	var genreViolations []string
	if gc, ok := result.Dimensions["genre_compliance"]; ok {
		genreComplianceScore = gc.Score
		genreViolations = gc.Issues
	}
	genreViolJSON, _ := json.Marshal(genreViolations)
	summaryJSON, _ := json.Marshal(map[string]interface{}{
		"overall_score":  result.OverallScore,
		"passed":         result.Passed,
		"ai_probability": result.AIProbability,
		"issues":         result.Issues,
	})
	s.db.Exec(ctx,
		`UPDATE chapters SET quality_report = $1, genre_compliance_score = $2, genre_violations = $3 WHERE id = $4`,
		summaryJSON, genreComplianceScore, genreViolJSON, chapter.ID)

	return &models.AuditReport{
		ID:            reportID,
		ChapterID:     chapter.ID,
		ProjectID:     projectID,
		Dimensions:    result.Dimensions,
		OverallScore:  result.OverallScore,
		Passed:        result.Passed,
		AIProbability: result.AIProbability,
		Issues:        result.Issues,
		CreatedAt:     createdAt,
	}, nil
}

// GetLatestReport returns the most recent audit report for a chapter.
func (s *AuditService) GetLatestReport(ctx context.Context, chapterID string) (*models.AuditReport, error) {
	var r models.AuditReport
	var dimsJSON, issuesJSON []byte
	err := s.db.QueryRow(ctx,
		`SELECT id, chapter_id, project_id, dimensions, overall_score, passed,
		        ai_probability, issues, revision_count, created_at
		 FROM audit_reports WHERE chapter_id = $1
		 ORDER BY created_at DESC LIMIT 1`, chapterID).
		Scan(&r.ID, &r.ChapterID, &r.ProjectID, &dimsJSON,
			&r.OverallScore, &r.Passed, &r.AIProbability, &issuesJSON,
			&r.RevisionCount, &r.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(dimsJSON, &r.Dimensions); err != nil {
		s.logger.Warn("GetLatestReport: failed to unmarshal dimensions",
			zap.String("chapter_id", chapterID), zap.Error(err))
	}
	if err := json.Unmarshal(issuesJSON, &r.Issues); err != nil {
		s.logger.Warn("GetLatestReport: failed to unmarshal issues",
			zap.String("chapter_id", chapterID), zap.Error(err))
	}
	return &r, nil
}
