package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	db             *pgxpool.Pool
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
	db *pgxpool.Pool,
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

// Generate creates a placeholder blueprint record immediately (status="generating") and
// launches the actual AI generation in the background. The caller receives 202 and
// should poll GET /projects/:id/blueprint until status changes.
func (s *BlueprintService) Generate(ctx context.Context, projectID string, req models.GenerateBlueprintRequest) (*models.BookBlueprint, error) {
	// Validate that the project exists before creating anything.
	var project models.Project
	err := s.db.QueryRow(ctx,
		`SELECT id, title, genre, description, style_description, target_words, chapter_words FROM projects WHERE id = $1`, projectID).Scan(
		&project.ID, &project.Title, &project.Genre, &project.Description, &project.StyleDescription,
		&project.TargetWords, &project.ChapterWords)
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
		 ON CONFLICT (project_id) DO UPDATE SET
		     status = 'generating',
		     error_message = NULL,
		     updated_at = NOW()
		 RETURNING id, project_id, world_bible_ref, master_outline, relation_graph, global_timeline, status, version, review_comment, error_message, created_at, updated_at`,
		bpID, projectID).Scan(
		&bp.ID, &bp.ProjectID, &bp.WorldBibleRef, &bp.MasterOutline, &bp.RelationGraph,
		&bp.GlobalTimeline, &bp.Status, &bp.Version, &bp.ReviewComment, &bp.ErrorMessage,
		&bp.CreatedAt, &bp.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create blueprint placeholder: %w", err)
	}
	// On conflict, RETURNING gives us the existing row's id, not the new bpID.
	bpID = bp.ID

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

// doGenerateWork gathers all existing project assets, builds a context-rich prompt
// incorporating them, calls the LLM, then merges the AI response into the DB while
// preserving any user-edited content.
func (s *BlueprintService) doGenerateWork(ctx context.Context, projectID, bpID, runID string, project models.Project, req models.GenerateBlueprintRequest) error {
	return s.doGenerateBlueprintWork(ctx, projectID, bpID, runID, project, req)
}

// min returns the smaller of two ints (stdlib min is Go 1.21+).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractBlueprintJSON strips markdown code fences, then extracts the outermost
// JSON object while correctly tracking string literals so that { } inside
// quoted values do not corrupt the depth counter.
func extractBlueprintJSON(s string) string {
	s = strings.TrimSpace(s)
	// Strip markdown code fences: ```json ... ``` or ``` ... ```
	if idx := strings.Index(s, "```"); idx != -1 {
		rest := s[idx+3:]
		if nl := strings.IndexByte(rest, '\n'); nl != -1 {
			rest = rest[nl+1:]
		}
		if end := strings.LastIndex(rest, "```"); end != -1 {
			rest = rest[:end]
		}
		s = strings.TrimSpace(rest)
	}

	start := strings.Index(s, "{")
	if start == -1 {
		return s
	}

	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inStr {
			escaped = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	// Truncated JSON — return what we have and let the caller attempt to parse.
	return s[start:]
}

// buildWorldBibleFieldsHint returns JSON field hint string appropriate for the genre.
func buildWorldBibleFieldsHint(genre string) string {
	switch genre {
	case "西幻":
		return `
    "world_view": "世界观总览（大陆/世界名称、历史纪元）",
    "era_background": "时代背景（当前纪元状况、主要历史事件）",
    "geography": "地理环境（大陆格局、主要地名、地形特色）",
    "races": "种族体系（精灵/矮人/兽人/人类等各族特征与关系）",
    "magic_system": "魔法体系（规则、代价、等级划分、施法方式）",
    "power_system": "职业体系（骑士/法师/游侠/牧师等职业设定）",
    "faction_structure": "阵营结构（王国/帝国/公会/神殿等势力划分）",
    "social_structure": "社会结构（贵族制度、平民生活、种族关系）",
    "religion_mythology": "神明与神话（主要神系、宗教信仰、神话传说）",
    "core_conflict": "核心冲突（黑暗势力/古老诅咒/种族矛盾等主要矛盾）"`
	case "玄幻":
		return `
    "world_view": "世界观概述",
    "era_background": "时代背景",
    "geography": "地理环境",
    "cultivation_system": "修炼体系（境界划分、修炼方式、天才资质标准）",
    "power_system": "力量体系（法则、禁忌、至高境界）",
    "social_structure": "社会结构（宗门/家族/皇朝势力格局）",
    "core_conflict": "核心冲突"`
	case "末世":
		return `
    "world_view": "末世背景（灾变类型、爆发时间、当前时间节点）",
    "era_background": "时代背景",
    "geography": "地理环境（安全区/危险区/资源点分布）",
    "threat_system": "威胁体系（变异生物/病毒/怪物等级划分）",
    "power_system": "力量体系（异能/进化/武装类型）",
    "social_structure": "社会结构（幸存者营地/组织/势力格局）",
    "resource_economy": "资源经济（稀缺资源、交易体系）",
    "core_conflict": "核心冲突"`
	default:
		return `
    "world_view": "世界观概述",
    "era_background": "时代背景",
    "geography": "地理环境",
    "social_structure": "社会结构",
    "power_system": "力量体系",
    "core_conflict": "核心冲突"`
	}
}

// buildGenreConstraints returns genre-specific bullet-point constraints for the prompt.
func buildGenreConstraints(genre string, gt *models.GenreTemplate) string {
	var points []string

	switch genre {
	case "西幻":
		points = []string{
			"- 人名、地名、技能名须采用西式风格（可音译或创造），避免使用中文传统风格词汇",
			"- 魔法体系须有明确规则与代价，不能是\"万能魔法\"",
			"- master_outline 须体现英雄旅程阶段：「启程→考验→深渊→涅槃→归返」",
			"- 角色 profile 应包含种族、职业、技能特色",
			"- 【题材禁入元素】严禁出现以下不属于西幻的元素：科技/机械/电子设备/枪械/火箭/电脑/手机/网络/基因工程/纳米技术/人工智能/太空旅行/修炼境界/丹药/灵石/宗门体系/仙人/渡劫飞升/现代都市场景。一切力量来源必须是魔法、神力、血脉或自然元素，禁止出现科技驱动的力量体系。",
		}
	case "玄幻":
		points = []string{
			"- 修炼境界须清晰标注，成长不可一步登天",
			"- 战斗描写须结合力量体系，避免泛化",
			"- master_outline 须体现修炼突破的阶段感",
			"- 【题材禁入元素】严禁出现以下不属于玄幻的元素：枪械/火箭/电脑/手机/网络/基因工程/纳米技术/人工智能/太空旅行/西式骑士团/精灵矮人等西幻种族/现代都市场景/现代科技产品。一切力量来源必须是修炼、功法、天材地宝、血脉觉醒等修仙体系元素。",
		}
	case "末世":
		points = []string{
			"- 资源匮乏和紧张感须贯穿全文规划",
			"- 异能/进化逻辑须与世界设定自洽",
			"- master_outline 须体现生存→建立据点→反攻的递进结构",
			"- 【题材禁入元素】严禁出现以下不属于末世的元素：修炼境界/丹药/灵石/宗门体系/仙人/魔法/精灵矮人等奇幻种族/太空旅行/星际贸易。一切力量来源必须基于末世变异/异能觉醒/科技残留/生物进化等末世体系元素。",
		}
	case "科幻":
		points = []string{
			"- 科技设定须自洽，技术限制和副作用需与优势并存",
			"- 世界观须有宏观政治体系和微观生活细节",
			"- 【题材禁入元素】严禁出现以下不属于科幻的元素：修炼境界/丹药/灵石/宗门体系/仙人/魔法咒语/魔杖/精灵矮人等奇幻种族/武侠内功/剑气。一切力量来源必须基于科学技术/基因改造/机械增强/AI等科技体系元素。",
		}
	case "都市":
		points = []string{
			"- 社会规则、法律、商业逻辑须合理",
			"- 角色能力成长需符合现实逻辑",
			"- 【题材禁入元素】严禁出现以下不属于都市的元素：修炼飞升/魔法/精灵矮人/星际旅行/末世灾变（除非设定有超自然元素）。能力设定须以现实为基础。",
		}
	case "言情":
		points = []string{
			"- 感情发展须有事件驱动，不可无理由心动",
			"- 角色需有独立人格和成长弧线",
			"- 【题材禁入元素】禁止出现与感情主线无关的大量战斗/修炼/科技展示等喧宾夺主的内容。",
		}
	case "悬疑":
		points = []string{
			"- 核心谜题须在首卷前3章内抛出",
			"- 线索须公平分布，禁止突然出现从未提及的关键信息",
			"- 【题材禁入元素】严禁出现超自然力量/魔法/修炼等破坏推理逻辑的元素（除非设定为超自然悬疑）。",
		}
	}

	if gt != nil && len(points) == 0 {
		// Generic genre — no extra hard constraints beyond the template already in context.
		return ""
	}

	return strings.Join(points, "\n")
}

// buildGenreExclusionBlock returns the genre-specific forbidden element text for injection
// into chapter generation system prompts. This ensures the chapter author AI also enforces
// genre boundaries, not just the outline planner.
func buildGenreExclusionBlock(genre string) string {
	switch genre {
	case "西幻":
		return "【题材禁入元素 — 违反即为严重错误】本作品为西幻题材。严禁出现：科技产品（电脑/手机/枪械/机械装置）、修仙元素（丹药/灵石/宗门/渡劫飞升）、现代都市场景。一切力量来源必须基于魔法/神力/血脉/自然元素。"
	case "玄幻":
		return "【题材禁入元素 — 违反即为严重错误】本作品为玄幻题材。严禁出现：现代科技产品（电脑/手机/枪械）、西幻种族（精灵/矮人/兽人）、现代都市场景。一切力量来源必须基于修炼/功法/天材地宝/血脉觉醒。"
	case "末世":
		return "【题材禁入元素 — 违反即为严重错误】本作品为末世题材。严禁出现：修仙元素（丹药/灵石/宗门/飞升）、纯奇幻种族（精灵/矮人）、完好如初的现代社会秩序。一切设定需基于末世背景。"
	case "科幻":
		return "【题材禁入元素 — 违反即为严重错误】本作品为科幻题材。严禁出现：修仙元素（丹药/灵石/功法/渡劫）、纯魔法体系（咒语/魔杖/魔法阵）、中古奇幻种族。一切力量来源必须基于科技。"
	case "都市":
		return "【题材禁入元素 — 违反即为严重错误】本作品为都市题材。严禁出现：修炼飞升/魔法/奇幻种族/星际旅行/末世灾变等超出现实框架的元素（除非世界观设定明确允许）。"
	case "言情":
		return "【题材禁入元素】本作品为言情题材。战斗/修炼/科技等元素若有则必须为感情主线服务，不可喧宾夺主。"
	case "悬疑":
		return "【题材禁入元素】本作品为悬疑题材。严禁出现破坏推理逻辑的超自然力量（除非世界观设定为超自然悬疑）。"
	default:
		return ""
	}
}

// summariseJSON extracts a short summary string from a JSONB field for prompt context.
func summariseJSON(raw json.RawMessage, maxLen int) string {
	if len(raw) == 0 {
		return ""
	}
	// Try as a map first.
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err == nil {
		var parts []string
		for k, v := range m {
			str := fmt.Sprintf("%v", v)
			if len(str) > 80 {
				str = str[:80] + "…"
			}
			parts = append(parts, fmt.Sprintf("%s: %s", k, str))
			if len(strings.Join(parts, "; ")) > maxLen {
				break
			}
		}
		return strings.Join(parts, "; ")
	}
	// Try as a string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if len(s) > maxLen {
			return s[:maxLen] + "…"
		}
		return s
	}
	// Raw fallback.
	str := string(raw)
	if len(str) > maxLen {
		return str[:maxLen] + "…"
	}
	return str
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
		&bp.ID, &bp.ProjectID, &bp.WorldBibleRef, &bp.MasterOutline, &bp.RelationGraph,
		&bp.GlobalTimeline, &bp.Status, &bp.Version, &bp.ReviewComment, &bp.ErrorMessage,
		&bp.CreatedAt, &bp.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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
	updateBatch := &pgx.Batch{}
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

// extractTextFromJSON extracts text from a JSON field that might be a string or {raw_content: "..."}.
func extractTextFromJSON(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		return str
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err == nil {
		if rawContent, ok := obj["raw_content"].(string); ok {
			// Try to parse raw_content as JSON
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(rawContent), &parsed); err == nil {
				// Extract the specific field if exists
				for _, val := range parsed {
					if s, ok := val.(string); ok && s != "" {
						return s
					}
				}
			}
			return rawContent
		}
	}
	return string(data)
}

// extractVolumeSection attempts to extract the outline text for a specific volume
// from the master outline string. The master outline typically uses markers like
// "第N卷" or the volume title to separate sections.
// Returns the volume-specific section, or empty string if extraction fails.
func extractVolumeSection(masterOutline string, volumeNum int, volumeTitle string) string {
	if masterOutline == "" {
		return ""
	}

	// Try multiple patterns to locate the volume section
	// Pattern 1: "第N卷" with Chinese colon or colon
	markers := []string{
		fmt.Sprintf("第%d卷", volumeNum),
		volumeTitle,
	}

	bestStart := -1
	for _, marker := range markers {
		idx := strings.Index(masterOutline, marker)
		if idx >= 0 && (bestStart < 0 || idx < bestStart) {
			bestStart = idx
		}
	}

	if bestStart < 0 {
		return "" // Could not locate volume section
	}

	// Find the end: next volume marker or end of string
	nextVolMarker := fmt.Sprintf("第%d卷", volumeNum+1)
	endIdx := strings.Index(masterOutline[bestStart+1:], nextVolMarker)
	if endIdx >= 0 {
		return strings.TrimSpace(masterOutline[bestStart : bestStart+1+endIdx])
	}

	// No next volume marker found — take the rest but cap at reasonable length
	section := masterOutline[bestStart:]
	if len(section) > 500 {
		// Try to find a natural break point
		if nl := strings.Index(section[400:], "\n"); nl >= 0 {
			section = section[:400+nl]
		} else {
			section = section[:500]
		}
	}
	return strings.TrimSpace(section)
}

// BlueprintExport represents the complete blueprint package for export/import.
type BlueprintExport struct {
	Blueprint       models.BookBlueprint   `json:"blueprint"`
	Volumes         []models.Volume        `json:"volumes"`
	ChapterOutlines []models.Outline       `json:"chapter_outlines"`
	Characters      []models.Character     `json:"characters"`
	Foreshadowings  []models.Foreshadowing `json:"foreshadowings"`
	WorldBible      *models.WorldBible     `json:"world_bible,omitempty"`
	ExportedAt      time.Time              `json:"exported_at"`
	Version         string                 `json:"version"` // Format version for compatibility
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
		chBatch := &pgx.Batch{}
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
		fsBatch := &pgx.Batch{}
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
		volBatch := &pgx.Batch{}
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
		outlineBatch := &pgx.Batch{}
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
