package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
		if _, err := tx.Exec(ctx,
			`INSERT INTO world_bibles (id, project_id, content, version, created_at, updated_at)
			 VALUES ($1, $2, $3, 1, NOW(), NOW())
			 ON CONFLICT (project_id) DO UPDATE SET content = $3, version = world_bibles.version + 1, updated_at = NOW()`,
			wbID, projectID, blueprint.WorldBible); err != nil {
			return nil, fmt.Errorf("store world bible: %w", err)
		}
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
		for i := range blueprint.Characters {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return nil, fmt.Errorf("insert character %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return nil, fmt.Errorf("character batch close: %w", err)
		}
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
		for i := range blueprint.Foreshadowings {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return nil, fmt.Errorf("insert foreshadowing %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return nil, fmt.Errorf("foreshadowing batch close: %w", err)
		}
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
		for i := range blueprint.Volumes {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return nil, fmt.Errorf("insert volume %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return nil, fmt.Errorf("volume batch close: %w", err)
		}
	}

	// Update project status within the transaction for atomicity
	if _, err := tx.Exec(ctx, `UPDATE projects SET status = 'blueprint_generated', updated_at = NOW() WHERE id = $1`, projectID); err != nil {
		return nil, fmt.Errorf("update project status: %w", err)
	}

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
