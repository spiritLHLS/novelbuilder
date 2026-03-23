package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
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
		 COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0), status, version, COALESCE(review_comment, ''), created_at, updated_at
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
		 COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0), status, version, COALESCE(review_comment, ''), created_at, updated_at
		 FROM chapters WHERE id = $1`, id).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
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
		 COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0), status, version, COALESCE(review_comment, ''), created_at, updated_at
		 FROM chapters WHERE project_id = $1 AND chapter_num = $2`, projectID, chapterNum).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
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
	// Idempotency guard: if this chapter already exists (e.g. due to a previous task
	// attempt that succeeded at the DB step but failed to mark the task as done),
	// return the existing chapter without re-generating or calling the AI again.
	if existing, _ := s.GetByProjectAndNum(ctx, projectID, chapterNum); existing != nil {
		s.logger.Info("chapter already exists, skipping generation (idempotent)",
			zap.String("project_id", projectID),
			zap.Int("chapter_num", chapterNum),
			zap.String("chapter_id", existing.ID))
		return existing, nil
	}

	// Enforce workflow gates
	if err := s.wf.CanGenerateNextChapter(ctx, projectID); err != nil {
		return nil, err
	}

	// Resolve word-count range (request > project default > sensible defaults).
	wordsMin := req.ChapterWordsMin
	wordsMax := req.ChapterWordsMax
	if wordsMin <= 0 {
		var pw int
		s.db.QueryRow(ctx, `SELECT chapter_words FROM projects WHERE id = $1`, projectID).Scan(&pw)
		if pw > 0 {
			wordsMin = pw * 2 / 3
			wordsMax = pw * 4 / 3
		} else {
			wordsMin, wordsMax = 2000, 3500
		}
	}
	if wordsMax <= 0 {
		wordsMax = wordsMin * 2
	}
	if wordsMin > wordsMax {
		wordsMin, wordsMax = wordsMax, wordsMin
	}

	// Build system prompt using HEAD/MIDDLE/TAIL (Lost-in-Middle) layout
	systemPrompt := s.buildSystemPrompt(ctx, projectID, chapterNum, req)

	// Build user prompt for chapter generation
	userPrompt := fmt.Sprintf(`请生成第 %d 章的完整内容。

生成参数：
- 叙事视角：%s
- POV角色：%s
- 目标节奏：%s
- 结尾钩子类型：%s
- 结尾钩子强度：%d
- 张力水平：%.1f

要求：
1. 内容字数控制在 %d～%d 字之间（根据本章剧情密度自然调整，无需强行凑字数）
2. 保持与前文的连贯性
3. 按照大纲推进剧情
4. 自然融入伏笔
5. 章节结尾留有悬念`,
		chapterNum,
		req.NarrativeOrder, req.POVCharacter, req.TargetPace,
		req.EndHookType, req.EndHookStrength, req.TensionLevel,
		wordsMin, wordsMax)

	if req.ContextHint != "" {
		userPrompt += fmt.Sprintf("\n\n本章特别方向：%s", req.ContextHint)
	}
	userPrompt += "\n\n请直接输出章节内容，不要包含任何元数据或标记。"

	resp, err := s.ai.ChatWithConfig(ctx, gateway.ChatRequest{
		Task: "chapter_generation",
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}, req.LLMConfig)
	if err != nil {
		return nil, fmt.Errorf("AI generation failed: %w", err)
	}

	chapterContent := resp.Content
	totalInputTokens := resp.InputTokens
	totalOutputTokens := resp.OutputTokens

	// ── Humanizer pipeline (8-step) ───────────────────────────────────────────
	if humanized, hErr := s.humanizeContent(ctx, chapterContent, 0.7); hErr == nil {
		chapterContent = humanized
	} else {
		s.logger.Warn("humanizer skipped", zap.Error(hErr))
	}

	chapterContent, extraIn, extraOut, err := s.ensureChapterWordCount(ctx, systemPrompt, chapterContent, wordsMin, wordsMax, req.LLMConfig)
	if err != nil {
		return nil, err
	}
	totalInputTokens += extraIn
	totalOutputTokens += extraOut

	wordCount := utf8.RuneCountInString(chapterContent)

	// Generate summary
	summary := s.generateSummary(ctx, chapterContent)

	// Generate title
	title := s.generateTitle(ctx, chapterContent, chapterNum)

	// Determine which volume this chapter belongs to
	var volumeID *string
	s.db.QueryRow(ctx,
		`SELECT id FROM volumes WHERE project_id = $1 AND chapter_start <= $2 AND chapter_end >= $2`,
		projectID, chapterNum).Scan(&volumeID)

	// Save chapter (including token usage from AI response).
	// ON CONFLICT DO NOTHING handles the rare race where two workers both pass the
	// idempotency pre-check above and race to insert the same chapter number. The
	// winner inserts; the loser gets ErrNoRows from RETURNING, then falls back to
	// fetching the already-inserted row.
	chID := uuid.New().String()
	genParams, _ := json.Marshal(req)
	var ch models.Chapter
	err = s.db.QueryRow(ctx,
		`INSERT INTO chapters (id, project_id, volume_id, chapter_num, title, content, word_count, summary,
		 gen_params, input_tokens, output_tokens, status, version, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'draft', 1, NOW(), NOW())
		 ON CONFLICT (project_id, chapter_num) DO NOTHING
		 RETURNING id, project_id, volume_id, chapter_num, title, content, word_count, COALESCE(summary, ''),
		 COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0), status, version, COALESCE(review_comment, ''), created_at, updated_at`,
		chID, projectID, volumeID, chapterNum, title, chapterContent, wordCount, summary, genParams,
		totalInputTokens, totalOutputTokens).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
		&ch.Status, &ch.Version, &ch.ReviewComment, &ch.CreatedAt, &ch.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Another worker inserted this chapter in the narrow race window; fetch it.
		existing, qErr := s.GetByProjectAndNum(ctx, projectID, chapterNum)
		if qErr != nil || existing == nil {
			return nil, fmt.Errorf("save chapter: conflict but fetch also failed: %w", qErr)
		}
		s.logger.Info("chapter insert skipped due to concurrent insert, returning existing",
			zap.String("project_id", projectID),
			zap.Int("chapter_num", chapterNum))
		return existing, nil
	}
	if err != nil {
		return nil, fmt.Errorf("save chapter: %w", err)
	}
	_ = s.CreateSnapshot(ctx, ch.ID, "generated", "initial generated snapshot")

	// Store summary in Redis for RecurrentGPT sliding window memory
	s.rdb.Set(ctx, fmt.Sprintf("chapter_summary:%s:%d", projectID, chapterNum), summary, 7*24*time.Hour)
	// Store content in Redis for recent context
	s.rdb.Set(ctx, fmt.Sprintf("chapter_content:%s:%d", projectID, chapterNum), chapterContent, 24*time.Hour)

	// ── Async originality audit ───────────────────────────────────────────────
	if s.originality != nil {
		go func() {
			auditCtx := context.Background()
			if _, err := s.originality.AuditChapter(auditCtx, ch.ID, projectID, chapterContent); err != nil {
				s.logger.Warn("originality audit failed", zap.Error(err))
			}
		}()
	}

	// ── Async dependency recording (for change propagation) ───────────────────
	if s.propagation != nil {
		go s.propagation.RecordChapterDependencies(context.Background(), projectID, ch.ID)
	}

	// ── Async webhook notification ────────────────────────────────────────────
	if s.webhook != nil {
		go s.webhook.Fire(context.Background(), projectID, "chapter_generated", map[string]any{
			"chapter_id":  ch.ID,
			"chapter_num": ch.ChapterNum,
			"word_count":  ch.WordCount,
		})
	}

	// ── Async post-generation state settlement ────────────────────────────────
	go func(pid, cid, content string) {
		sCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.settleChapterState(sCtx, pid, cid, content)
	}(projectID, ch.ID, chapterContent)

	s.logger.Info("chapter generated",
		zap.String("project_id", projectID),
		zap.Int("chapter_num", chapterNum),
		zap.Int("word_count", wordCount))

	return &ch, nil
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
		           COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0), status, version, COALESCE(review_comment, ''), created_at, updated_at`,
		snapshot.Title, snapshot.Content, snapshot.WordCount, snapshot.Summary,
		snapshot.QualityReport, snapshot.OriginalityScore, chapterID).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
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
		           COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0), status, version, COALESCE(review_comment, ''), created_at, updated_at`,
		content, wordCount, summary, status, id).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
		&ch.Status, &ch.Version, &ch.ReviewComment, &ch.CreatedAt, &ch.UpdatedAt)
	if err != nil {
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
	var projectID string
	var chapterNum int
	err := s.db.QueryRow(ctx,
		`SELECT project_id, chapter_num FROM chapters WHERE id = $1`, id).Scan(&projectID, &chapterNum)
	if err != nil {
		return nil, fmt.Errorf("chapter not found: %w", err)
	}
	_ = s.CreateSnapshot(ctx, id, "before_regenerate", "before chapter regenerate")

	// Resolve word-count range (request > project default > sensible defaults).
	wordsMin := req.ChapterWordsMin
	wordsMax := req.ChapterWordsMax
	if wordsMin <= 0 {
		var pw int
		s.db.QueryRow(ctx, `SELECT chapter_words FROM projects WHERE id = $1`, projectID).Scan(&pw)
		if pw > 0 {
			wordsMin = pw * 2 / 3
			wordsMax = pw * 4 / 3
		} else {
			wordsMin, wordsMax = 2000, 3500
		}
	}
	if wordsMax <= 0 {
		wordsMax = wordsMin * 2
	}
	if wordsMin > wordsMax {
		wordsMin, wordsMax = wordsMax, wordsMin
	}

	systemPrompt := s.buildSystemPrompt(ctx, projectID, chapterNum, req)
	userPrompt := fmt.Sprintf(`请生成第 %d 章的完整内容。

生成参数：
- 叙事视角：%s
- POV角色：%s
- 目标节奏：%s
- 结尾钩子类型：%s
- 结尾钩子强度：%d
- 张力水平：%.1f

要求：
1. 内容字数控制在 %d～%d 字之间（根据本章剧情密度自然调整，无需强行凑字数）
2. 保持与前文的连贯性
3. 按照大纲推进剧情
4. 自然融入伏笔
5. 章节结尾留有悬念`,
		chapterNum,
		req.NarrativeOrder, req.POVCharacter, req.TargetPace,
		req.EndHookType, req.EndHookStrength, req.TensionLevel,
		wordsMin, wordsMax)
	if req.ContextHint != "" {
		userPrompt += fmt.Sprintf("\n\n本章特别方向：%s", req.ContextHint)
	}
	userPrompt += "\n\n请直接输出章节内容，不要包含任何元数据或标记。"

	resp, err := s.ai.ChatWithConfig(ctx, gateway.ChatRequest{
		Task: "chapter_regeneration",
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}, req.LLMConfig)
	if err != nil {
		return nil, fmt.Errorf("AI regeneration failed: %w", err)
	}

	chapterContent := resp.Content
	totalInputTokens := resp.InputTokens
	totalOutputTokens := resp.OutputTokens
	if humanized, hErr := s.humanizeContent(ctx, chapterContent, 0.7); hErr == nil {
		chapterContent = humanized
	}
	chapterContent, extraIn, extraOut, err := s.ensureChapterWordCount(ctx, systemPrompt, chapterContent, wordsMin, wordsMax, req.LLMConfig)
	if err != nil {
		return nil, err
	}
	totalInputTokens += extraIn
	totalOutputTokens += extraOut

	// Update token counts alongside content
	s.db.Exec(ctx,
		`UPDATE chapters SET input_tokens = input_tokens + $1, output_tokens = output_tokens + $2 WHERE id = $3`,
		totalInputTokens, totalOutputTokens, id)

	updated, err := s.UpdateContent(ctx, id, chapterContent, "draft")
	if err != nil {
		return nil, fmt.Errorf("update regenerated chapter: %w", err)
	}
	_ = s.CreateSnapshot(ctx, id, "regenerated", "after chapter regenerate")

	if s.originality != nil {
		go func(chID, pid, content string) {
			if _, err := s.originality.AuditChapter(context.Background(), chID, pid, content); err != nil {
				s.logger.Warn("originality audit failed (regenerate)", zap.Error(err))
			}
		}(updated.ID, updated.ProjectID, updated.Content)
	}

	// ── Async post-generation state settlement ────────────────────────────────
	go func(pid, cid, content string) {
		sCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.settleChapterState(sCtx, pid, cid, content)
	}(projectID, updated.ID, updated.Content)

	return updated, nil
}

func (s *ChapterService) ensureChapterWordCount(
	ctx context.Context,
	systemPrompt, content string,
	wordsMin, wordsMax int,
	llmConfig map[string]interface{},
) (string, int, int, error) {
	if wordsMin <= 0 || wordsMax <= 0 {
		return content, 0, 0, nil
	}
	current := utf8.RuneCountInString(content)
	if current >= wordsMin && current <= wordsMax {
		return content, 0, 0, nil
	}

	adjusted := content
	totalInputTokens := 0
	totalOutputTokens := 0

	for attempt := 0; attempt < 2; attempt++ {
		current = utf8.RuneCountInString(adjusted)
		if current >= wordsMin && current <= wordsMax {
			return adjusted, totalInputTokens, totalOutputTokens, nil
		}

		action := "压缩"
		if current < wordsMin {
			action = "扩写"
		}

		resp, err := s.ai.ChatWithConfig(ctx, gateway.ChatRequest{
			Task: "chapter_length_adjustment",
			Messages: []gateway.ChatMessage{
				{Role: "system", Content: systemPrompt + "\n\n补充规则：字数范围是硬约束，如果正文超出范围必须压缩，如果不足范围必须扩写。不得输出解释，只输出修订后的完整正文。"},
				{Role: "user", Content: fmt.Sprintf("当前正文约 %d 字，目标区间是 %d-%d 字。请对下面正文做%s，使最终长度严格落在目标区间内，同时保留本章核心剧情、人物关系和结尾功能。只输出修订后的完整正文。\n\n%s", current, wordsMin, wordsMax, action, adjusted)},
			},
		}, llmConfig)
		if err != nil {
			return content, totalInputTokens, totalOutputTokens, fmt.Errorf("adjust chapter length: %w", err)
		}

		adjusted = resp.Content
		totalInputTokens += resp.InputTokens
		totalOutputTokens += resp.OutputTokens
	}

	current = utf8.RuneCountInString(adjusted)
	if current < wordsMin || current > wordsMax {
		return content, totalInputTokens, totalOutputTokens,
			fmt.Errorf("chapter length %d out of range %d-%d after adjustment", current, wordsMin, wordsMax)
	}

	return adjusted, totalInputTokens, totalOutputTokens, nil
}

// settleChapterState performs post-generation state settlement in a background goroutine.
// It makes a single LLM call to extract character state changes and foreshadowing resolutions
// from the chapter content, then applies all updates in one pgx.Batch to avoid N+1 queries.
