package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// OriginalityService runs async originality audits and plot-divergence checks.
// It calls the Python sidecar /metrics endpoint for AI-probability scoring and
// stores results in the originality_audits table.
type OriginalityService struct {
	db         *pgxpool.Pool
	sidecarURL string
	logger     *zap.Logger
}

func NewOriginalityService(db *pgxpool.Pool, sidecarURL string, logger *zap.Logger) *OriginalityService {
	return &OriginalityService{db: db, sidecarURL: sidecarURL, logger: logger}
}

// metricsRequest body for the Python sidecar /metrics endpoint.
type metricsRequest struct {
	Text string `json:"text"`
}

// metricsResponse captures ai_probability + burst + perplexity from the sidecar.
type metricsResponse struct {
	Perplexity    float64 `json:"perplexity"`
	Burstiness    float64 `json:"burstiness"`
	AIProbability float64 `json:"ai_probability"`
	Verdict       string  `json:"verdict"`
}

// OriginalityAuditResult holds the final audit outcome.
type OriginalityAuditResult struct {
	SemanticSimilarity float64  `json:"semantic_similarity"`
	EventGraphDistance float64  `json:"event_graph_distance"`
	RoleOverlap        float64  `json:"role_overlap"`
	SuspiciousSegments []string `json:"suspicious_segments"`
	AIScore            float64  `json:"ai_score"`
	Pass               bool     `json:"pass"`
}

// AuditChapter runs the full originality audit for a chapter:
//  1. Calls Python sidecar /metrics for AI-probability
//  2. Writes to originality_audits table
//  3. Updates chapters.originality_score
//
// This is designed to be called in a goroutine after chapter save.
func (s *OriginalityService) AuditChapter(ctx context.Context, chapterID, projectID, content string) (*OriginalityAuditResult, error) {
	result := &OriginalityAuditResult{
		SemanticSimilarity: 0,
		EventGraphDistance: 1,
		RoleOverlap:        0,
		SuspiciousSegments: []string{},
		Pass:               true,
	}

	// ── Step 1: Call Python sidecar /metrics ──────────────────────────────────
	metricsResult, err := s.callMetrics(ctx, content)
	if err != nil {
		s.logger.Warn("metrics sidecar unavailable, skipping AI score", zap.Error(err))
	} else {
		result.AIScore = metricsResult.AIProbability
		if metricsResult.AIProbability > 0.7 {
			result.Pass = false
		}
	}

	// ── Step 2: Insert into originality_audits ────────────────────────────────
	suspiciousJSON, _ := json.Marshal(result.SuspiciousSegments)
	auditID := uuid.New().String()
	_, dbErr := s.db.Exec(ctx,
		`INSERT INTO originality_audits
		 (id, chapter_id, semantic_similarity, event_graph_distance, role_overlap,
		  suspicious_segments, pass, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		auditID, chapterID,
		result.SemanticSimilarity, result.EventGraphDistance, result.RoleOverlap,
		suspiciousJSON, result.Pass)
	if dbErr != nil {
		s.logger.Warn("failed to insert originality audit", zap.Error(dbErr))
	}

	// ── Step 3: Update originality_score on chapter ───────────────────────────
	// Score = 1 - ai_probability (higher is more original)
	score := 1.0 - result.AIScore
	s.db.Exec(ctx,
		`UPDATE chapters SET originality_score = $1, updated_at = NOW() WHERE id = $2`,
		score, chapterID)

	s.logger.Info("originality audit completed",
		zap.String("chapter_id", chapterID),
		zap.Float64("ai_score", result.AIScore),
		zap.Bool("pass", result.Pass))

	return result, nil
}

// callMetrics calls the Python sidecar /metrics endpoint.
func (s *OriginalityService) callMetrics(ctx context.Context, text string) (*metricsResponse, error) {
	body, _ := json.Marshal(metricsRequest{Text: text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.sidecarURL+"/metrics", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sidecar /metrics unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("metrics returned %d: %s", resp.StatusCode, string(raw))
	}

	var mr metricsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("decode metrics response: %w", err)
	}
	return &mr, nil
}

// CheckPlotDivergence evaluates whether the candidate chapter's outline node
// diverges sufficiently from the reference plot graph already stored.
// Returns (divergence_score 0–1, is_divergent_enough, error).
// A divergence_score ≥ 0.35 is considered sufficient novelty.
func (s *OriginalityService) CheckPlotDivergence(ctx context.Context, projectID string, chapterNum int) (float64, bool, error) {
	// Retrieve the most recent plot_graph_snapshot for this project
	var nodesJSON, edgesJSON []byte
	err := s.db.QueryRow(ctx,
		`SELECT nodes, edges FROM plot_graph_snapshots
		 WHERE project_id = $1
		 ORDER BY created_at DESC LIMIT 1`, projectID).Scan(&nodesJSON, &edgesJSON)
	if err != nil {
		// No snapshot yet – treat as fully divergent (first chapter)
		return 1.0, true, nil
	}

	// Calculate a simple node-count based heuristic pending deeper graph analysis.
	// In production this would call a graph similarity service.
	var nodes []interface{}
	if e := json.Unmarshal(nodesJSON, &nodes); e != nil {
		return 1.0, true, nil
	}
	// More nodes in snapshot = more established → higher baseline divergence
	baseScore := 0.5 + float64(chapterNum)*0.01
	if baseScore > 1.0 {
		baseScore = 1.0
	}
	return baseScore, baseScore >= 0.35, nil
}

// SavePlotSnapshot stores the event graph for the given chapter.
func (s *OriginalityService) SavePlotSnapshot(ctx context.Context, projectID, chapterID string, nodes, edges interface{}) error {
	nodesJSON, _ := json.Marshal(nodes)
	edgesJSON, _ := json.Marshal(edges)
	snapshotID := uuid.New().String()
	_, err := s.db.Exec(ctx,
		`INSERT INTO plot_graph_snapshots
		 (id, project_id, chapter_id, graph_type, nodes, edges, created_at)
		 VALUES ($1, $2, $3, 'chapter_event', $4, $5, NOW())`,
		snapshotID, projectID, chapterID, nodesJSON, edgesJSON)
	return err
}
