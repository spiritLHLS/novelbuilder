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

// doGenerateWork gathers all existing project assets, builds a context-rich prompt
// incorporating them, calls the LLM, then merges the AI response into the DB while
// preserving any user-edited content.
func (s *BlueprintService) doGenerateWork(ctx context.Context, projectID, bpID, runID string, project models.Project, req models.GenerateBlueprintRequest) error {
	logger := s.logger.With(
		zap.String("project_id", projectID),
		zap.String("blueprint_id", bpID),
	)
	logger.Info("blueprint generation: gathering existing project data")

	// ── 1. Collect all existing project assets ────────────────────────────────

	existingWB, _ := s.worldBibles.Get(ctx, projectID)
	existingChars, _ := s.characters.List(ctx, projectID)
	existingForeshadowings, _ := s.foreshadowings.List(ctx, projectID)
	existingOutlines, _ := s.outlines.List(ctx, projectID)
	existingRefs, _ := s.references.List(ctx, projectID)
	glossaryBlock := s.glossary.BuildPromptBlock(ctx, projectID)
	genreTemplate, _ := s.genreTemplates.Get(ctx, project.Genre)

	logger.Info("blueprint generation: existing data gathered",
		zap.Int("characters", len(existingChars)),
		zap.Int("foreshadowings", len(existingForeshadowings)),
		zap.Int("outlines", len(existingOutlines)),
		zap.Int("references", len(existingRefs)),
		zap.Bool("has_world_bible", existingWB != nil),
		zap.Bool("has_genre_template", genreTemplate != nil),
	)

	// ── 2. Resolve generation parameters ─────────────────────────────────────

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
		// Derive from target word count: ~100k words per volume is typical for CN web novels.
		if project.TargetWords > 0 {
			volumeCount = project.TargetWords / 100000
		}
		if volumeCount < 4 {
			volumeCount = 4
		}
	}
	chaptersPerVolume := req.ChaptersPerVolume
	if chaptersPerVolume == 0 {
		if project.ChapterWords > 0 && project.TargetWords > 0 {
			totalChapters := project.TargetWords / project.ChapterWords
			chaptersPerVolume = (totalChapters + volumeCount - 1) / volumeCount
			if chaptersPerVolume < 10 {
				chaptersPerVolume = 10
			}
		} else {
			chaptersPerVolume = 30
		}
	}

	// ── 3. Build context sections from existing assets ────────────────────────

	var sb strings.Builder

	// Genre template rules (highest priority — placed first so AI internalises them).
	if genreTemplate != nil {
		sb.WriteString(fmt.Sprintf("## 【流派创作规则 - %s】\n", genreTemplate.Genre))
		if genreTemplate.RulesContent != "" {
			sb.WriteString(genreTemplate.RulesContent)
			sb.WriteString("\n")
		}
		if genreTemplate.LanguageConstraints != "" {
			sb.WriteString(fmt.Sprintf("\n**语言约束：** %s\n", genreTemplate.LanguageConstraints))
		}
		if genreTemplate.RhythmRules != "" {
			sb.WriteString(fmt.Sprintf("\n**节奏规则：** %s\n", genreTemplate.RhythmRules))
		}
		sb.WriteString("\n")
	}

	// Existing world bible.
	if existingWB != nil && len(existingWB.Content) > 4 {
		sb.WriteString("## 【现有世界观设定（请在此基础上延伸扩展，禁止修改已有字段值）】\n")
		sb.WriteString(string(existingWB.Content))
		sb.WriteString("\n\n")
	}

	// Existing characters.
	if len(existingChars) > 0 {
		sb.WriteString("## 【现有角色（这些角色已被用户确认，请保留并在大纲/关系图中合理使用，可补充新角色）】\n")
		for _, ch := range existingChars {
			profileStr := strings.TrimSpace(string(ch.Profile))
			if profileStr == "" || profileStr == "{}" || profileStr == "null" {
				profileStr = "(暂无描述)"
			}
			sb.WriteString(fmt.Sprintf("- **%s**（%s）：%s\n", ch.Name, ch.RoleType, profileStr))
		}
		sb.WriteString("\n")
	}

	// Existing foreshadowings.
	if len(existingForeshadowings) > 0 {
		sb.WriteString("## 【现有伏笔（请在大纲与时间线中安排这些伏笔的铺垫与揭露，并可新增伏笔）】\n")
		for i, fs := range existingForeshadowings {
			if i >= 20 {
				sb.WriteString(fmt.Sprintf("  ...（共%d条，仅列前20条）\n", len(existingForeshadowings)))
				break
			}
			sb.WriteString(fmt.Sprintf("%d. %s [状态: %s, 优先级: %d]\n", i+1, fs.Content, fs.Status, fs.Priority))
		}
		sb.WriteString("\n")
	}

	// Existing outlines.
	if len(existingOutlines) > 0 {
		sb.WriteString("## 【现有大纲节点（请参考这些节点规划全书结构，已有节点优先级更高）】\n")
		for i, o := range existingOutlines {
			if i >= 30 {
				sb.WriteString(fmt.Sprintf("  ...（共%d条大纲节点）\n", len(existingOutlines)))
				break
			}
			contentStr := ""
			var contentMap map[string]interface{}
			if json.Unmarshal(o.Content, &contentMap) == nil {
				if c, ok := contentMap["content"].(string); ok && c != "" {
					contentStr = "：" + c
				} else if c, ok := contentMap["key_events"].(string); ok && c != "" {
					contentStr = "：" + c
				}
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s%s\n", o.Level, o.Title, contentStr))
		}
		sb.WriteString("\n")
	}

	// Completed reference materials — provide style/narrative/atmosphere analysis.
	refCount := 0
	for _, ref := range existingRefs {
		if ref.Status != "completed" {
			continue
		}
		if refCount == 0 {
			sb.WriteString("## 【参考素材风格分析（请吸收以下风格特征融入创作）】\n")
		}
		refCount++
		sb.WriteString(fmt.Sprintf("### 《%s》", ref.Title))
		if ref.Author != "" {
			sb.WriteString(fmt.Sprintf(" 作者：%s", ref.Author))
		}
		sb.WriteString("\n")
		if len(ref.NarrativeLayer) > 4 {
			sb.WriteString(fmt.Sprintf("  叙事特征：%s\n", summariseJSON(ref.NarrativeLayer, 200)))
		}
		if len(ref.AtmosphereLayer) > 4 {
			sb.WriteString(fmt.Sprintf("  氛围特征：%s\n", summariseJSON(ref.AtmosphereLayer, 200)))
		}
		if len(ref.StyleLayer) > 4 {
			sb.WriteString(fmt.Sprintf("  语言风格：%s\n", summariseJSON(ref.StyleLayer, 200)))
		}
	}
	if refCount > 0 {
		sb.WriteString("\n")
	}

	// Glossary terms.
	if glossaryBlock != "" {
		sb.WriteString(glossaryBlock)
		sb.WriteString("\n")
	}

	contextSection := sb.String()

	// ── 4. Build genre-specific output instructions ───────────────────────────

	worldBibleFields := buildWorldBibleFieldsHint(genre)

	// ── 5. Build final prompt ─────────────────────────────────────────────────

	hasExistingData := existingWB != nil || len(existingChars) > 0 || len(existingForeshadowings) > 0 || len(existingOutlines) > 0
	taskInstruction := "生成一套全新的完整整书资产包"
	if hasExistingData {
		taskInstruction = "在现有素材基础上完成并扩展整书资产包（不要删改用户已有内容，只补全缺失部分并新增内容）"
	}

	// Compute human-readable word-count hints for the prompt.
	targetWordsWan := project.TargetWords / 10000 // convert to 万字
	if targetWordsWan == 0 {
		targetWordsWan = 50 // sensible default
	}
	chapterWordsVal := project.ChapterWords
	if chapterWordsVal <= 0 {
		chapterWordsVal = 3000
	}
	estimatedTotalChapters := project.TargetWords / chapterWordsVal
	if estimatedTotalChapters <= 0 {
		estimatedTotalChapters = volumeCount * chaptersPerVolume
	}

	prompt := fmt.Sprintf(`你是一位资深小说策划编辑，擅长%s类型的长篇小说规划。
请%s。

%s
---
## 当前项目信息
- 小说标题：%s
- 类型/流派：%s
- 核心创意：%s
- 风格描述：%s
- 计划卷数：%d卷（**必须规划足够的卷数，不得少于%d卷**）
- 每卷章节数：%d章
- 全书目标字数：约%d万字
- 每章目标字数：约%d字
- 推算总章节数：约%d章（目标字数 ÷ 每章字数）
- 每卷预期字数：约%d万字（均匀分配）

---
## 输出要求

请**严格以 JSON 格式**返回以下资产（不要在 JSON 外输出任何文字）：

{
  "world_bible": {%s},
  "characters": [{"name":"角色名","role_type":"protagonist|supporting|antagonist|mentor|minor","profile":"角色描述（100字以内纯字符串）"}],
  "master_outline": "第1卷:主题/核心冲突/高潮点;第2卷:...",
  "relation_graph": "角色A-角色B:关系描述;...",
  "global_timeline": "时间节点1:事件描述;...",
  "foreshadowings": [{"content":"伏笔内容","embed_method":"explicit|implicit|symbolic","priority":8}],
  "volumes": [{"title":"卷名","chapter_start":1,"chapter_end":%d}]
}

**重要约束：**
%s
- volumes 数组**必须**包含恰好 %d 个卷，覆盖第1章到第%d章
- characters 中已存在角色无需重复列出，只列出**新增**角色
- foreshadowings 中已存在伏笔无需重复，只列出**新增**伏笔
- 所有内容须与已有世界观、角色、术语表保持一致
- 确保所有伏笔在大纲时间线中都有合适的铺垫与解决安排
`,
		genre,
		taskInstruction,
		contextSection,
		project.Title,
		genre,
		idea,
		project.StyleDescription,
		volumeCount, volumeCount,
		chaptersPerVolume,
		targetWordsWan,
		chapterWordsVal,
		estimatedTotalChapters,
		targetWordsWan/volumeCount,
		worldBibleFields,
		chaptersPerVolume,
		buildGenreConstraints(genre, genreTemplate),
		volumeCount,
		volumeCount, volumeCount*chaptersPerVolume,
	)

	// ── 6. Call the LLM ───────────────────────────────────────────────────────
	logger.Info("blueprint generation: calling LLM")
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

	// ── 7. Write to DB — merge strategy preserves user edits ─────────────────
	logger.Info("blueprint generation: writing data to database")
	tx, txErr := s.db.Begin(ctx)
	if txErr != nil {
		return fmt.Errorf("begin transaction: %w", txErr)
	}
	defer tx.Rollback(ctx)

	// World bible: merge AI content with existing — existing fields WIN on conflict.
	// This means user's manual edits are never overwritten; AI only fills blank fields.
	if parsed.WorldBible != nil && len(parsed.WorldBible) > 4 {
		wbID := uuid.New().String()
		if _, err := tx.Exec(ctx,
			`INSERT INTO world_bibles (id, project_id, content, version, created_at, updated_at)
			 VALUES ($1, $2, $3, 1, NOW(), NOW())
			 ON CONFLICT (project_id) DO UPDATE
			     SET content    = EXCLUDED.content || world_bibles.content,
			         version    = world_bibles.version + 1,
			         updated_at = NOW()`,
			wbID, projectID, parsed.WorldBible); err != nil {
			return fmt.Errorf("store world bible: %w", err)
		}
	}

	// Characters: ON CONFLICT DO NOTHING — never overwrite user-edited characters.
	// Only brand-new names are inserted.
	if len(parsed.Characters) > 0 {
		chBatch := &pgx.Batch{}
		for _, ch := range parsed.Characters {
			profileJSON := ch.Profile
			if len(profileJSON) == 0 {
				profileJSON = json.RawMessage(`{}`)
			}
			// Normalise profile: if it's a plain string wrap it in {"description":"..."}
			if len(profileJSON) > 0 && profileJSON[0] == '"' {
				var s string
				if json.Unmarshal(profileJSON, &s) == nil {
					profileJSON, _ = json.Marshal(map[string]string{"description": s})
				}
			}
			chBatch.Queue(
				`INSERT INTO characters (id, project_id, name, role_type, profile, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
				 ON CONFLICT (project_id, name) DO NOTHING`,
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
		logger.Info("blueprint generation: new characters processed", zap.Int("count", len(parsed.Characters)))
	}

	// Foreshadowings: always additive — just insert new ones.
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
				 VALUES ($1, $2, $3, $4, $5, 'planned', NOW(), NOW())`,
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

	// Blueprint record: update with generated outlines.
	masterOutline := parsed.MasterOutline
	if len(masterOutline) == 0 {
		masterOutline = json.RawMessage(`{}`)
	}
	relationGraph := parsed.RelationGraph
	if len(relationGraph) == 0 {
		relationGraph = json.RawMessage(`{}`)
	}
	globalTimeline := parsed.GlobalTimeline
	if len(globalTimeline) == 0 {
		globalTimeline = json.RawMessage(`[]`)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE book_blueprints
		 SET status = 'draft', master_outline = $1, relation_graph = $2, global_timeline = $3, updated_at = NOW()
		 WHERE id = $4`,
		masterOutline, relationGraph, globalTimeline, bpID); err != nil {
		return fmt.Errorf("update blueprint content: %w", err)
	}

	// Volumes: replace existing draft volumes for this blueprint.
	if len(parsed.Volumes) > 0 {
		// Delete any existing volumes linked to this specific blueprint placeholder.
		if _, err := tx.Exec(ctx,
			`DELETE FROM volumes WHERE blueprint_id = $1`, bpID); err != nil {
			return fmt.Errorf("clear placeholder volumes: %w", err)
		}
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
		zap.Int("new_characters", len(parsed.Characters)),
		zap.Int("new_foreshadowings", len(parsed.Foreshadowings)),
		zap.Int("volumes", len(parsed.Volumes)))
	return nil
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
		}
	case "玄幻":
		points = []string{
			"- 修炼境界须清晰标注，成长不可一步登天",
			"- 战斗描写须结合力量体系，避免泛化",
			"- master_outline 须体现修炼突破的阶段感",
		}
	case "末世":
		points = []string{
			"- 资源匮乏和紧张感须贯穿全文规划",
			"- 异能/进化逻辑须与世界设定自洽",
			"- master_outline 须体现生存→建立据点→反攻的递进结构",
		}
	}

	if gt != nil && len(points) == 0 {
		// Generic genre — no extra hard constraints beyond the template already in context.
		return ""
	}

	return strings.Join(points, "\n")
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
