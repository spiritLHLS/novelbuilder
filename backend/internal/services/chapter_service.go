package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/workflow"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// ============================================================
// Chapter Service
// ============================================================

type ChapterService struct {
	db          *pgxpool.Pool
	rdb         *redis.Client
	ai          *gateway.AIGateway
	wf          *workflow.Engine
	rag         *RAGService
	originality *OriginalityService
	propagation *EditPropagationService
	glossary    *GlossaryService
	webhook     *WebhookService
	sidecarURL  string
	httpClient  *http.Client
	logger      *zap.Logger
}

var ErrOnlyLatestChapterDeletable = errors.New("only the latest chapter can be deleted")

func NewChapterService(
	db *pgxpool.Pool,
	rdb *redis.Client,
	ai *gateway.AIGateway,
	wf *workflow.Engine,
	rag *RAGService,
	originality *OriginalityService,
	propagation *EditPropagationService,
	glossary *GlossaryService,
	webhook *WebhookService,
	sidecarURL string,
	logger *zap.Logger,
) *ChapterService {
	return &ChapterService{
		db:          db,
		rdb:         rdb,
		ai:          ai,
		wf:          wf,
		rag:         rag,
		originality: originality,
		propagation: propagation,
		glossary:    glossary,
		webhook:     webhook,
		sidecarURL:  sidecarURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		logger:      logger,
	}
}

func (s *ChapterService) PingRedis(ctx context.Context) error {
	return s.rdb.Ping(ctx).Err()
}

func (s *ChapterService) List(ctx context.Context, projectID string) ([]models.Chapter, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, volume_id, chapter_num, title, content, word_count, COALESCE(summary, ''),
		 COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0),
		 COALESCE(genre_compliance_score, 1.0), COALESCE(genre_violations, '[]'),
		 status, version, COALESCE(review_comment, ''), created_at, updated_at
		 FROM chapters WHERE project_id = $1 ORDER BY chapter_num`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list chapters: %w", err)
	}
	defer rows.Close()

	var chapters []models.Chapter
	for rows.Next() {
		var ch models.Chapter
		if err := rows.Scan(&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
			&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
			&ch.GenreComplianceScore, &ch.GenreViolations,
			&ch.Status, &ch.Version, &ch.ReviewComment, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		chapters = append(chapters, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list chapters rows: %w", err)
	}
	return chapters, nil
}

func (s *ChapterService) Get(ctx context.Context, id string) (*models.Chapter, error) {
	var ch models.Chapter
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, volume_id, chapter_num, title, content, word_count, COALESCE(summary, ''),
		 COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0),
		 COALESCE(genre_compliance_score, 1.0), COALESCE(genre_violations, '[]'),
		 status, version, COALESCE(review_comment, ''), created_at, updated_at
		 FROM chapters WHERE id = $1`, id).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
		&ch.GenreComplianceScore, &ch.GenreViolations,
		&ch.Status, &ch.Version, &ch.ReviewComment, &ch.CreatedAt, &ch.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// GetRecentSummaries returns up to `limit` chapter summaries before the given chapter number.
// It only reads summary + chapter_num columns to avoid loading large chapter bodies.
func (s *ChapterService) GetRecentSummaries(ctx context.Context, projectID string, beforeChapterNum, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 3
	}
	rows, err := s.db.Query(ctx,
		`SELECT summary
		 FROM chapters
		 WHERE project_id = $1 AND chapter_num < $2 AND COALESCE(summary, '') <> ''
		 ORDER BY chapter_num DESC
		 LIMIT $3`,
		projectID, beforeChapterNum, limit)
	if err != nil {
		return nil, fmt.Errorf("get recent summaries: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0, limit)
	for rows.Next() {
		var ssum string
		if err := rows.Scan(&ssum); err != nil {
			return nil, err
		}
		if ssum != "" {
			out = append(out, ssum)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Keep chronological order for easier prompt consumption.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// GetByProjectAndNum fetches a chapter by project and chapter number.
func (s *ChapterService) GetByProjectAndNum(ctx context.Context, projectID string, chapterNum int) (*models.Chapter, error) {
	var ch models.Chapter
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, volume_id, chapter_num, title, content, word_count, COALESCE(summary, ''),
		 COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0),
		 COALESCE(genre_compliance_score, 1.0), COALESCE(genre_violations, '[]'),
		 status, version, COALESCE(review_comment, ''), created_at, updated_at
		 FROM chapters WHERE project_id = $1 AND chapter_num = $2`, projectID, chapterNum).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
		&ch.GenreComplianceScore, &ch.GenreViolations,
		&ch.Status, &ch.Version, &ch.ReviewComment, &ch.CreatedAt, &ch.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *ChapterService) Generate(ctx context.Context, projectID string, chapterNum int, req models.GenerateChapterRequest) (*models.Chapter, error) {
	return s.generateChapter(ctx, projectID, chapterNum, req)
}

// MaxChapterNum returns the highest chapter_num for the project (0 if none).
// Used by ContinueGenerate to avoid loading all chapter content just to find the max.
func (s *ChapterService) MaxChapterNum(ctx context.Context, projectID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(chapter_num), 0) FROM chapters WHERE project_id = $1`, projectID).Scan(&n)
	return n, err
}

func (s *ChapterService) Delete(ctx context.Context, id string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete chapter tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var projectID string
	var chapterNum int
	err = tx.QueryRow(ctx,
		`SELECT project_id, chapter_num FROM chapters WHERE id = $1`, id).Scan(&projectID, &chapterNum)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load chapter for delete: %w", err)
	}

	var latestChapterNum int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(chapter_num), 0) FROM chapters WHERE project_id = $1`, projectID).Scan(&latestChapterNum); err != nil {
		return fmt.Errorf("load latest chapter num: %w", err)
	}
	if chapterNum != latestChapterNum {
		return ErrOnlyLatestChapterDeletable
	}

	if _, err := tx.Exec(ctx, `DELETE FROM plot_graph_snapshots WHERE chapter_id = $1`, id); err != nil {
		return fmt.Errorf("delete plot graph snapshots: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM content_dependencies WHERE dependent_type = 'chapter' AND dependent_id = $1`, id); err != nil {
		return fmt.Errorf("delete chapter dependencies: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM patch_items WHERE item_type = 'chapter' AND item_id = $1`, id); err != nil {
		return fmt.Errorf("delete chapter patch items: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chapters WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete chapter: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete chapter: %w", err)
	}

	if s.rdb != nil {
		s.rdb.Del(ctx,
			fmt.Sprintf("chapter_summary:%s:%d", projectID, chapterNum),
			fmt.Sprintf("chapter_content:%s:%d", projectID, chapterNum),
		)
	}

	return nil
}

func (s *ChapterService) CreateSnapshot(ctx context.Context, chapterID, source, note string) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO chapter_snapshots
		    (chapter_id, version, title, content, word_count, summary, quality_report, originality_score, source, note, created_at)
		 SELECT id, version, title, content, word_count, summary, quality_report, originality_score, $2, $3, NOW()
		 FROM chapters WHERE id = $1`,
		chapterID, source, note)
	return err
}

func (s *ChapterService) ListSnapshots(ctx context.Context, chapterID string, limit int) ([]models.ChapterSnapshot, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(ctx,
		`SELECT id, chapter_id, version, title, content, word_count, summary,
		        COALESCE(quality_report, '{}'), originality_score, source, note, created_at
		 FROM chapter_snapshots
		 WHERE chapter_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`, chapterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	defer rows.Close()
	out := make([]models.ChapterSnapshot, 0, limit)
	for rows.Next() {
		var it models.ChapterSnapshot
		if err := rows.Scan(&it.ID, &it.ChapterID, &it.Version, &it.Title, &it.Content,
			&it.WordCount, &it.Summary, &it.QualityReport, &it.OriginalityScore,
			&it.Source, &it.Note, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *ChapterService) RestoreFromSnapshot(ctx context.Context, chapterID, snapshotID string) (*models.Chapter, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var snapshot models.ChapterSnapshot
	err = tx.QueryRow(ctx,
		`SELECT id, chapter_id, version, title, content, word_count, summary,
		        COALESCE(quality_report, '{}'), originality_score, source, note, created_at
		 FROM chapter_snapshots
		 WHERE id = $1 AND chapter_id = $2`, snapshotID, chapterID).Scan(
		&snapshot.ID, &snapshot.ChapterID, &snapshot.Version, &snapshot.Title,
		&snapshot.Content, &snapshot.WordCount, &snapshot.Summary, &snapshot.QualityReport,
		&snapshot.OriginalityScore, &snapshot.Source, &snapshot.Note, &snapshot.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO chapter_snapshots
		    (chapter_id, version, title, content, word_count, summary, quality_report, originality_score, source, note, created_at)
		 SELECT id, version, title, content, word_count, summary, quality_report, originality_score,
		        'before_restore', $2, NOW()
		 FROM chapters WHERE id = $1`,
		chapterID, fmt.Sprintf("restore to snapshot %s", snapshotID)); err != nil {
		return nil, err
	}

	var ch models.Chapter
	err = tx.QueryRow(ctx,
		`UPDATE chapters
		 SET title = $1,
		     content = $2,
		     word_count = $3,
		     summary = $4,
		     quality_report = $5,
		     originality_score = $6,
		     status = 'needs_recheck',
		     version = version + 1,
		     updated_at = NOW()
		 WHERE id = $7
		 RETURNING id, project_id, volume_id, chapter_num, title, content, word_count, COALESCE(summary, ''),
		           COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0),
		           COALESCE(genre_compliance_score, 1.0), COALESCE(genre_violations, '[]'),
		           status, version, COALESCE(review_comment, ''), created_at, updated_at`,
		snapshot.Title, snapshot.Content, snapshot.WordCount, snapshot.Summary,
		snapshot.QualityReport, snapshot.OriginalityScore, chapterID).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
		&ch.GenreComplianceScore, &ch.GenreViolations,
		&ch.Status, &ch.Version, &ch.ReviewComment, &ch.CreatedAt, &ch.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *ChapterService) UpdateContent(ctx context.Context, id, content, status string) (*models.Chapter, error) {
	if strings.TrimSpace(content) == "" {
		return nil, errors.New("content cannot be empty")
	}
	wordCount := utf8.RuneCountInString(content)
	summary := s.generateSummary(ctx, content)
	if status == "" {
		status = "draft"
	}

	var ch models.Chapter
	err := s.db.QueryRow(ctx,
		`UPDATE chapters
		 SET content = $1,
		     word_count = $2,
		     summary = $3,
		     status = $4,
		     version = version + 1,
		     updated_at = NOW()
		 WHERE id = $5
		 RETURNING id, project_id, volume_id, chapter_num, title, content, word_count, COALESCE(summary, ''),
		           COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0),
		           COALESCE(genre_compliance_score, 1.0), COALESCE(genre_violations, '[]'),
		           status, version, COALESCE(review_comment, ''), created_at, updated_at`,
		content, wordCount, summary, status, id).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
		&ch.GenreComplianceScore, &ch.GenreViolations,
		&ch.Status, &ch.Version, &ch.ReviewComment, &ch.CreatedAt, &ch.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func summarizeManualChapterContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	selected := make([]string, 0, 3)
	runeCount := 0
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		selected = append(selected, part)
		runeCount += utf8.RuneCountInString(part)
		if len(selected) >= 3 || runeCount >= 300 {
			break
		}
	}
	if len(selected) == 0 {
		selected = append(selected, trimmed)
	}

	summary := strings.Join(selected, " / ")
	runes := []rune(summary)
	if len(runes) > 300 {
		return string(runes[:300]) + "..."
	}
	return summary
}

func (s *ChapterService) UpdateManualContent(ctx context.Context, id, title, content string, version int) (*models.Chapter, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("content cannot be empty")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	snapshotResult, err := tx.Exec(ctx,
		`INSERT INTO chapter_snapshots
		    (chapter_id, version, title, content, word_count, summary, quality_report, originality_score, source, note, created_at)
		 SELECT id, version, title, content, word_count, summary, quality_report, originality_score,
		        'before_manual_edit', 'before manual chapter edit', NOW()
		 FROM chapters WHERE id = $1 AND version = $2`,
		id, version,
	)
	if err != nil {
		return nil, err
	}
	if snapshotResult.RowsAffected() == 0 {
		return nil, workflow.ErrOptimisticLock
	}

	wordCount := utf8.RuneCountInString(content)
	summary := summarizeManualChapterContent(content)
	title = strings.TrimSpace(title)

	var ch models.Chapter
	err = tx.QueryRow(ctx,
		`UPDATE chapters
		 SET title = CASE WHEN $1 = '' THEN title ELSE $1 END,
		     content = $2,
		     word_count = $3,
		     summary = $4,
		     status = CASE
		         WHEN status IN ('approved', 'pending_review', 'needs_recheck') THEN 'needs_recheck'
		         ELSE 'draft'
		     END,
		     version = version + 1,
		     updated_at = NOW()
		 WHERE id = $5 AND version = $6
		 RETURNING id, project_id, volume_id, chapter_num, title, content, word_count, COALESCE(summary, ''),
		           COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0),
		           COALESCE(genre_compliance_score, 1.0), COALESCE(genre_violations, '[]'),
		           status, version, COALESCE(review_comment, ''), created_at, updated_at`,
		title, content, wordCount, summary, id, version,
	).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
		&ch.GenreComplianceScore, &ch.GenreViolations,
		&ch.Status, &ch.Version, &ch.ReviewComment, &ch.CreatedAt, &ch.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, workflow.ErrOptimisticLock
		}
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *ChapterService) SubmitReview(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE chapters SET status = 'pending_review', updated_at = NOW() WHERE id = $1`, id)
	return err
}

func (s *ChapterService) Approve(ctx context.Context, id, comment string, version int) error {
	result, err := s.db.Exec(ctx,
		`UPDATE chapters SET status = 'approved', review_comment = $1, version = version + 1, updated_at = NOW()
		 WHERE id = $2 AND version = $3`,
		comment, id, version)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return workflow.ErrOptimisticLock
	}
	return nil
}

// AutoApprove approves a chapter unconditionally without optimistic-lock checking.
// Used by background task pipelines (auto-write, batch-generate) in non-strict-review
// mode so subsequent tasks can proceed without human intervention.
func (s *ChapterService) AutoApprove(ctx context.Context, id, comment string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE chapters SET status = 'approved', review_comment = $1, version = version + 1, updated_at = NOW()
		 WHERE id = $2 AND status NOT IN ('rejected')`,
		comment, id)
	return err
}

func (s *ChapterService) Reject(ctx context.Context, id, comment string, version int) error {
	result, err := s.db.Exec(ctx,
		`UPDATE chapters SET status = 'rejected', review_comment = $1, version = version + 1, updated_at = NOW()
		 WHERE id = $2 AND version = $3`,
		comment, id, version)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return workflow.ErrOptimisticLock
	}
	return nil
}

func (s *ChapterService) Regenerate(ctx context.Context, id string, req models.GenerateChapterRequest) (*models.Chapter, error) {
	return s.regenerateChapter(ctx, id, req)
}

// settleChapterState performs post-generation state settlement in a background goroutine.
// It makes a single LLM call to extract character state changes and foreshadowing resolutions
// from the chapter content, then applies all updates in one pgx.Batch to avoid N+1 queries.
