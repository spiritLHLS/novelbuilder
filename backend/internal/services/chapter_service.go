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
		wordsMax = 3500 // hard default cap
	}
	if wordsMin > wordsMax {
		wordsMin, wordsMax = wordsMax, wordsMin
	}
	// Enforce absolute ceiling: never allow max to exceed 3500 unless explicitly configured
	if wordsMax > 3500 && req.ChapterWordsMax <= 0 {
		wordsMax = 3500
	}

	// Pass resolved word-count limits into req so buildSystemPrompt can embed them as anchor constraints
	req.ChapterWordsMin = wordsMin
	req.ChapterWordsMax = wordsMax

	// Build system prompt using HEAD/MIDDLE/TAIL (Lost-in-Middle) layout
	systemPrompt := s.buildSystemPrompt(ctx, projectID, chapterNum, req)

	// Build user prompt for chapter generation
	// First, extract the outline events for this chapter to embed in user prompt
	// First, extract the outline events for this chapter to embed in user prompt
	var outlineEventsForPrompt string
	var outlineJSON json.RawMessage
	if s.db.QueryRow(ctx,
		`SELECT content FROM outlines WHERE project_id = $1 AND level = 'chapter' AND order_num = $2`,
		projectID, chapterNum).Scan(&outlineJSON) == nil {
		var outlineData map[string]interface{}
		if json.Unmarshal(outlineJSON, &outlineData) == nil {
			if events, ok := outlineData["events"].([]interface{}); ok && len(events) > 0 {
				var eventLines []string
				// Hard cap at 3 events
				maxEvents := 3
				if len(events) < maxEvents {
					maxEvents = len(events)
				}
				for i := 0; i < maxEvents; i++ {
					if es, ok := events[i].(string); ok {
						eventLines = append(eventLines, fmt.Sprintf("  %d. %s", i+1, es))
					}
				}
				outlineEventsForPrompt = strings.Join(eventLines, "\n")
			}
		}
	}

	userPrompt := fmt.Sprintf(`请生成第 %d 章的完整内容。

生成参数：
- 叙事视角：%s
- POV角色：%s
- 目标节奏：%s
- 结尾钩子类型：%s
- 结尾钩子强度：%d
- 张力水平：%.1f

⚠️ 【本章必须完成的大纲事件 — 不可遗漏、不可自行添加】
%s
以上事件是本章的全部剧情内容，正文必须逐一展开上述每个事件对应的场景，禁止自行发明大纲以外的新事件、新冲突、新角色登场。

硬性要求：
1. ⚠️ 字数硬上限：正文不得超过 %d 字。当写作接近 %d 字时立即执行断章收尾；字数不足 %d 字时通过对话和动作细节充实。超出上限是严重错误。
2. 本章只展开上方大纲列出的事件（最多3件），事件展开优先用**对话和动作**推进（不要大段景物描写）
3. 从上一章结尾处自然承接，禁止复述前文，延续前一章的语言风格和叙述节奏
4. 严格锁定指定POV视角，不写该角色感知不到的信息
5. **强制断章要求**：章节必须在场景动作、对话或悬念的高点处戛然而止
   - 禁止任何收尾：不写总结段、展望段、升华段、情绪收束段
   - 禁止预告句式：【他知道XXX】【未来XXX】【更大的XXX即将到来】
   - 最后一段不超过2句话，必须是未完成的动作/对话/悬念
   - 参考网文断章：在读者最想知道下文时立即断开
6. 遵守系统提示中的全部反AI文风规则（禁用微微/缓缓/淡淡等）
7. **角色能力严格遵循时间线**：
   - 角色只能使用系统提示【角色状态】中已明确记载的能力/装备/身份
   - 禁止给角色突然出现未记录的新能力、新武器、新师承、新身份
   - 如本章大纲要求角色获得某项资源，必须写完整获得过程（至少200字场景）
   - 一章最多1次实力提升，禁止连续突破或批量获得资源
8. **角色和道具出场约束**：
   - 正文中出现的有名有姓的角色必须来自系统提示中的【角色状态】列表或本章大纲事件
   - 任何武器/法宝/道具首次出场必须有明确来源（大纲事件获得/战利品/NPC赠予/购买/祖传）
   - 禁止凭空出现没有来源的角色或道具`,
		chapterNum,
		req.NarrativeOrder, req.POVCharacter, req.TargetPace,
		req.EndHookType, req.EndHookStrength, req.TensionLevel,
		outlineEventsForPrompt,
		wordsMax, wordsMax, wordsMin)

	if req.ContextHint != "" {
		userPrompt += fmt.Sprintf("\n\n本章特别方向：%s", req.ContextHint)
	}
	userPrompt += "\n\n请直接输出章节正文，不要包含章节号、标题、元数据或任何标记。"

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
		 COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0),
		 COALESCE(genre_compliance_score, 1.0), COALESCE(genre_violations, '[]'),
		 status, version, COALESCE(review_comment, ''), created_at, updated_at`,
		chID, projectID, volumeID, chapterNum, title, chapterContent, wordCount, summary, genParams,
		totalInputTokens, totalOutputTokens).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
		&ch.GenreComplianceScore, &ch.GenreViolations,
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

	// ── Async chapter similarity check (anti-repetition logging) ──────────────
	go func(pid, cid, content string, cNum int) {
		sCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.logChapterSimilarity(sCtx, pid, cid, content, cNum)
	}(projectID, ch.ID, chapterContent, chapterNum)

	// ── Async Qdrant memory upsert (store chapter summary for future retrieval) ──
	if s.rag != nil {
		go func(pid string, cNum int, sum string) {
			rCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = s.rag.StoreEmbedding(rCtx, pid, "chapter_summaries", sum, "chapter", fmt.Sprintf("ch_%d", cNum), map[string]interface{}{
				"chapter_num": cNum,
			})
		}(projectID, chapterNum, summary)
	}

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
		wordsMax = 3500 // hard default cap
	}
	if wordsMin > wordsMax {
		wordsMin, wordsMax = wordsMax, wordsMin
	}
	// Enforce absolute ceiling: never allow max to exceed 3500 unless explicitly configured
	if wordsMax > 3500 && req.ChapterWordsMax <= 0 {
		wordsMax = 3500
	}

	// Pass resolved word-count limits into req so buildSystemPrompt can embed them as anchor constraints
	req.ChapterWordsMin = wordsMin
	req.ChapterWordsMax = wordsMax

	systemPrompt := s.buildSystemPrompt(ctx, projectID, chapterNum, req)

	// Extract outline events for reinforcement in user prompt
	var regenOutlineEventsForPrompt string
	var regenOutlineJSON json.RawMessage
	if s.db.QueryRow(ctx,
		`SELECT content FROM outlines WHERE project_id = $1 AND level = 'chapter' AND order_num = $2`,
		projectID, chapterNum).Scan(&regenOutlineJSON) == nil {
		var outlineData map[string]interface{}
		if json.Unmarshal(regenOutlineJSON, &outlineData) == nil {
			if events, ok := outlineData["events"].([]interface{}); ok && len(events) > 0 {
				var eventLines []string
				maxEvents := 3
				if len(events) < maxEvents {
					maxEvents = len(events)
				}
				for i := 0; i < maxEvents; i++ {
					if es, ok := events[i].(string); ok {
						eventLines = append(eventLines, fmt.Sprintf("  %d. %s", i+1, es))
					}
				}
				regenOutlineEventsForPrompt = strings.Join(eventLines, "\n")
			}
		}
	}

	userPrompt := fmt.Sprintf(`请生成第 %d 章的完整内容。

生成参数：
- 叙事视角：%s
- POV角色：%s
- 目标节奏：%s
- 结尾钩子类型：%s
- 结尾钩子强度：%d
- 张力水平：%.1f

⚠️ 【本章必须完成的大纲事件 — 不可遗漏、不可自行添加】
%s
以上事件是本章的全部剧情内容，正文必须逐一展开上述每个事件对应的场景，禁止自行发明大纲以外的新事件、新冲突、新角色登场。

硬性要求：
1. ⚠️ 字数硬上限：正文不得超过 %d 字。当写作接近 %d 字时立即执行断章收尾；字数不足 %d 字时通过对话和动作细节充实。超出上限是严重错误。
2. 本章只展开上方大纲列出的事件（最多3件），事件展开优先用**对话和动作**推进（不要大段景物描写）
3. 从上一章结尾处自然承接，禁止复述前文，延续前一章的语言风格和叙述节奏
4. 严格锁定指定POV视角，不写该角色感知不到的信息
5. **强制断章要求**：章节必须在场景动作、对话或悬念的高点处戛然而止
   - 禁止任何收尾：不写总结段、展望段、升华段、情绪收束段
   - 禁止预告句式：【他知道XXX】【未来XXX】【更大的XXX即将到来】
   - 最后一段不超过2句话，必须是未完成的动作/对话/悬念
   - 参考网文断章：在读者最想知道下文时立即断开
6. 遵守系统提示中的全部反AI文风规则（禁用微微/缓缓/淡淡等）
7. **角色能力严格遵循时间线**：
   - 角色只能使用系统提示【角色状态】中已明确记载的能力/装备/身份
   - 禁止给角色突然出现未记录的新能力、新武器、新师承、新身份
   - 如本章大纲要求角色获得某项资源，必须写完整获得过程（至少200字场景）
   - 一章最多1次实力提升，禁止连续突破或批量获得资源
8. **角色和道具出场约束**：
   - 正文中出现的有名有姓的角色必须来自系统提示中的【角色状态】列表或本章大纲事件
   - 任何武器/法宝/道具首次出场必须有明确来源（大纲事件获得/战利品/NPC赠予/购买/祖传）
   - 禁止凭空出现没有来源的角色或道具`,
		chapterNum,
		req.NarrativeOrder, req.POVCharacter, req.TargetPace,
		req.EndHookType, req.EndHookStrength, req.TensionLevel,
		regenOutlineEventsForPrompt,
		wordsMax, wordsMax, wordsMin)
	if req.ContextHint != "" {
		userPrompt += fmt.Sprintf("\n\n本章特别方向：%s", req.ContextHint)
	}
	userPrompt += "\n\n请直接输出章节正文，不要包含章节号、标题、元数据或任何标记。"

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

		// If over the max, do NOT compress — the prompt must be strong enough to prevent over-generation.
		// Truncating post-hoc breaks narrative flow and is explicitly unwanted.
		if current > wordsMax {
			s.logger.Warn("chapter over word limit, returning as-is (no post-hoc compression)",
				zap.Int("length", current),
				zap.Int("max", wordsMax))
			return adjusted, totalInputTokens, totalOutputTokens, nil
		}

		action := "扩写" // only expand if under min

		resp, err := s.ai.ChatWithConfig(ctx, gateway.ChatRequest{
			Task: "chapter_length_adjustment",
			Messages: []gateway.ChatMessage{
				{Role: "system", Content: systemPrompt + "\n\n补充规则：字数范围是硬约束，如果正文超出范围必须压缩，如果不足范围必须扩写。压缩时优先删减重复的心理描写、冗余环境描写、多余的过渡句；扩写时优先补充场景细节、角色微表情和感官描写，不要添加新事件。结尾不得添加总结/展望段。不得输出解释，只输出修订后的完整正文。"},
				{Role: "user", Content: fmt.Sprintf("当前正文约 %d 字，目标区间是 %d-%d 字。请对下面正文做%s，使最终长度严格落在目标区间内，同时保留本章核心剧情、人物关系和结尾功能。保持反AI文风规则（禁止使用微微缓缓淡淡等AI高频词）。只输出修订后的完整正文。\n\n%s", current, wordsMin, wordsMax, action, adjusted)},
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
		// Best-effort: return the adjusted content (closer to target than original)
		// rather than failing the task. The pipeline continues; a warning is logged
		// so the overshoot is visible without blocking automated runs.
		s.logger.Warn("chapter length still out of range after adjustment, using best-effort content",
			zap.Int("length", current),
			zap.Int("min", wordsMin),
			zap.Int("max", wordsMax),
		)
		return adjusted, totalInputTokens, totalOutputTokens, nil
	}

	return adjusted, totalInputTokens, totalOutputTokens, nil
}

// logChapterSimilarity compares the new chapter content with recent chapters
// and logs similarity scores to chapter_similarity_log for repetition tracking.
// Uses character-level 4-gram Jaccard similarity (fast, no external deps).
func (s *ChapterService) logChapterSimilarity(ctx context.Context, projectID, chapterID, content string, chapterNum int) {
	if chapterNum <= 1 {
		return
	}
	// Fetch last 10 chapters' content snippets in a single query (no N+1)
	rows, err := s.db.Query(ctx,
		`SELECT id, chapter_num, content
		 FROM chapters
		 WHERE project_id = $1 AND chapter_num < $2 AND chapter_num >= $3
		 ORDER BY chapter_num DESC`,
		projectID, chapterNum, max(1, chapterNum-10))
	if err != nil {
		s.logger.Debug("similarity check: failed to load recent chapters", zap.Error(err))
		return
	}
	defer rows.Close()

	newGrams := buildNgramSet(content, 4)
	if len(newGrams) == 0 {
		return
	}

	batch := &pgx.Batch{}
	for rows.Next() {
		var prevID string
		var prevNum int
		var prevContent string
		if rows.Scan(&prevID, &prevNum, &prevContent) != nil {
			continue
		}
		prevGrams := buildNgramSet(prevContent, 4)
		if len(prevGrams) == 0 {
			continue
		}
		sim := jaccardSimilarity(newGrams, prevGrams)
		batch.Queue(
			`INSERT INTO chapter_similarity_log (project_id, chapter_a_id, chapter_b_id, similarity_score, detection_method, created_at)
			 VALUES ($1, $2, $3, $4, 'ngram_jaccard', NOW())
			 ON CONFLICT DO NOTHING`,
			projectID, chapterID, prevID, sim)
		if sim > 0.4 {
			s.logger.Warn("high chapter similarity detected",
				zap.String("project_id", projectID),
				zap.Int("new_chapter", chapterNum),
				zap.Int("prev_chapter", prevNum),
				zap.Float64("similarity", sim))
		}
	}
	if batch.Len() > 0 {
		br := s.db.SendBatch(ctx, batch)
		defer br.Close()
		for i := 0; i < batch.Len(); i++ {
			br.Exec()
		}
	}
}

func buildNgramSet(text string, n int) map[string]struct{} {
	runes := []rune(text)
	// Remove whitespace/newlines
	filtered := make([]rune, 0, len(runes))
	for _, r := range runes {
		if r != '\n' && r != '\r' && r != ' ' && r != '\t' {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) < n {
		return nil
	}
	set := make(map[string]struct{}, len(filtered)-n+1)
	for i := 0; i <= len(filtered)-n; i++ {
		gram := string(filtered[i : i+n])
		set[gram] = struct{}{}
	}
	return set
}

func jaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// settleChapterState performs post-generation state settlement in a background goroutine.
// It makes a single LLM call to extract character state changes and foreshadowing resolutions
// from the chapter content, then applies all updates in one pgx.Batch to avoid N+1 queries.
