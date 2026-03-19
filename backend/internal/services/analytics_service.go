package services

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// AnalyticsService aggregates project statistics for the analytics dashboard.
type AnalyticsService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewAnalyticsService(db *pgxpool.Pool, logger *zap.Logger) *AnalyticsService {
	return &AnalyticsService{db: db, logger: logger}
}

// ChapterStat is one row of per-chapter metrics.
type ChapterStat struct {
	ChapterNum    int     `json:"chapter_num"`
	Title         string  `json:"title"`
	WordCount     int     `json:"word_count"`
	Status        string  `json:"status"`
	AuditPassed   *bool   `json:"audit_passed"` // nil if no audit yet
	AuditScore    float64 `json:"audit_score"`
	AIProbability float64 `json:"ai_probability"`
}

// AuditIssueStat groups recurrent audit dimension failures.
type AuditIssueStat struct {
	Dimension string `json:"dimension"`
	Count     int    `json:"count"`
}

// ProjectAnalytics is the full payload returned by GetProjectAnalytics.
type ProjectAnalytics struct {
	ProjectID string `json:"project_id"`

	// Overall counters
	TotalChapters    int `json:"total_chapters"`
	ApprovedChapters int `json:"approved_chapters"`
	TotalWords       int `json:"total_words"`

	// Audit pass/fail rates
	AuditedChapters  int     `json:"audited_chapters"`
	AuditPassCount   int     `json:"audit_pass_count"`
	AuditPassRate    float64 `json:"audit_pass_rate"` // 0-1
	AvgAuditScore    float64 `json:"avg_audit_score"`
	AvgAIProbability float64 `json:"avg_ai_probability"`

	// Foreshadowing
	OpenForeshadowings     int `json:"open_foreshadowings"`
	ResolvedForeshadowings int `json:"resolved_foreshadowings"`

	// Resource ledger
	ResourceCount int `json:"resource_count"`

	// Per-chapter breakdown (ordered)
	ChapterStats []ChapterStat `json:"chapter_stats"`

	// Top audit issues across all chapters
	TopIssues []AuditIssueStat `json:"top_issues"`

	// AIGC (AI detection) distribution buckets
	AIGCBuckets map[string]int `json:"aigc_buckets"` // low/medium/high

	// Token usage (summed across all chapters)
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	EstimatedCostUSD  float64 `json:"estimated_cost_usd"` // rough estimate at $0.002/1K tokens
}

func (s *AnalyticsService) GetProjectAnalytics(ctx context.Context, projectID string) (*ProjectAnalytics, error) {
	out := &ProjectAnalytics{ProjectID: projectID}

	// ── Overall chapter counters ──────────────────────────────────────────────
	if err := s.db.QueryRow(ctx,
		`SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'approved'),
			COALESCE(SUM(word_count),0)
		 FROM chapters WHERE project_id = $1`, projectID).
		Scan(&out.TotalChapters, &out.ApprovedChapters, &out.TotalWords); err != nil {
		return nil, fmt.Errorf("chapter counters: %w", err)
	}

	// ── Audit aggregates ──────────────────────────────────────────────────────
	if err := s.db.QueryRow(ctx,
		`SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE passed),
			COALESCE(AVG(overall_score),0),
			COALESCE(AVG(ai_probability),0)
		 FROM audit_reports WHERE project_id = $1`, projectID).
		Scan(&out.AuditedChapters, &out.AuditPassCount, &out.AvgAuditScore, &out.AvgAIProbability); err != nil {
		return nil, fmt.Errorf("audit aggregates: %w", err)
	}
	if out.AuditedChapters > 0 {
		out.AuditPassRate = float64(out.AuditPassCount) / float64(out.AuditedChapters)
	}

	// ── Foreshadowing status ──────────────────────────────────────────────────
	if err := s.db.QueryRow(ctx,
		`SELECT
			COUNT(*) FILTER (WHERE status IN ('planned','planted')),
			COUNT(*) FILTER (WHERE status = 'resolved')
		 FROM foreshadowings WHERE project_id = $1`, projectID).
		Scan(&out.OpenForeshadowings, &out.ResolvedForeshadowings); err != nil {
		return nil, fmt.Errorf("foreshadowing stats: %w", err)
	}

	// ── Resource ledger count ─────────────────────────────────────────────────
	if err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM story_resources WHERE project_id = $1`, projectID).
		Scan(&out.ResourceCount); err != nil {
		return nil, fmt.Errorf("resource count: %w", err)
	}

	// ── Per-chapter stats ─────────────────────────────────────────────────────
	rows, err := s.db.Query(ctx,
		`SELECT c.chapter_num, c.title, c.word_count, c.status,
		        ar.passed, COALESCE(ar.overall_score,0), COALESCE(ar.ai_probability,0)
		 FROM chapters c
		 LEFT JOIN LATERAL (
		     SELECT passed, overall_score, ai_probability
		     FROM audit_reports
		     WHERE chapter_id = c.id
		     ORDER BY created_at DESC LIMIT 1
		 ) ar ON TRUE
		 WHERE c.project_id = $1
		 ORDER BY c.chapter_num`, projectID)
	if err != nil {
		return nil, fmt.Errorf("chapter stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cs ChapterStat
		if err := rows.Scan(&cs.ChapterNum, &cs.Title, &cs.WordCount, &cs.Status,
			&cs.AuditPassed, &cs.AuditScore, &cs.AIProbability); err != nil {
			return nil, err
		}
		out.ChapterStats = append(out.ChapterStats, cs)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// ── Top failing audit dimensions ──────────────────────────────────────────
	// dimensions is JSONB map: {dim_name: {score, passed, issues}}.
	// Count how many times each dimension has passed=false across all reports.
	dimRows, err := s.db.Query(ctx,
		`SELECT key, COUNT(*) as cnt
		 FROM audit_reports ar,
		      jsonb_each(ar.dimensions) AS d(key, val)
		 WHERE ar.project_id = $1
		   AND (val->>'passed')::boolean = FALSE
		 GROUP BY key
		 ORDER BY cnt DESC
		 LIMIT 10`, projectID)
	if err != nil {
		// No audit data or older schema version – not fatal
		s.logger.Warn("top issues query failed", zap.Error(err))
	} else {
		defer dimRows.Close()
		for dimRows.Next() {
			var is AuditIssueStat
			if err := dimRows.Scan(&is.Dimension, &is.Count); err != nil {
				s.logger.Warn("top issues scan failed", zap.Error(err))
				break
			}
			out.TopIssues = append(out.TopIssues, is)
		}
		if err := dimRows.Err(); err != nil {
			s.logger.Warn("top issues rows iteration failed", zap.Error(err))
		}
	}

	// ── AIGC buckets ──────────────────────────────────────────────────────────
	out.AIGCBuckets = map[string]int{"low": 0, "medium": 0, "high": 0}
	bucketRows, err := s.db.Query(ctx,
		`SELECT
		   CASE
		     WHEN ai_probability < 0.33 THEN 'low'
		     WHEN ai_probability < 0.67 THEN 'medium'
		     ELSE 'high'
		   END AS bucket,
		   COUNT(*) AS cnt
		 FROM audit_reports
		 WHERE project_id = $1
		 GROUP BY 1`, projectID)
	if err == nil {
		defer bucketRows.Close()
		for bucketRows.Next() {
			var bucket string
			var cnt int
			if err := bucketRows.Scan(&bucket, &cnt); err != nil {
				s.logger.Warn("aigc bucket scan failed", zap.Error(err))
				break
			}
			out.AIGCBuckets[bucket] = cnt
		}
		if err := bucketRows.Err(); err != nil {
			s.logger.Warn("aigc bucket rows iteration failed", zap.Error(err))
		}
	}

	// ── Token usage aggregates ────────────────────────────────────────────────
	if err := s.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0)
		 FROM chapters WHERE project_id = $1`, projectID).
		Scan(&out.TotalInputTokens, &out.TotalOutputTokens); err != nil {
		s.logger.Warn("token usage query failed", zap.Error(err))
	}
	// Rough cost estimate: $0.002 per 1K tokens (blended average)
	out.EstimatedCostUSD = float64(out.TotalInputTokens+out.TotalOutputTokens) * 0.000002

	return out, nil
}
