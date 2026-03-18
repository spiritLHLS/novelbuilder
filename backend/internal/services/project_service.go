package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
// Project Service
// ============================================================

type ProjectService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewProjectService(db *pgxpool.Pool, logger *zap.Logger) *ProjectService {
	return &ProjectService{db: db, logger: logger}
}

func (s *ProjectService) List(ctx context.Context) ([]models.Project, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, title, genre, description, status, created_at, updated_at
		 FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(&p.ID, &p.Title, &p.Genre, &p.Description, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func (s *ProjectService) Create(ctx context.Context, req models.CreateProjectRequest) (*models.Project, error) {
	id := uuid.New().String()
	var p models.Project
	err := s.db.QueryRow(ctx,
		`INSERT INTO projects (id, title, genre, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'draft', NOW(), NOW())
		 RETURNING id, title, genre, description, status, created_at, updated_at`,
		id, req.Title, req.Genre, req.Description).Scan(
		&p.ID, &p.Title, &p.Genre, &p.Description, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return &p, nil
}

func (s *ProjectService) Get(ctx context.Context, id string) (*models.Project, error) {
	var p models.Project
	err := s.db.QueryRow(ctx,
		`SELECT id, title, genre, description, status, created_at, updated_at
		 FROM projects WHERE id = $1`, id).Scan(
		&p.ID, &p.Title, &p.Genre, &p.Description, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return &p, nil
}

func (s *ProjectService) Update(ctx context.Context, id string, req models.CreateProjectRequest) (*models.Project, error) {
	var p models.Project
	err := s.db.QueryRow(ctx,
		`UPDATE projects SET title = $1, genre = $2, description = $3, updated_at = NOW()
		 WHERE id = $4
		 RETURNING id, title, genre, description, status, created_at, updated_at`,
		req.Title, req.Genre, req.Description, id).Scan(
		&p.ID, &p.Title, &p.Genre, &p.Description, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return &p, nil
}

func (s *ProjectService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return err
}

// ============================================================
// Blueprint Service
// ============================================================

type BlueprintService struct {
	db     *pgxpool.Pool
	ai     *gateway.AIGateway
	wf     *workflow.Engine
	logger *zap.Logger
}

func NewBlueprintService(db *pgxpool.Pool, ai *gateway.AIGateway, wf *workflow.Engine, logger *zap.Logger) *BlueprintService {
	return &BlueprintService{db: db, ai: ai, wf: wf, logger: logger}
}

func (s *BlueprintService) Generate(ctx context.Context, projectID string, req models.GenerateBlueprintRequest) (*models.BookBlueprint, error) {
	// Get project info
	var project models.Project
	err := s.db.QueryRow(ctx,
		`SELECT id, title, genre, description FROM projects WHERE id = $1`, projectID).Scan(
		&project.ID, &project.Title, &project.Genre, &project.Description)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	genre := req.Genre
	if genre == "" {
		genre = project.Genre
	}
	volumeCount := req.VolumeCount
	if volumeCount == 0 {
		volumeCount = 3
	}
	chaptersPerVolume := req.ChaptersPerVolume
	if chaptersPerVolume == 0 {
		chaptersPerVolume = 30
	}

	// Create workflow run
	runID, err := s.wf.CreateRun(ctx, projectID, false)
	if err != nil {
		return nil, fmt.Errorf("create workflow run: %w", err)
	}

	prompt := fmt.Sprintf(`你是一位资深小说策划编辑。请根据以下信息生成一套完整的整书资产包（Book Blueprint）。

小说标题：%s
类型/流派：%s
核心创意：%s
计划卷数：%d
每卷章节数：%d

请以 JSON 格式返回以下资产：

1. world_bible: 世界观设定（包括世界背景、力量体系、社会结构、地理环境等）
2. characters: 角色列表，每个角色包含 name, role_type(主角/配角/反派), profile(性格、背景、动机、能力)
3. master_outline: 主线大纲，包含每卷的主题、核心冲突、高潮点
4. relation_graph: 角色关系图，描述角色间的关系
5. global_timeline: 全局时间线，重要事件节点
6. foreshadowings: 初始伏笔设置列表
7. volumes: 卷级结构，每卷的标题和章节范围

请确保所有内容逻辑自洽，伏笔安排合理，角色弧线完整。`,
		project.Title, genre, req.Idea, volumeCount, chaptersPerVolume)

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task:     "blueprint_generation",
		Messages: []gateway.ChatMessage{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return nil, fmt.Errorf("AI generation failed: %w", err)
	}

	content := extractJSON(resp.Content)

	// Parse the response
	var blueprint struct {
		WorldBible json.RawMessage `json:"world_bible"`
		Characters []struct {
			Name     string          `json:"name"`
			RoleType string          `json:"role_type"`
			Profile  json.RawMessage `json:"profile"`
		} `json:"characters"`
		MasterOutline  json.RawMessage `json:"master_outline"`
		RelationGraph  json.RawMessage `json:"relation_graph"`
		GlobalTimeline json.RawMessage `json:"global_timeline"`
		Foreshadowings []struct {
			Content     string `json:"content"`
			EmbedMethod string `json:"embed_method"`
			Priority    int    `json:"priority"`
		} `json:"foreshadowings"`
		Volumes []struct {
			Title        string `json:"title"`
			ChapterStart int    `json:"chapter_start"`
			ChapterEnd   int    `json:"chapter_end"`
		} `json:"volumes"`
	}
	if err := json.Unmarshal([]byte(content), &blueprint); err != nil {
		s.logger.Warn("failed to parse blueprint JSON, storing raw", zap.Error(err))
		// Store raw content as master_outline
		rawJSON, _ := json.Marshal(map[string]string{"raw_content": content})
		blueprint.MasterOutline = rawJSON
	}

	// Wrap all DB writes in a single atomic transaction to prevent partial state
	// on storage failure. AI generation runs BEFORE this tx opens
	// so the transaction is short and never held during slow LLM calls.
	tx, txErr := s.db.Begin(ctx)
	if txErr != nil {
		return nil, fmt.Errorf("begin transaction: %w", txErr)
	}
	defer tx.Rollback(ctx)

	// Store world bible
	if blueprint.WorldBible != nil {
		wbID := uuid.New().String()
		tx.Exec(ctx,
			`INSERT INTO world_bibles (id, project_id, content, version, created_at, updated_at)
			 VALUES ($1, $2, $3, 1, NOW(), NOW())
			 ON CONFLICT (project_id) DO UPDATE SET content = $3, version = world_bibles.version + 1, updated_at = NOW()`,
			wbID, projectID, blueprint.WorldBible)
	}

	// Store characters (batch insert)
	if len(blueprint.Characters) > 0 {
		chBatch := &pgx.Batch{}
		for _, ch := range blueprint.Characters {
			profileJSON := ch.Profile
			if profileJSON == nil {
				profileJSON = json.RawMessage(`{}`)
			}
			chBatch.Queue(
				`INSERT INTO characters (id, project_id, name, role_type, profile, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`,
				uuid.New().String(), projectID, ch.Name, ch.RoleType, profileJSON)
		}
		br := tx.SendBatch(ctx, chBatch)
		for range blueprint.Characters {
			br.Exec() //nolint:errcheck
		}
		br.Close()
	}

	// Store foreshadowings (batch insert)
	if len(blueprint.Foreshadowings) > 0 {
		fsBatch := &pgx.Batch{}
		for _, fs := range blueprint.Foreshadowings {
			embedMethod := fs.EmbedMethod
			if embedMethod == "" {
				embedMethod = "implicit"
			}
			priority := fs.Priority
			if priority == 0 {
				priority = 5
			}
			fsBatch.Queue(
				`INSERT INTO foreshadowings (id, project_id, content, embed_method, priority, status, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, 'planted', NOW(), NOW())`,
				uuid.New().String(), projectID, fs.Content, embedMethod, priority)
		}
		br := tx.SendBatch(ctx, fsBatch)
		for range blueprint.Foreshadowings {
			br.Exec() //nolint:errcheck
		}
		br.Close()
	}

	// Create book blueprint record
	bpID := uuid.New().String()
	masterOutline := blueprint.MasterOutline
	if masterOutline == nil {
		masterOutline = json.RawMessage(`{}`)
	}
	relationGraph := blueprint.RelationGraph
	if relationGraph == nil {
		relationGraph = json.RawMessage(`{}`)
	}
	globalTimeline := blueprint.GlobalTimeline
	if globalTimeline == nil {
		globalTimeline = json.RawMessage(`{}`)
	}

	var bp models.BookBlueprint
	err = tx.QueryRow(ctx,
		`INSERT INTO book_blueprints (id, project_id, master_outline, relation_graph, global_timeline, status, version, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, 'draft', 1, NOW(), NOW())
		 RETURNING id, project_id, world_bible_ref, master_outline, relation_graph, global_timeline, status, version, review_comment, created_at, updated_at`,
		bpID, projectID, masterOutline, relationGraph, globalTimeline).Scan(
		&bp.ID, &bp.ProjectID, &bp.WorldBibleRef, &bp.MasterOutline, &bp.RelationGraph,
		&bp.GlobalTimeline, &bp.Status, &bp.Version, &bp.ReviewComment, &bp.CreatedAt, &bp.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("store blueprint: %w", err)
	}

	// Store volumes (batch insert)
	if len(blueprint.Volumes) > 0 {
		volBatch := &pgx.Batch{}
		for i, vol := range blueprint.Volumes {
			title := vol.Title
			if title == "" {
				title = fmt.Sprintf("第%d卷", i+1)
			}
			volBatch.Queue(
				`INSERT INTO volumes (id, project_id, volume_num, title, blueprint_id, chapter_start, chapter_end, status, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, 'draft', NOW(), NOW())`,
				uuid.New().String(), projectID, i+1, title, bpID, vol.ChapterStart, vol.ChapterEnd)
		}
		br := tx.SendBatch(ctx, volBatch)
		for range blueprint.Volumes {
			br.Exec() //nolint:errcheck
		}
		br.Close()
	}

	// Update project status within the transaction for atomicity
	tx.Exec(ctx, `UPDATE projects SET status = 'blueprint_generated', updated_at = NOW() WHERE id = $1`, projectID)

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit blueprint transaction: %w", err)
	}

	// Create workflow step after successful commit (metadata; non-critical if this fails)
	stepID, _ := s.wf.CreateStep(ctx, runID, "blueprint", "blueprint_gate", 0)
	s.wf.MarkStepGenerated(ctx, stepID, bpID)

	s.logger.Info("blueprint generated", zap.String("project_id", projectID), zap.String("blueprint_id", bpID))
	return &bp, nil
}

func (s *BlueprintService) Get(ctx context.Context, projectID string) (*models.BookBlueprint, error) {
	var bp models.BookBlueprint
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, world_bible_ref, master_outline, relation_graph, global_timeline, status, version, review_comment, created_at, updated_at
		 FROM book_blueprints WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&bp.ID, &bp.ProjectID, &bp.WorldBibleRef, &bp.MasterOutline, &bp.RelationGraph,
		&bp.GlobalTimeline, &bp.Status, &bp.Version, &bp.ReviewComment, &bp.CreatedAt, &bp.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &bp, nil
}

func (s *BlueprintService) SubmitReview(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE book_blueprints SET status = 'pending_review', updated_at = NOW() WHERE id = $1`, id)
	return err
}

func (s *BlueprintService) Approve(ctx context.Context, id, comment string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE book_blueprints SET status = 'approved', review_comment = $1, updated_at = NOW() WHERE id = $2`,
		comment, id)
	if err != nil {
		return err
	}

	// Also approve the workflow step
	var projectID string
	s.db.QueryRow(ctx, `SELECT project_id FROM book_blueprints WHERE id = $1`, id).Scan(&projectID)

	var stepID string
	var stepVersion int
	if err := s.db.QueryRow(ctx,
		`SELECT ws.id, ws.version FROM workflow_steps ws
		 JOIN workflow_runs wr ON ws.run_id = wr.id
		 WHERE wr.project_id = $1 AND ws.step_key = 'blueprint' AND ws.status = 'generated'
		 LIMIT 1`, projectID).Scan(&stepID, &stepVersion); err == nil {
		s.wf.TransitStep(ctx, stepID, "approved", stepVersion)
	}

	s.db.Exec(ctx, `UPDATE projects SET status = 'blueprint_approved', updated_at = NOW() WHERE id = $1`, projectID)
	return nil
}

func (s *BlueprintService) Reject(ctx context.Context, id, comment string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE book_blueprints SET status = 'rejected', review_comment = $1, updated_at = NOW() WHERE id = $2`,
		comment, id)
	return err
}

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
	sidecarURL  string
	logger      *zap.Logger
}

func NewChapterService(
	db *pgxpool.Pool,
	rdb *redis.Client,
	ai *gateway.AIGateway,
	wf *workflow.Engine,
	rag *RAGService,
	originality *OriginalityService,
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
		sidecarURL:  sidecarURL,
		logger:      logger,
	}
}

func (s *ChapterService) List(ctx context.Context, projectID string) ([]models.Chapter, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, volume_id, chapter_num, title, content, word_count, summary,
		 gen_params, quality_report, originality_score, status, version, review_comment, created_at, updated_at
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
	return chapters, nil
}

func (s *ChapterService) Get(ctx context.Context, id string) (*models.Chapter, error) {
	var ch models.Chapter
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, volume_id, chapter_num, title, content, word_count, summary,
		 gen_params, quality_report, originality_score, status, version, review_comment, created_at, updated_at
		 FROM chapters WHERE id = $1`, id).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
		&ch.Status, &ch.Version, &ch.ReviewComment, &ch.CreatedAt, &ch.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *ChapterService) Generate(ctx context.Context, projectID string, chapterNum int, req models.GenerateChapterRequest) (*models.Chapter, error) {
	// Enforce workflow gates
	if err := s.wf.CanGenerateNextChapter(ctx, projectID); err != nil {
		return nil, err
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
1. 内容至少2000字
2. 保持与前文的连贯性
3. 按照大纲推进剧情
4. 自然融入伏笔
5. 章节结尾留有悬念

请直接输出章节内容，不要包含任何元数据或标记。`,
		chapterNum,
		req.NarrativeOrder, req.POVCharacter, req.TargetPace,
		req.EndHookType, req.EndHookStrength, req.TensionLevel)

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task: "chapter_generation",
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("AI generation failed: %w", err)
	}

	chapterContent := resp.Content

	// ── Humanizer pipeline (8-step) ───────────────────────────────────────────
	if humanized, hErr := s.humanizeContent(ctx, chapterContent, 0.7); hErr == nil {
		chapterContent = humanized
	} else {
		s.logger.Warn("humanizer skipped", zap.Error(hErr))
	}

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

	// Save chapter
	chID := uuid.New().String()
	genParams, _ := json.Marshal(req)
	var ch models.Chapter
	err = s.db.QueryRow(ctx,
		`INSERT INTO chapters (id, project_id, volume_id, chapter_num, title, content, word_count, summary,
		 gen_params, status, version, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'draft', 1, NOW(), NOW())
		 RETURNING id, project_id, volume_id, chapter_num, title, content, word_count, summary,
		 gen_params, quality_report, originality_score, status, version, review_comment, created_at, updated_at`,
		chID, projectID, volumeID, chapterNum, title, chapterContent, wordCount, summary, genParams).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
		&ch.Status, &ch.Version, &ch.ReviewComment, &ch.CreatedAt, &ch.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("save chapter: %w", err)
	}

	// Store summary in Redis for RecurrentGPT sliding window memory
	s.rdb.Set(ctx, fmt.Sprintf("chapter_summary:%s:%d", projectID, chapterNum), summary, 0)
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

	s.logger.Info("chapter generated",
		zap.String("project_id", projectID),
		zap.Int("chapter_num", chapterNum),
		zap.Int("word_count", wordCount))

	return &ch, nil
}

func (s *ChapterService) StreamGenerate(ctx context.Context, projectID string, chapterNum int, req models.GenerateChapterRequest, handler func(chunk gateway.StreamChunk)) error {
	if err := s.wf.CanGenerateNextChapter(ctx, projectID); err != nil {
		return err
	}

	systemPrompt := s.buildSystemPrompt(ctx, projectID, chapterNum, req)
	userPrompt := fmt.Sprintf(`请生成第 %d 章的完整内容。叙事视角：%s，POV角色：%s，目标节奏：%s，张力水平：%.1f。请直接输出章节内容。`,
		chapterNum, req.NarrativeOrder, req.POVCharacter, req.TargetPace, req.TensionLevel)

	var fullContent strings.Builder
	err := s.ai.ChatStream(ctx, gateway.ChatRequest{
		Task: "chapter_generation",
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}, func(chunk string) error {
		fullContent.WriteString(chunk)
		handler(gateway.StreamChunk{Content: chunk, Done: false})
		return nil
	})
	if err != nil {
		return err
	}

	// Save the completed chapter
	content := fullContent.String()

	// ── Humanizer pipeline ────────────────────────────────────────────────────
	if humanized, hErr := s.humanizeContent(ctx, content, 0.7); hErr == nil {
		content = humanized
	} else {
		s.logger.Warn("humanizer skipped (stream)", zap.Error(hErr))
	}

	wordCount := utf8.RuneCountInString(content)
	summary := s.generateSummary(ctx, content)
	title := s.generateTitle(ctx, content, chapterNum)

	var volumeID *string
	s.db.QueryRow(ctx,
		`SELECT id FROM volumes WHERE project_id = $1 AND chapter_start <= $2 AND chapter_end >= $2`,
		projectID, chapterNum).Scan(&volumeID)

	chID := uuid.New().String()
	genParams, _ := json.Marshal(req)
	s.db.Exec(ctx,
		`INSERT INTO chapters (id, project_id, volume_id, chapter_num, title, content, word_count, summary,
		 gen_params, status, version, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'draft', 1, NOW(), NOW())`,
		chID, projectID, volumeID, chapterNum, title, content, wordCount, summary, genParams)

	s.rdb.Set(ctx, fmt.Sprintf("chapter_summary:%s:%d", projectID, chapterNum), summary, 0)
	s.rdb.Set(ctx, fmt.Sprintf("chapter_content:%s:%d", projectID, chapterNum), content, 24*time.Hour)

	// ── Async originality audit ───────────────────────────────────────────────
	if s.originality != nil {
		go func() {
			auditCtx := context.Background()
			if _, err := s.originality.AuditChapter(auditCtx, chID, projectID, content); err != nil {
				s.logger.Warn("originality audit failed (stream)", zap.Error(err))
			}
		}()
	}

	handler(gateway.StreamChunk{Done: true})
	return nil
}

// MaxChapterNum returns the highest chapter_num for the project (0 if none).
// Used by ContinueGenerate to avoid loading all chapter content just to find the max.
func (s *ChapterService) MaxChapterNum(ctx context.Context, projectID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(chapter_num), 0) FROM chapters WHERE project_id = $1`, projectID).Scan(&n)
	return n, err
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

	// Delete old chapter
	s.db.Exec(ctx, `DELETE FROM chapters WHERE id = $1`, id)

	// Regenerate
	return s.Generate(ctx, projectID, chapterNum, req)
}

// buildSystemPrompt constructs the system prompt using the Lost-in-Middle layout:
// HEAD: world bible + constitution + character states (high attention)
// MIDDLE: previous summaries + foreshadowing status (lower attention)
// TAIL: current outline + tension target + generation params (high attention)
func (s *ChapterService) buildSystemPrompt(ctx context.Context, projectID string, chapterNum int, req models.GenerateChapterRequest) string {
	var sb strings.Builder

	// ===== HEAD: World Bible + Constitution + Character States =====
	sb.WriteString("=== 世界观设定 ===\n")
	var worldContent json.RawMessage
	err := s.db.QueryRow(ctx,
		`SELECT content FROM world_bibles WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&worldContent)
	if err == nil {
		sb.WriteString(string(worldContent))
		sb.WriteString("\n\n")
	}

	// Constitution
	sb.WriteString("=== 世界宪法（不可违反的规则）===\n")
	var immutableRules, mutableRules json.RawMessage
	err = s.db.QueryRow(ctx,
		`SELECT immutable_rules, mutable_rules FROM world_bible_constitutions WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&immutableRules, &mutableRules)
	if err == nil {
		sb.WriteString("不可变规则：")
		sb.WriteString(string(immutableRules))
		sb.WriteString("\n可变规则：")
		sb.WriteString(string(mutableRules))
		sb.WriteString("\n\n")
	}

	// Character states
	sb.WriteString("=== 角色状态 ===\n")
	charRows, _ := s.db.Query(ctx,
		`SELECT name, role_type, profile, current_state FROM characters WHERE project_id = $1`, projectID)
	if charRows != nil {
		for charRows.Next() {
			var name, roleType string
			var profile, state json.RawMessage
			charRows.Scan(&name, &roleType, &profile, &state)
			sb.WriteString(fmt.Sprintf("- %s（%s）: %s\n", name, roleType, string(profile)))
			if state != nil {
				sb.WriteString(fmt.Sprintf("  当前状态：%s\n", string(state)))
			}
		}
		charRows.Close() // close immediately to return connection to pool
	}
	sb.WriteString("\n")

	// ===== MIDDLE: Previous Summaries (RecurrentGPT sliding window) =====
	sb.WriteString("=== 前文摘要（记忆窗口）===\n")
	windowSize := 5
	startChapter := chapterNum - windowSize
	if startChapter < 1 {
		startChapter = 1
	}
	for i := startChapter; i < chapterNum; i++ {
		summaryKey := fmt.Sprintf("chapter_summary:%s:%d", projectID, i)
		summary, err := s.rdb.Get(ctx, summaryKey).Result()
		if err == nil {
			sb.WriteString(fmt.Sprintf("第%d章摘要：%s\n", i, summary))
		}
	}
	sb.WriteString("\n")

	// Foreshadowing status
	sb.WriteString("=== 伏笔状态 ===\n")
	fsRows, _ := s.db.Query(ctx,
		`SELECT content, embed_method, status, priority FROM foreshadowings WHERE project_id = $1 ORDER BY priority DESC`,
		projectID)
	if fsRows != nil {
		for fsRows.Next() {
			var content, embedMethod, status string
			var priority int
			fsRows.Scan(&content, &embedMethod, &status, &priority)
			sb.WriteString(fmt.Sprintf("- [%s] P%d %s（方式：%s）\n", status, priority, content, embedMethod))
		}
		fsRows.Close() // close immediately to return connection to pool
	}
	sb.WriteString("\n")

	// ===== TAIL: Current Outline + Tension Target =====
	sb.WriteString("=== 当前章节大纲 ===\n")
	var outlineContent json.RawMessage
	var tensionTarget float64
	err = s.db.QueryRow(ctx,
		`SELECT content, tension_target FROM outlines
		 WHERE project_id = $1 AND level = 'chapter' AND order_num = $2`,
		projectID, chapterNum).Scan(&outlineContent, &tensionTarget)
	if err == nil {
		sb.WriteString(string(outlineContent))
		sb.WriteString(fmt.Sprintf("\n目标张力值：%.1f\n", tensionTarget))
	}

	// Previous chapter's last paragraph (Re3 dual-track context)
	if chapterNum > 1 {
		prevContentKey := fmt.Sprintf("chapter_content:%s:%d", projectID, chapterNum-1)
		prevContent, err := s.rdb.Get(ctx, prevContentKey).Result()
		if err == nil && len(prevContent) > 0 {
			sb.WriteString("\n=== 上一章结尾 ===\n")
			lines := strings.Split(prevContent, "\n")
			tailLines := lines
			if len(lines) > 5 {
				tailLines = lines[len(lines)-5:]
			}
			sb.WriteString(strings.Join(tailLines, "\n"))
			sb.WriteString("\n")
		}
	}

	// ===== TAIL (continued): RAG sensory / style samples =====
	if s.rag != nil {
		// Query for style samples that match the current chapter outline context
		queryContext := fmt.Sprintf("第%d章 %s %s", chapterNum, req.TargetPace, req.NarrativeOrder)
		samples, err := s.rag.SearchSensory(ctx, projectID, queryContext, "style_samples", 3)
		if err == nil && len(samples) > 0 {
			sb.WriteString("\n=== 风格参考样本（来自参考书目）===\n")
			for i, sample := range samples {
				sb.WriteString(fmt.Sprintf("【样本%d】%s\n", i+1, sample))
			}
			sb.WriteString("\n请模仿以上样本的感官描写风格和句式节奏，但不要照抄内容。\n")
		}
		// Sensory samples for immersive injection
		sensorySamples, err := s.rag.SearchSensory(ctx, projectID, queryContext, "sensory_samples", 2)
		if err == nil && len(sensorySamples) > 0 {
			sb.WriteString("\n=== 感官描写片段参考 ===\n")
			for _, s := range sensorySamples {
				sb.WriteString("- ")
				sb.WriteString(s)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	// ===== Narrative generation params summary =====
	var narrativeSection strings.Builder
	if req.NarrativeOrder != "" {
		narrativeSection.WriteString(fmt.Sprintf("叙事顺序：%s  ", req.NarrativeOrder))
	}
	if req.POVCharacter != "" {
		narrativeSection.WriteString(fmt.Sprintf("主视角角色：%s  ", req.POVCharacter))
		if req.AllowPOVDrift {
			narrativeSection.WriteString("（允许视角漂移）  ")
		}
	}
	if req.TargetPace != "" {
		narrativeSection.WriteString(fmt.Sprintf("目标节奏：%s  ", req.TargetPace))
	}
	if req.EndHookType != "" {
		narrativeSection.WriteString(fmt.Sprintf("结尾钩子：%s（强度 %d）  ", req.EndHookType, req.EndHookStrength))
	}
	if req.TensionLevel > 0 {
		narrativeSection.WriteString(fmt.Sprintf("张力值：%.1f  ", req.TensionLevel))
	}
	if narrativeSection.Len() > 0 {
		sb.WriteString("=== 生成参数 ===\n")
		sb.WriteString(narrativeSection.String())
		sb.WriteString("\n\n")
	}

	sb.WriteString("\n你是一位经验丰富的网络小说作者，请严格遵守世界观设定和宪法规则，保持角色性格一致性。")

	return sb.String()
}

// humanizeContent calls the Python sidecar /humanize endpoint to run the
// 8-step humanization pipeline on the generated text.
// intensity: 0.0–1.0 (0 = no change, 1 = maximum humanization).
func (s *ChapterService) humanizeContent(ctx context.Context, text string, intensity float64) (string, error) {
	if s.sidecarURL == "" {
		return text, nil // no sidecar configured
	}

	body, _ := json.Marshal(map[string]interface{}{
		"text":      text,
		"intensity": intensity,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.sidecarURL+"/humanize", bytes.NewReader(body))
	if err != nil {
		return text, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return text, fmt.Errorf("humanizer unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return text, fmt.Errorf("humanizer returned %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return text, fmt.Errorf("decode humanizer response: %w", err)
	}
	if result.Text == "" {
		return text, nil
	}
	return result.Text, nil
}

func (s *ChapterService) generateSummary(ctx context.Context, content string) string {
	// Truncate for summary generation to avoid token overflow
	truncated := content
	if utf8.RuneCountInString(truncated) > 3000 {
		runes := []rune(truncated)
		truncated = string(runes[:3000])
	}

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task: "summarization",
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: "你是一位文学编辑。请用200字以内概括以下章节的主要情节、角色变化和关键转折点。"},
			{Role: "user", Content: truncated},
		},
		MaxTokens: 500,
	})
	if err != nil {
		s.logger.Warn("summary generation failed", zap.Error(err))
		// Fallback: take the first 200 characters
		runes := []rune(content)
		if len(runes) > 200 {
			return string(runes[:200]) + "..."
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

func extractJSON(text string) string {
	start := strings.Index(text, "{")
	if start == -1 {
		return text
	}
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return text[start:]
}
