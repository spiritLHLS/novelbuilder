package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/workflow"
	"go.uber.org/zap"
)

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

// Generate creates a placeholder blueprint record immediately (status="generating") and
// launches the actual AI generation in the background. The caller receives 202 and
// should poll GET /projects/:id/blueprint until status changes.
func (s *BlueprintService) Generate(ctx context.Context, projectID string, req models.GenerateBlueprintRequest) (*models.BookBlueprint, error) {
	// Validate that the project exists before creating anything.
	var project models.Project
	err := s.db.QueryRow(ctx,
		`SELECT id, title, genre, description FROM projects WHERE id = $1`, projectID).Scan(
		&project.ID, &project.Title, &project.Genre, &project.Description)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	// Create a workflow run (non-critical; ignore error).
	runID, _ := s.wf.CreateRun(ctx, projectID, false)

	// Insert a placeholder blueprint record immediately so the frontend can track status.
	bpID := uuid.New().String()
	var bp models.BookBlueprint
	err = s.db.QueryRow(ctx,
		`INSERT INTO book_blueprints (id, project_id, master_outline, relation_graph, global_timeline, status, version, created_at, updated_at)
		 VALUES ($1, $2, '{}', '{}', '[]', 'generating', 1, NOW(), NOW())
		 RETURNING id, project_id, world_bible_ref, master_outline, relation_graph, global_timeline, status, version, review_comment, error_message, created_at, updated_at`,
		bpID, projectID).Scan(
		&bp.ID, &bp.ProjectID, &bp.WorldBibleRef, &bp.MasterOutline, &bp.RelationGraph,
		&bp.GlobalTimeline, &bp.Status, &bp.Version, &bp.ReviewComment, &bp.ErrorMessage,
		&bp.CreatedAt, &bp.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create blueprint placeholder: %w", err)
	}

	// Launch background generation with a generous timeout (LLM calls can be slow).
	genCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	go func() {
		defer cancel()
		if genErr := s.doGenerateWork(genCtx, projectID, bpID, runID, project, req); genErr != nil {
			s.logger.Error("blueprint background generation failed",
				zap.String("project_id", projectID),
				zap.String("blueprint_id", bpID),
				zap.Error(genErr))
			s.db.Exec(genCtx,
				`UPDATE book_blueprints SET status='failed', error_message=$1, updated_at=NOW() WHERE id=$2`,
				genErr.Error(), bpID)
		}
	}()

	s.logger.Info("blueprint generation queued",
		zap.String("project_id", projectID),
		zap.String("blueprint_id", bpID))
	return &bp, nil
}

// doGenerateWork performs the actual LLM call and DB writes for blueprint generation.
// On success it updates the placeholder blueprint to status='draft'. On failure the
// caller marks the blueprint as status='failed'.
func (s *BlueprintService) doGenerateWork(ctx context.Context, projectID, bpID, runID string, project models.Project, req models.GenerateBlueprintRequest) error {
	logger := s.logger.With(
		zap.String("project_id", projectID),
		zap.String("blueprint_id", bpID),
	)
	logger.Info("blueprint generation: starting LLM call")

	genre := req.Genre
	if genre == "" {
		genre = project.Genre
	}
	idea := req.Idea
	if idea == "" {
		idea = project.Description
	}
	if idea == "" {
		idea = project.Title
	}
	volumeCount := req.VolumeCount
	if volumeCount == 0 {
		volumeCount = 3
	}
	chaptersPerVolume := req.ChaptersPerVolume
	if chaptersPerVolume == 0 {
		chaptersPerVolume = 30
	}

	prompt := fmt.Sprintf(`你是一位资深小说策划编辑。请根据以下信息生成一套完整的整书资产包（Book Blueprint）。

小说标题：%s
类型/流派：%s
核心创意：%s
计划卷数：%d
每卷章节数：%d

请以 JSON 格式返回以下资产：

1. world_bible: 世界观设定，必须包含以下字段（均为字符串）：
   - world_view: 世界观概述
   - era_background: 时代背景
   - geography: 地理环境
   - social_structure: 社会结构
   - power_system: 力量体系
   - core_conflict: 核心冲突
2. characters: 角色列表，每个角色包含 name, role_type(protagonist/supporting/antagonist/mentor/minor), profile（用一段连续文字描述该角色的性格、背景、动机、能力，100字以内的纯字符串，禁止嵌套JSON）
3. master_outline: 用分号分隔的字符串，每条格式为"第N卷:主题/核心冲突/高潮点"，例如"第1卷:少年崛起/腥风血雨的首战/突破境界"
4. relation_graph: 用分号分隔的角色关系描述字符串，格式为"角色A-角色B:关系描述"，例如"主角-师傅:师徒相授；主角-反派:宿命对手"
5. global_timeline: 用分号分隔的时间线字符串，格式为"时间节点:事件描述"，例如"修炼第1年:入门拜师；修炼第3年:首次大战"
6. foreshadowings: 初始伏笔设置列表
7. volumes: 卷级结构，每卷的标题和章节范围

请确保所有内容逻辑自洽，伏笔安排合理，角色弧线完整。`,
		project.Title, genre, idea, volumeCount, chaptersPerVolume)

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task:     "blueprint_generation",
		Messages: []gateway.ChatMessage{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return fmt.Errorf("AI generation failed: %w", err)
	}
	logger.Info("blueprint generation: LLM call completed, parsing response")

	content := extractJSON(resp.Content)

	var parsed struct {
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
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		logger.Warn("failed to parse blueprint JSON, storing raw response", zap.Error(err))
		rawJSON, _ := json.Marshal(map[string]string{"raw_content": content})
		parsed.MasterOutline = rawJSON
	}

	// All DB writes run inside a single short transaction (LLM call is already done).
	logger.Info("blueprint generation: writing data to database")
	tx, txErr := s.db.Begin(ctx)
	if txErr != nil {
		return fmt.Errorf("begin transaction: %w", txErr)
	}
	defer tx.Rollback(ctx)

	// Store world bible.
	if parsed.WorldBible != nil {
		wbID := uuid.New().String()
		if _, err := tx.Exec(ctx,
			`INSERT INTO world_bibles (id, project_id, content, version, created_at, updated_at)
			 VALUES ($1, $2, $3, 1, NOW(), NOW())
			 ON CONFLICT (project_id) DO UPDATE SET content = $3, version = world_bibles.version + 1, updated_at = NOW()`,
			wbID, projectID, parsed.WorldBible); err != nil {
			return fmt.Errorf("store world bible: %w", err)
		}
	}

	// Store characters (batch upsert).
	if len(parsed.Characters) > 0 {
		chBatch := &pgx.Batch{}
		for _, ch := range parsed.Characters {
			profileJSON := ch.Profile
			if profileJSON == nil {
				profileJSON = json.RawMessage(`{}`)
			}
			chBatch.Queue(
				`INSERT INTO characters (id, project_id, name, role_type, profile, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
				 ON CONFLICT (project_id, name) DO UPDATE
				     SET role_type  = EXCLUDED.role_type,
				         profile    = EXCLUDED.profile,
				         updated_at = NOW()`,
				uuid.New().String(), projectID, ch.Name, ch.RoleType, profileJSON)
		}
		br := tx.SendBatch(ctx, chBatch)
		for i := range parsed.Characters {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("insert character %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("character batch close: %w", err)
		}
		logger.Info("blueprint generation: characters stored", zap.Int("count", len(parsed.Characters)))
	}

	// Store foreshadowings (batch insert).
	if len(parsed.Foreshadowings) > 0 {
		fsBatch := &pgx.Batch{}
		for _, fs := range parsed.Foreshadowings {
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
		for i := range parsed.Foreshadowings {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("insert foreshadowing %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("foreshadowing batch close: %w", err)
		}
	}

	// Update the placeholder blueprint record with the generated content.
	masterOutline := parsed.MasterOutline
	if masterOutline == nil {
		masterOutline = json.RawMessage(`{}`)
	}
	relationGraph := parsed.RelationGraph
	if relationGraph == nil {
		relationGraph = json.RawMessage(`{}`)
	}
	globalTimeline := parsed.GlobalTimeline
	if globalTimeline == nil {
		globalTimeline = json.RawMessage(`[]`)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE book_blueprints
		 SET status = 'draft', master_outline = $1, relation_graph = $2, global_timeline = $3, updated_at = NOW()
		 WHERE id = $4`,
		masterOutline, relationGraph, globalTimeline, bpID); err != nil {
		return fmt.Errorf("update blueprint content: %w", err)
	}

	// Store volumes (batch insert).
	if len(parsed.Volumes) > 0 {
		volBatch := &pgx.Batch{}
		for i, vol := range parsed.Volumes {
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
		for i := range parsed.Volumes {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("insert volume %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("volume batch close: %w", err)
		}
	}

	// Update project status atomically.
	if _, err := tx.Exec(ctx,
		`UPDATE projects SET status = 'blueprint_generated', updated_at = NOW() WHERE id = $1`, projectID); err != nil {
		return fmt.Errorf("update project status: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit blueprint transaction: %w", err)
	}

	// Record the workflow step (non-critical).
	stepID, _ := s.wf.CreateStep(ctx, runID, "blueprint", "blueprint_gate", 0)
	s.wf.MarkStepGenerated(ctx, stepID, bpID)

	logger.Info("blueprint generation: completed successfully",
		zap.Int("characters", len(parsed.Characters)),
		zap.Int("foreshadowings", len(parsed.Foreshadowings)),
		zap.Int("volumes", len(parsed.Volumes)))
	return nil
}

func (s *BlueprintService) Get(ctx context.Context, projectID string) (*models.BookBlueprint, error) {
	var bp models.BookBlueprint
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, world_bible_ref, master_outline, relation_graph, global_timeline, status, version, review_comment, error_message, created_at, updated_at
		 FROM book_blueprints WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&bp.ID, &bp.ProjectID, &bp.WorldBibleRef, &bp.MasterOutline, &bp.RelationGraph,
		&bp.GlobalTimeline, &bp.Status, &bp.Version, &bp.ReviewComment, &bp.ErrorMessage,
		&bp.CreatedAt, &bp.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
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
	if err := s.db.QueryRow(ctx, `SELECT project_id FROM book_blueprints WHERE id = $1`, id).Scan(&projectID); err != nil {
		s.logger.Error("Approve: failed to get project_id", zap.String("blueprint_id", id), zap.Error(err))
		return nil
	}

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
