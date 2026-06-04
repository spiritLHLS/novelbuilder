package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/database"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/workflow"
	"go.uber.org/zap"
)

// ============================================================
// Blueprint Service
// ============================================================

type BlueprintService struct {
	db             *database.DB
	ai             *gateway.AIGateway
	wf             *workflow.Engine
	worldBibles    *WorldBibleService
	characters     *CharacterService
	foreshadowings *ForeshadowingService
	glossary       *GlossaryService
	outlines       *OutlineService
	references     *ReferenceService
	genreTemplates *GenreTemplateService
	logger         *zap.Logger
}

func NewBlueprintService(
	db *database.DB,
	ai *gateway.AIGateway,
	wf *workflow.Engine,
	worldBibles *WorldBibleService,
	characters *CharacterService,
	foreshadowings *ForeshadowingService,
	glossary *GlossaryService,
	outlines *OutlineService,
	references *ReferenceService,
	genreTemplates *GenreTemplateService,
	logger *zap.Logger,
) *BlueprintService {
	return &BlueprintService{
		db:             db,
		ai:             ai,
		wf:             wf,
		worldBibles:    worldBibles,
		characters:     characters,
		foreshadowings: foreshadowings,
		glossary:       glossary,
		outlines:       outlines,
		references:     references,
		genreTemplates: genreTemplates,
		logger:         logger,
	}
}

// PrepareGenerate creates a placeholder blueprint record immediately
// (status="generating"). The actual AI work is performed by a tracked task via
// RunGenerateTask so project/task pause, resume, cancel and retry all use the
// same state machine.
func (s *BlueprintService) PrepareGenerate(ctx context.Context, projectID string, req models.GenerateBlueprintRequest) (*models.BookBlueprint, string, error) {
	// Validate that the project exists before creating anything.
	var project models.Project
	err := s.db.QueryRow(ctx,
		`SELECT id, title, genre, description, style_description, COALESCE(language, 'zh-CN'), target_words, chapter_words,
		        COALESCE(project_type,'original'), continuation_ref_id, COALESCE(continuation_start_chapter,1)
		 FROM projects WHERE id = $1`, projectID).Scan(
		&project.ID, &project.Title, &project.Genre, &project.Description, &project.StyleDescription,
		&project.Language,
		&project.TargetWords, &project.ChapterWords,
		&project.ProjectType, &project.ContinuationRefID, &project.ContinuationStartChapter)
	if err != nil {
		return nil, "", fmt.Errorf("project not found: %w", err)
	}

	// Create a workflow run (non-critical; ignore error).
	runID, _ := s.wf.CreateRun(ctx, projectID, false)

	// Insert a placeholder blueprint record immediately so the frontend can track status.
	bpID := uuid.New().String()
	var bp models.BookBlueprint
	err = s.db.QueryRow(ctx,
		`INSERT INTO book_blueprints (id, project_id, master_outline, relation_graph, global_timeline, status, version, created_at, updated_at)
		 VALUES ($1, $2, '{}', '{}', '[]', 'generating', 1, NOW(), NOW())
		 ON CONFLICT (project_id) DO UPDATE SET
		     status = 'generating',
		     error_message = NULL,
		     updated_at = NOW()
		 RETURNING id, project_id, world_bible_ref, master_outline, relation_graph, global_timeline, status, version, review_comment, error_message, created_at, updated_at`,
		bpID, projectID).Scan(
		&bp.ID, &bp.ProjectID, &bp.WorldBibleRef,
		rawJSONScanner{dst: &bp.MasterOutline},
		rawJSONScanner{dst: &bp.RelationGraph},
		rawJSONScanner{dst: &bp.GlobalTimeline},
		&bp.Status, &bp.Version, &bp.ReviewComment, &bp.ErrorMessage,
		&bp.CreatedAt, &bp.UpdatedAt)
	if err != nil {
		return nil, "", fmt.Errorf("create blueprint placeholder: %w", err)
	}
	// On conflict, RETURNING gives us the existing row's id, not the new bpID.
	bpID = bp.ID

	s.logger.Info("blueprint generation queued",
		zap.String("project_id", projectID),
		zap.String("blueprint_id", bpID))
	return &bp, runID, nil
}

// Generate is kept as a synchronous helper for tests and internal callers.
func (s *BlueprintService) Generate(ctx context.Context, projectID string, req models.GenerateBlueprintRequest) (*models.BookBlueprint, error) {
	bp, runID, err := s.PrepareGenerate(ctx, projectID, req)
	if err != nil {
		return nil, err
	}
	if err := s.RunGenerateTask(ctx, projectID, bp.ID, runID, req); err != nil {
		return bp, err
	}
	return s.Get(ctx, projectID)
}

func (s *BlueprintService) RunGenerateTask(ctx context.Context, projectID, bpID, runID string, req models.GenerateBlueprintRequest) error {
	var project models.Project
	err := s.db.QueryRow(ctx,
		`SELECT id, title, genre, description, style_description, COALESCE(language, 'zh-CN'), target_words, chapter_words,
		        COALESCE(project_type,'original'), continuation_ref_id, COALESCE(continuation_start_chapter,1)
		 FROM projects WHERE id = $1`, projectID).Scan(
		&project.ID, &project.Title, &project.Genre, &project.Description, &project.StyleDescription,
		&project.Language,
		&project.TargetWords, &project.ChapterWords,
		&project.ProjectType, &project.ContinuationRefID, &project.ContinuationStartChapter)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}
	if _, err := s.db.Exec(ctx,
		`UPDATE book_blueprints SET status='generating', error_message=NULL, updated_at=NOW() WHERE id=$1`,
		bpID); err != nil {
		return fmt.Errorf("mark blueprint generating: %w", err)
	}
	if err := s.doGenerateWork(ctx, projectID, bpID, runID, project, req); err != nil {
		if ctx.Err() != nil {
			return err
		}
		s.logger.Error("blueprint generation failed",
			zap.String("project_id", projectID),
			zap.String("blueprint_id", bpID),
			zap.Error(err))
		_, _ = s.db.Exec(context.Background(),
			`UPDATE book_blueprints SET status='failed', error_message=$1, updated_at=NOW() WHERE id=$2`,
			err.Error(), bpID)
		return err
	}
	return nil
}

// doGenerateWork gathers all existing project assets, builds a context-rich prompt
// incorporating them, calls the LLM, then merges the AI response into the DB while
// preserving any user-edited content.
func (s *BlueprintService) doGenerateWork(ctx context.Context, projectID, bpID, runID string, project models.Project, req models.GenerateBlueprintRequest) error {
	return s.doGenerateBlueprintWork(ctx, projectID, bpID, runID, project, req)
}

func (s *BlueprintService) projectSnapshot(ctx context.Context, projectID string) (models.Project, error) {
	var project models.Project
	err := s.db.QueryRow(ctx,
		`SELECT id, title, genre, description, style_description, COALESCE(language, 'zh-CN'), target_words, chapter_words,
		        status, COALESCE(project_type,'original'), continuation_ref_id, COALESCE(continuation_start_chapter,1), created_at, updated_at
		 FROM projects WHERE id = $1`, projectID).Scan(
		&project.ID, &project.Title, &project.Genre, &project.Description, &project.StyleDescription,
		&project.Language, &project.TargetWords, &project.ChapterWords, &project.Status,
		&project.ProjectType, &project.ContinuationRefID, &project.ContinuationStartChapter,
		&project.CreatedAt, &project.UpdatedAt)
	if err != nil {
		return models.Project{}, fmt.Errorf("project not found: %w", err)
	}
	return project, nil
}

func (s *BlueprintService) Get(ctx context.Context, projectID string) (*models.BookBlueprint, error) {
	var bp models.BookBlueprint
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, world_bible_ref, master_outline, relation_graph, global_timeline, status, version, review_comment, error_message, created_at, updated_at
		 FROM book_blueprints WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&bp.ID, &bp.ProjectID, &bp.WorldBibleRef,
		rawJSONScanner{dst: &bp.MasterOutline},
		rawJSONScanner{dst: &bp.RelationGraph},
		rawJSONScanner{dst: &bp.GlobalTimeline},
		&bp.Status, &bp.Version, &bp.ReviewComment, &bp.ErrorMessage,
		&bp.CreatedAt, &bp.UpdatedAt)
	if err != nil {
		if errors.Is(err, database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &bp, nil
}

func (s *BlueprintService) Update(ctx context.Context, id string, req models.UpdateBlueprintRequest) (*models.BookBlueprint, error) {
	masterOutline, err := json.Marshal(strings.TrimSpace(req.MasterOutline))
	if err != nil {
		return nil, fmt.Errorf("marshal master outline: %w", err)
	}
	relationGraph, err := json.Marshal(strings.TrimSpace(req.RelationGraph))
	if err != nil {
		return nil, fmt.Errorf("marshal relation graph: %w", err)
	}
	globalTimeline, err := json.Marshal(strings.TrimSpace(req.GlobalTimeline))
	if err != nil {
		return nil, fmt.Errorf("marshal global timeline: %w", err)
	}

	var bp models.BookBlueprint
	err = s.db.QueryRow(ctx,
		`UPDATE book_blueprints
		 SET master_outline = $1,
		     relation_graph = $2,
		     global_timeline = $3,
		     status = CASE
		         WHEN status IN ('failed', 'rejected', 'pending_review') THEN 'draft'
		         ELSE status
		     END,
		     error_message = NULL,
		     version = version + 1,
		     updated_at = NOW()
		 WHERE id = $4 AND version = $5
		 RETURNING id, project_id, world_bible_ref, master_outline, relation_graph, global_timeline,
		           status, version, review_comment, error_message, created_at, updated_at`,
		masterOutline, relationGraph, globalTimeline, id, req.Version,
	).Scan(
		&bp.ID, &bp.ProjectID, &bp.WorldBibleRef,
		rawJSONScanner{dst: &bp.MasterOutline},
		rawJSONScanner{dst: &bp.RelationGraph},
		rawJSONScanner{dst: &bp.GlobalTimeline},
		&bp.Status, &bp.Version, &bp.ReviewComment, &bp.ErrorMessage,
		&bp.CreatedAt, &bp.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, database.ErrNoRows) {
			return nil, workflow.ErrOptimisticLock
		}
		return nil, fmt.Errorf("update blueprint: %w", err)
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
		 WHERE wr.project_id = $1 AND ws.step_key = 'blueprint' AND ws.status IN ('pending', 'generated')
		 ORDER BY wr.created_at DESC
		 LIMIT 1`, projectID).Scan(&stepID, &stepVersion); err == nil {
		// Ensure the step is in 'generated' state before transitioning to 'approved'.
		// MarkStepGenerated is idempotent if already generated.
		_ = s.wf.MarkStepGenerated(ctx, stepID, id)
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

// GenerateChapterOutlines generates chapter outlines for a specific volume or batch of chapters.
// This allows for incremental generation to avoid overwhelming the AI with too many chapters at once.
// startChapter: 0 means auto-continue from where it left off, >0 means start from specific chapter (supports regeneration)
func (s *BlueprintService) GenerateChapterOutlines(ctx context.Context, projectID string, volumeNum int, batchSize int, startChapter int) error {
	return s.generateChapterOutlines(ctx, projectID, volumeNum, batchSize, startChapter)
}

// autoAssignForeshadowingTimings calls the LLM to assign planned_embed_chapter and
// planned_resolve_chapter to any foreshadowings that still have 0 for those fields.
// It does two short non-transactional reads and one short batch write — no N+1, no long txn.
func (s *BlueprintService) autoAssignForeshadowingTimings(ctx context.Context, projectID string, logger *zap.Logger) {
	// 1. Fetch only unassigned foreshadowings in a single query.
	type unassignedFS struct {
		ID          string
		Content     string
		Priority    int
		EmbedMethod string
	}
	var unassigned []unassignedFS
	fsRows, err := s.db.Query(ctx,
		`SELECT id, content, priority, COALESCE(embed_method, '')
		 FROM foreshadowings
		 WHERE project_id = $1
		   AND COALESCE(planned_embed_chapter, 0) = 0
		   AND status IN ('planned', 'planted')
		 ORDER BY priority DESC`,
		projectID)
	if err != nil {
		return
	}
	for fsRows.Next() {
		var f unassignedFS
		if fsRows.Scan(&f.ID, &f.Content, &f.Priority, &f.EmbedMethod) == nil {
			unassigned = append(unassigned, f)
		}
	}
	fsRows.Close()

	if len(unassigned) == 0 {
		return
	}

	// 2. Fetch all generated chapter outlines for this project in a single query.
	type chSummary struct {
		Num        int
		Title      string
		FirstEvent string
	}
	var chapters []chSummary
	outlineRows, err := s.db.Query(ctx,
		`SELECT order_num, title, content FROM outlines
		 WHERE project_id = $1 AND level = 'chapter'
		 ORDER BY order_num`,
		projectID)
	if err != nil {
		return
	}
	for outlineRows.Next() {
		var oNum int
		var oTitle string
		var oContent json.RawMessage
		if outlineRows.Scan(&oNum, &oTitle, &oContent) != nil {
			continue
		}
		firstEvent := ""
		var data map[string]interface{}
		if json.Unmarshal(oContent, &data) == nil {
			if evts, ok := data["events"].([]interface{}); ok && len(evts) > 0 {
				if es, ok := evts[0].(string); ok {
					firstEvent = es
				}
			}
		}
		chapters = append(chapters, chSummary{Num: oNum, Title: oTitle, FirstEvent: firstEvent})
	}
	outlineRows.Close()

	if len(chapters) == 0 {
		return
	}

	// 3. Build LLM prompt.
	lastChapterNum := chapters[len(chapters)-1].Num
	var sb strings.Builder
	sb.WriteString("你是小说策划助手。根据以下章节大纲列表，为每条尚未分配时间点的伏笔指定【植入章节号】和【回收章节号】。\n\n")
	sb.WriteString("## 章节大纲摘要\n")
	for _, ch := range chapters {
		sb.WriteString(fmt.Sprintf("第%d章《%s》: %s\n", ch.Num, ch.Title, ch.FirstEvent))
	}
	sb.WriteString(fmt.Sprintf("\n（已生成至第%d章）\n\n", lastChapterNum))
	sb.WriteString("## 待分配伏笔列表（序号从0开始）\n")
	for i, fs := range unassigned {
		sb.WriteString(fmt.Sprintf("%d. [P%d] %s（埋设方式：%s）\n", i, fs.Priority, fs.Content, fs.EmbedMethod))
	}
	sb.WriteString(`
请输出JSON，为每条伏笔分配植入和回收章节号：
{
  "assignments": [
    {"idx": 0, "embed": 植入章节号, "resolve": 回收章节号},
    ...
  ]
}

约束：
- embed必须严格小于resolve，且两者至少相差5章
- 高优先级（P>=7）伏笔的resolve优先安排在高潮/转折章节
- 植入/回收章节必须在已生成的章节范围内
- 只输出JSON，无其他文字`)

	// 4. Call LLM (fire and forget on error).
	resp, aiErr := s.ai.Chat(ctx, gateway.ChatRequest{
		Task:      "foreshadowing_assignment",
		MaxTokens: 1200,
		Messages:  []gateway.ChatMessage{{Role: "user", Content: sb.String()}},
	})
	if aiErr != nil {
		logger.Warn("auto-assign foreshadowing timings: LLM call failed", zap.Error(aiErr))
		return
	}

	rawJSON := extractBlueprintJSON(resp.Content)
	var result struct {
		Assignments []struct {
			Idx     int `json:"idx"`
			Embed   int `json:"embed"`
			Resolve int `json:"resolve"`
		} `json:"assignments"`
	}
	if jsonErr := json.Unmarshal([]byte(rawJSON), &result); jsonErr != nil {
		previewLen := 200
		if len(rawJSON) < previewLen {
			previewLen = len(rawJSON)
		}
		logger.Warn("auto-assign foreshadowing timings: parse failed",
			zap.Error(jsonErr), zap.String("raw", rawJSON[:previewLen]))
		return
	}

	// 5. Batch update — short, no explicit transaction needed for individual row updates.
	updateBatch := &database.Batch{}
	for _, a := range result.Assignments {
		if a.Idx < 0 || a.Idx >= len(unassigned) {
			continue
		}
		if a.Embed <= 0 || a.Resolve <= a.Embed+4 {
			continue // skip invalid assignments
		}
		updateBatch.Queue(
			`UPDATE foreshadowings
			 SET planned_embed_chapter = $1, planned_resolve_chapter = $2, updated_at = NOW()
			 WHERE id = $3 AND COALESCE(planned_embed_chapter, 0) = 0`,
			a.Embed, a.Resolve, unassigned[a.Idx].ID)
	}

	if updateBatch.Len() == 0 {
		return
	}

	ubr := s.db.SendBatch(ctx, updateBatch)
	for i := 0; i < updateBatch.Len(); i++ {
		if _, bErr := ubr.Exec(); bErr != nil {
			logger.Warn("auto-assign foreshadowing timings: update failed", zap.Error(bErr))
		}
	}
	ubr.Close()
	logger.Info("auto-assigned foreshadowing timings", zap.Int("updated", updateBatch.Len()))
}

func (s *BlueprintService) Export(ctx context.Context, projectID string) (*BlueprintExport, error) {
	export := &BlueprintExport{
		ExportedAt: time.Now(),
		Version:    "1.0",
	}

	// Get blueprint
	bp, err := s.Get(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get blueprint: %w", err)
	}
	if bp == nil {
		return nil, fmt.Errorf("no blueprint found for project")
	}
	export.Blueprint = *bp

	// Get volumes
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, volume_num, title, blueprint_id, status, chapter_start, chapter_end, review_comment, created_at, updated_at
		 FROM volumes WHERE project_id = $1 ORDER BY volume_num`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query volumes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v models.Volume
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.VolumeNum, &v.Title, &v.BlueprintID, &v.Status,
			&v.ChapterStart, &v.ChapterEnd, &v.ReviewComment, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan volume: %w", err)
		}
		export.Volumes = append(export.Volumes, v)
	}

	// Get chapter outlines
	outlines, err := s.outlines.List(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list outlines: %w", err)
	}
	for _, o := range outlines {
		if o.Level == "chapter" {
			export.ChapterOutlines = append(export.ChapterOutlines, o)
		}
	}

	// Get characters
	chars, err := s.characters.List(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list characters: %w", err)
	}
	export.Characters = chars

	// Get foreshadowings
	fss, err := s.foreshadowings.List(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list foreshadowings: %w", err)
	}
	export.Foreshadowings = fss

	// Get world bible
	wb, err := s.worldBibles.Get(ctx, projectID)
	if err == nil && wb != nil {
		export.WorldBible = wb
	}

	return export, nil
}

func (s *BlueprintService) BlankTemplate(ctx context.Context, projectID string) (*BlueprintExport, error) {
	project, err := s.projectSnapshot(ctx, projectID)
	if err != nil {
		return nil, err
	}
	chapterWords := project.ChapterWords
	if chapterWords <= 0 {
		chapterWords = 3000
	}
	targetWords := project.TargetWords
	if targetWords <= 0 {
		targetWords = 500000
	}
	estimatedChapters := targetWords / chapterWords
	if estimatedChapters <= 0 {
		estimatedChapters = 120
	}
	template := &BlueprintExport{
		ExportedAt: time.Now(),
		Version:    "1.0",
		Blueprint: models.BookBlueprint{
			ProjectID:      projectID,
			MasterOutline:  json.RawMessage(`"第1卷:填写本卷主题、核心冲突、高潮点。第2卷:继续填写；可按需增删卷。"`),
			RelationGraph:  json.RawMessage(`"主角-核心对手:填写关系变化;主角-盟友:填写关系变化"`),
			GlobalTimeline: json.RawMessage(`"开篇:填写关键事件;第一卷末:填写转折;结局:填写终局状态"`),
			Status:         "draft",
			Version:        1,
		},
		WorldBible: &models.WorldBible{
			ProjectID: projectID,
			Content: json.RawMessage(`{
  "world_view": "填写世界观总览",
  "era_background": "填写时代背景",
  "geography": "填写地理/舞台",
  "power_system": "填写力量或职业体系",
  "social_structure": "填写社会结构与势力",
  "core_conflict": "填写全书核心冲突"
}`),
			Version: 1,
		},
		Volumes: []models.Volume{
			{ProjectID: projectID, VolumeNum: 1, Title: "第一卷", ChapterStart: 1, ChapterEnd: estimatedChapters / 4, Status: "draft"},
			{ProjectID: projectID, VolumeNum: 2, Title: "第二卷", ChapterStart: estimatedChapters/4 + 1, ChapterEnd: estimatedChapters / 2, Status: "draft"},
			{ProjectID: projectID, VolumeNum: 3, Title: "第三卷", ChapterStart: estimatedChapters/2 + 1, ChapterEnd: estimatedChapters * 3 / 4, Status: "draft"},
			{ProjectID: projectID, VolumeNum: 4, Title: "第四卷", ChapterStart: estimatedChapters*3/4 + 1, ChapterEnd: estimatedChapters, Status: "draft"},
		},
		ChapterOutlines: []models.Outline{
			{ProjectID: projectID, Level: "chapter", OrderNum: 1, Title: "第一章标题", Content: json.RawMessage(`{"events":["填写本章事件1","填写本章事件2","填写本章断章点"]}`), TensionTarget: 0.5},
		},
		Characters: []models.Character{
			{ProjectID: projectID, Name: "主角姓名", RoleType: "protagonist", Profile: json.RawMessage(`{"description":"填写当前身份、欲望、弱点、初始能力"}`), CurrentState: json.RawMessage(`{}`)},
		},
		Foreshadowings: []models.Foreshadowing{
			{ProjectID: projectID, Content: "填写伏笔内容", EmbedMethod: "implicit", PlannedEmbedChapter: 3, PlannedResolveChapter: 20, Priority: 5, Status: "planned"},
		},
	}
	if template.Volumes[0].ChapterEnd <= 0 {
		template.Volumes[0].ChapterEnd = 30
		template.Volumes[1].ChapterStart, template.Volumes[1].ChapterEnd = 31, 60
		template.Volumes[2].ChapterStart, template.Volumes[2].ChapterEnd = 61, 90
		template.Volumes[3].ChapterStart, template.Volumes[3].ChapterEnd = 91, 120
	}
	return template, nil
}

func (s *BlueprintService) Import(ctx context.Context, projectID string, data *BlueprintExport) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Import world bible
	if data.WorldBible != nil && len(data.WorldBible.Content) > 4 {
		wbID := uuid.New().String()
		if _, err := tx.Exec(ctx,
			`INSERT INTO world_bibles (id, project_id, content, version, created_at, updated_at)
			 VALUES ($1, $2, $3, 1, NOW(), NOW())
			 ON CONFLICT (project_id) DO UPDATE
			     SET content    = EXCLUDED.content,
			         version    = world_bibles.version + 1,
			         updated_at = NOW()`,
			wbID, projectID, data.WorldBible.Content); err != nil {
			return fmt.Errorf("import world bible: %w", err)
		}
	}

	// Import characters
	if len(data.Characters) > 0 {
		chBatch := &database.Batch{}
		for _, ch := range data.Characters {
			chBatch.Queue(
				`INSERT INTO characters (id, project_id, name, role_type, profile, current_state, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
				 ON CONFLICT (project_id, name) DO UPDATE SET
				     role_type = EXCLUDED.role_type,
				     profile = EXCLUDED.profile,
				     current_state = EXCLUDED.current_state,
				     updated_at = NOW()`,
				uuid.New().String(), projectID, ch.Name, ch.RoleType, ch.Profile, ch.CurrentState)
		}
		br := tx.SendBatch(ctx, chBatch)
		for range data.Characters {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("import character: %w", err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("character batch close: %w", err)
		}
	}

	// Import foreshadowings
	if len(data.Foreshadowings) > 0 {
		// Clear existing foreshadowings to avoid duplicates
		if _, err := tx.Exec(ctx, `DELETE FROM foreshadowings WHERE project_id = $1`, projectID); err != nil {
			return fmt.Errorf("clear foreshadowings: %w", err)
		}
		fsBatch := &database.Batch{}
		for _, fs := range data.Foreshadowings {
			fsBatch.Queue(
				`INSERT INTO foreshadowings (id, project_id, content, embed_chapter_id, resolve_chapter_id, embed_method, resolve_method, planned_embed_chapter, planned_resolve_chapter, priority, status, tags, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW())`,
				uuid.New().String(), projectID, fs.Content, fs.EmbedChapterID, fs.ResolveChapterID,
				fs.EmbedMethod, fs.ResolveMethod, fs.PlannedEmbedChapter, fs.PlannedResolveChapter,
				fs.Priority, fs.Status, fs.Tags)
		}
		br := tx.SendBatch(ctx, fsBatch)
		for range data.Foreshadowings {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("import foreshadowing: %w", err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("foreshadowing batch close: %w", err)
		}
	}

	// Import blueprint
	bpID := uuid.New().String()
	wbRef := data.Blueprint.WorldBibleRef
	if _, err := tx.Exec(ctx,
		`INSERT INTO book_blueprints (id, project_id, world_bible_ref, master_outline, relation_graph, global_timeline, status, version, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'draft', 1, NOW(), NOW())
		 ON CONFLICT (project_id) DO UPDATE SET
		     master_outline = EXCLUDED.master_outline,
		     relation_graph = EXCLUDED.relation_graph,
		     global_timeline = EXCLUDED.global_timeline,
		     status = 'draft',
		     version = book_blueprints.version + 1,
		     updated_at = NOW()
		 RETURNING id`,
		bpID, projectID, wbRef, data.Blueprint.MasterOutline, data.Blueprint.RelationGraph, data.Blueprint.GlobalTimeline); err != nil {
		return fmt.Errorf("import blueprint: %w", err)
	}

	// Import volumes
	if len(data.Volumes) > 0 {
		// Clear existing volumes
		if _, err := tx.Exec(ctx, `UPDATE chapters SET volume_id = NULL WHERE project_id = $1`, projectID); err != nil {
			return fmt.Errorf("clear chapter volume refs: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM volumes WHERE project_id = $1`, projectID); err != nil {
			return fmt.Errorf("clear volumes: %w", err)
		}
		volBatch := &database.Batch{}
		for _, vol := range data.Volumes {
			volBatch.Queue(
				`INSERT INTO volumes (id, project_id, volume_num, title, blueprint_id, chapter_start, chapter_end, status, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, 'draft', NOW(), NOW())`,
				uuid.New().String(), projectID, vol.VolumeNum, vol.Title, bpID, vol.ChapterStart, vol.ChapterEnd)
		}
		br := tx.SendBatch(ctx, volBatch)
		for range data.Volumes {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("import volume: %w", err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("volume batch close: %w", err)
		}
	}

	// Import chapter outlines
	if len(data.ChapterOutlines) > 0 {
		// Clear existing chapter outlines
		if _, err := tx.Exec(ctx, `DELETE FROM outlines WHERE project_id = $1 AND level = 'chapter'`, projectID); err != nil {
			return fmt.Errorf("clear chapter outlines: %w", err)
		}
		outlineBatch := &database.Batch{}
		for _, outline := range data.ChapterOutlines {
			outlineBatch.Queue(
				`INSERT INTO outlines (id, project_id, level, order_num, title, content, tension_target, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())`,
				uuid.New().String(), projectID, outline.Level, outline.OrderNum, outline.Title, outline.Content, outline.TensionTarget)
		}
		br := tx.SendBatch(ctx, outlineBatch)
		for range data.ChapterOutlines {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("import chapter outline: %w", err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("chapter outline batch close: %w", err)
		}
	}

	// Update project status
	if _, err := tx.Exec(ctx, `UPDATE projects SET status = 'blueprint_generated', updated_at = NOW() WHERE id = $1`, projectID); err != nil {
		return fmt.Errorf("update project status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit import: %w", err)
	}

	s.logger.Info("blueprint imported successfully",
		zap.String("project_id", projectID),
		zap.Int("volumes", len(data.Volumes)),
		zap.Int("chapter_outlines", len(data.ChapterOutlines)),
		zap.Int("characters", len(data.Characters)),
		zap.Int("foreshadowings", len(data.Foreshadowings)))
	return nil
}
