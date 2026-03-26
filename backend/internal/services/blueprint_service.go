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

	// Resolve chapter word-count range (request > project default > sensible defaults).
	chapterWordsMin := req.ChapterWordsMin
	chapterWordsMax := req.ChapterWordsMax
	if chapterWordsMin <= 0 {
		chapterWordsMin = 2000
	}
	if chapterWordsMax <= 0 {
		// Use project's chapter_words as max hint if set, else 3500.
		chapterWordsMax = project.ChapterWords
		if chapterWordsMax <= 0 {
			chapterWordsMax = 3500
		}
	}
	// Ensure min <= max.
	if chapterWordsMin > chapterWordsMax {
		chapterWordsMin, chapterWordsMax = chapterWordsMax, chapterWordsMin
	}
	// Midpoint used for total-chapter estimation.
	avgChapterWords := (chapterWordsMin + chapterWordsMax) / 2

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
	estimatedTotalChapters := 0
	if avgChapterWords > 0 && project.TargetWords > 0 {
		estimatedTotalChapters = project.TargetWords / avgChapterWords
	}
	if estimatedTotalChapters <= 0 {
		estimatedTotalChapters = volumeCount * 30 // fallback
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
- 计划卷数：%d卷（必须恰好 %d 卷，不得增减）
- 每章字数范围：%d～%d字（短故事弧可少于此范围，高潮章节可超出此范围，整体控制在此区间内）
- 全书目标字数：约%d万字
- 推算总章节数：约%d章（目标字数 ÷ 每章平均字数）
- 各卷章节数：由你根据剧情弧度自由决定，无需均匀分配；剧情节奏紧凑的卷可章节少、字数短，高潮卷可章节多、字数长

---
## 输出格式

请**严格以 JSON 格式**返回以下资产，JSON 之外不得有任何文字：

{
  "master_outline": "第1卷:主题/核心冲突/高潮点。第2卷:...（每卷一句，句号分隔，共 %d 卷）",
  "volumes": [
    {"title":"第一卷卷名","chapter_start":1,"chapter_end":章节数},
    {"title":"第二卷卷名","chapter_start":下一章节号,"chapter_end":章节数},
    ...（按此格式列出全部 %d 卷，每卷的chapter_start/chapter_end由你根据剧情弧度自由决定，章节连续不重叠）
  ],
  "relation_graph": "角色A-角色B:关系描述;角色C-角色D:关系描述（分号分隔每对关系）",
  "global_timeline": "序章:关键事件;第一卷末:关键事件;第二卷末:关键事件;...（分号分隔）",
  "foreshadowings": [{"content":"伏笔内容","embed_method":"explicit|implicit|symbolic","planned_embed_chapter":5,"planned_resolve_chapter":25,"priority":8}],
  "characters": [{"name":"角色名","role_type":"protagonist|supporting|antagonist|mentor|minor","profile":"角色描述"}],
  "world_bible": {%s}
}

**注意事项：**
- 本次生成**仅包含整体框架**，不需要生成详细的章节大纲（chapter_outlines）
- 章节大纲将在后续分批生成，避免JSON过大导致截断
- 重点关注：整体剧情结构、卷册划分、核心角色关系、关键时间线节点

**重要约束：**
%s
- volumes 数组必须恰好 %d 个元素，所有卷的章节首尾相连（第一卷chapter_start=1，最后一卷chapter_end=推算总章节数附近），章节连续不重叠
- 各卷章节数由你根据剧情自由规划（可多可少），不要求相同
- characters 中已存在角色无需重复，只列**新增**角色
- foreshadowings 中已存在伏笔无需重复，只列**新增**伏笔
- 【伏笔规划约束】每条伏笔必须指定 planned_embed_chapter（植入章节号）和 planned_resolve_chapter（回收章节号）：
  * 植入章节必须早于回收章节，两者之间至少间隔5章以上
  * 伏笔回收不可拖到最后一卷才集中处理，应分散在各卷中逐步回收
  * 每卷至少安排2-3条伏笔植入，1-2条伏笔回收（早期卷以植入为主，中后期植入与回收并行）
  * 重要伏笔（priority>=7）的回收需安排在高潮章节附近
- 所有内容须与已有世界观、角色、术语表保持一致
- 确保所有伏笔在大纲、时间线中安排铺垫与揭露
- 【节奏控制约束】剧情推进速度需合理：
  * 前20%%章节用于世界观建立、角色引入、核心矛盾铺垫，不可在此阶段引入过多势力和冲突
  * 中间60%%章节为主要冒险/发展阶段，每卷有独立子目标但串联主线
  * 最后20%%章节用于高潮、真相揭示和收尾，不可出现新的重大设定
  * 每卷的剧情密度应渐进增加：卷首铺垫轻松→卷中矛盾加剧→卷末高潮或转折
- 【角色成长约束】角色的能力、武器、装备、身份、地位等属性必须符合时间线渐进获得原则：
  * 角色初始profile只写**现状基础属性**（性格、外貌、当前身份、当前实力水平）
  * 所有需要"获得"的东西（神器、秘籍、师父传承、新身份、突破境界）必须在timeline/章节大纲中明确标注**获得时间点和获得方式**
  * 禁止给角色"来历不明"的能力/装备（如：角色profile写"拥有上古神剑"但timeline/章节大纲中无获得过程）
  * 禁止一次性给予角色大量资源（如：一章内获得3件法宝+2个技能+1个新身份）
  * 成长必须有代价和过程：获得新能力需要明确的触发事件（奇遇、战斗突破、师父传授、任务奖励等），体现在章节事件中
  * 主角/重要角色的每次实力提升、装备获得都应在global_timeline和对应章节大纲中同时体现
`,
		genre,
		taskInstruction,
		contextSection,
		project.Title,
		genre,
		idea,
		project.StyleDescription,
		volumeCount, volumeCount,
		chapterWordsMin, chapterWordsMax,
		targetWordsWan,
		estimatedTotalChapters,
		volumeCount,
		volumeCount,
		worldBibleFields,
		buildGenreConstraints(genre, genreTemplate),
		volumeCount,
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

	content := extractBlueprintJSON(resp.Content)

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
			Content               string `json:"content"`
			EmbedMethod           string `json:"embed_method"`
			Priority              int    `json:"priority"`
			PlannedEmbedChapter   int    `json:"planned_embed_chapter"`
			PlannedResolveChapter int    `json:"planned_resolve_chapter"`
		} `json:"foreshadowings"`
		Volumes []struct {
			Title        string `json:"title"`
			ChapterStart int    `json:"chapter_start"`
			ChapterEnd   int    `json:"chapter_end"`
		} `json:"volumes"`
		ChapterOutlines []struct {
			ChapterNum int      `json:"chapter_num"`
			Title      string   `json:"title"`
			Events     []string `json:"events"`
		} `json:"chapter_outlines"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		logger.Warn("failed to parse blueprint JSON, storing raw response",
			zap.Error(err),
			zap.String("raw_prefix", content[:min(200, len(content))]))
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
				`INSERT INTO foreshadowings (id, project_id, content, embed_method, priority, planned_embed_chapter, planned_resolve_chapter, status, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, 'planned', NOW(), NOW())`,
				uuid.New().String(), projectID, fs.Content, embedMethod, priority, fs.PlannedEmbedChapter, fs.PlannedResolveChapter)
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
	// Use SQL NULL for missing fields so the frontend can reliably detect them.
	masterOutline := parsed.MasterOutline
	if len(masterOutline) == 0 {
		masterOutline = json.RawMessage(`null`)
	}
	relationGraph := parsed.RelationGraph
	if len(relationGraph) == 0 {
		relationGraph = json.RawMessage(`null`)
	}
	globalTimeline := parsed.GlobalTimeline
	if len(globalTimeline) == 0 {
		globalTimeline = json.RawMessage(`null`)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE book_blueprints
		 SET status = 'draft', master_outline = $1, relation_graph = $2, global_timeline = $3, updated_at = NOW()
		 WHERE id = $4`,
		masterOutline, relationGraph, globalTimeline, bpID); err != nil {
		return fmt.Errorf("update blueprint content: %w", err)
	}

	// Volumes: upsert new volumes and delete any excess from a previous generation.
	if len(parsed.Volumes) > 0 {
		newVolumeCount := len(parsed.Volumes)
		// First, null out the volume_id on chapters that belong to volumes we are
		// about to remove (those with a higher volume_num than the new total).
		if _, err := tx.Exec(ctx,
			`UPDATE chapters SET volume_id = NULL
			 WHERE volume_id IN (
			     SELECT id FROM volumes WHERE project_id = $1 AND volume_num > $2
			 )`,
			projectID, newVolumeCount); err != nil {
			return fmt.Errorf("nullify excess chapter volume refs: %w", err)
		}
		// Delete volumes beyond the new count.
		if _, err := tx.Exec(ctx,
			`DELETE FROM volumes WHERE project_id = $1 AND volume_num > $2`,
			projectID, newVolumeCount); err != nil {
			return fmt.Errorf("delete excess volumes: %w", err)
		}
		// Upsert all new volumes — ON CONFLICT preserves the row id so existing
		// chapter → volume foreign-key references remain valid.
		volBatch := &pgx.Batch{}
		for i, vol := range parsed.Volumes {
			title := vol.Title
			if title == "" {
				title = fmt.Sprintf("第%d卷", i+1)
			}
			volBatch.Queue(
				`INSERT INTO volumes (id, project_id, volume_num, title, blueprint_id, chapter_start, chapter_end, status, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, 'draft', NOW(), NOW())
				 ON CONFLICT (project_id, volume_num) DO UPDATE SET
				     title         = EXCLUDED.title,
				     blueprint_id  = EXCLUDED.blueprint_id,
				     chapter_start = EXCLUDED.chapter_start,
				     chapter_end   = EXCLUDED.chapter_end,
				     status        = 'draft',
				     updated_at    = NOW()`,
				uuid.New().String(), projectID, i+1, title, bpID, vol.ChapterStart, vol.ChapterEnd)
		}
		br := tx.SendBatch(ctx, volBatch)
		for i := range parsed.Volumes {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("upsert volume %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("volume batch close: %w", err)
		}
	}

	// Chapter outlines: delete existing chapter-level outlines and insert new ones.
	if len(parsed.ChapterOutlines) > 0 {
		// First delete all existing chapter-level outlines for this project.
		if _, err := tx.Exec(ctx,
			`DELETE FROM outlines WHERE project_id = $1 AND level = 'chapter'`,
			projectID); err != nil {
			return fmt.Errorf("delete existing chapter outlines: %w", err)
		}

		// Insert new chapter outlines.
		outlineBatch := &pgx.Batch{}
		for _, chOutline := range parsed.ChapterOutlines {
			title := chOutline.Title
			if title == "" {
				title = fmt.Sprintf("第%d章", chOutline.ChapterNum)
			}
			// Store events as JSON in the content field.
			contentJSON, _ := json.Marshal(map[string]interface{}{
				"events": chOutline.Events,
			})
			outlineBatch.Queue(
				`INSERT INTO outlines (id, project_id, level, order_num, title, content, created_at, updated_at)
				 VALUES ($1, $2, 'chapter', $3, $4, $5, NOW(), NOW())`,
				uuid.New().String(), projectID, chOutline.ChapterNum, title, contentJSON)
		}
		br := tx.SendBatch(ctx, outlineBatch)
		for i := range parsed.ChapterOutlines {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("insert chapter outline %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("chapter outline batch close: %w", err)
		}
		logger.Info("blueprint generation: chapter outlines stored", zap.Int("count", len(parsed.ChapterOutlines)))
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
	logger := s.logger.With(zap.String("project_id", projectID), zap.Int("volume_num", volumeNum), zap.Int("start_chapter", startChapter))

	// Get project info
	var project models.Project
	if err := s.db.QueryRow(ctx, `SELECT id, title, genre, description, style_description FROM projects WHERE id = $1`, projectID).
		Scan(&project.ID, &project.Title, &project.Genre, &project.Description, &project.StyleDescription); err != nil {
		return fmt.Errorf("get project: %w", err)
	}

	// Get blueprint
	bp, err := s.Get(ctx, projectID)
	if err != nil {
		return fmt.Errorf("get blueprint: %w", err)
	}
	if bp == nil {
		return fmt.Errorf("no blueprint found")
	}

	// Get volume info
	var volume models.Volume
	if err := s.db.QueryRow(ctx,
		`SELECT id, volume_num, title, chapter_start, chapter_end FROM volumes WHERE project_id = $1 AND volume_num = $2`,
		projectID, volumeNum).Scan(&volume.ID, &volume.VolumeNum, &volume.Title, &volume.ChapterStart, &volume.ChapterEnd); err != nil {
		return fmt.Errorf("get volume: %w", err)
	}

	totalChapters := volume.ChapterEnd - volume.ChapterStart + 1

	// Get existing chapter outlines for this volume (single query, no N+1)
	existingOutlinesMap := make(map[int]struct {
		ChapterNum int
		Title      string
		Content    json.RawMessage
	})
	rows, err := s.db.Query(ctx,
		`SELECT order_num, title, content FROM outlines 
		 WHERE project_id = $1 AND level = 'chapter' 
		 AND order_num >= $2 AND order_num <= $3
		 ORDER BY order_num`,
		projectID, volume.ChapterStart, volume.ChapterEnd)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var outline struct {
				ChapterNum int
				Title      string
				Content    json.RawMessage
			}
			if err := rows.Scan(&outline.ChapterNum, &outline.Title, &outline.Content); err == nil {
				existingOutlinesMap[outline.ChapterNum] = outline
			}
		}
	}

	// Determine the generation range
	var nextChapterNum, endChapterNum int
	if startChapter > 0 {
		// Explicit start: regenerate from specified chapter
		nextChapterNum = startChapter
		if nextChapterNum < volume.ChapterStart {
			nextChapterNum = volume.ChapterStart
		}
	} else {
		// Auto-continue: find the first missing chapter or start from beginning
		nextChapterNum = volume.ChapterStart
		for ch := volume.ChapterStart; ch <= volume.ChapterEnd; ch++ {
			if _, exists := existingOutlinesMap[ch]; !exists {
				nextChapterNum = ch
				break
			}
		}
	}

	// Check if all chapters are already generated (only when auto-continue)
	if startChapter == 0 && len(existingOutlinesMap) >= totalChapters {
		logger.Info("all chapters already generated, use start_chapter to regenerate")
		return fmt.Errorf("all chapters already generated, set start_chapter to regenerate specific chapters")
	}

	// Calculate end chapter for this batch
	if batchSize <= 0 {
		batchSize = 10
	}
	endChapterNum = nextChapterNum + batchSize - 1
	if endChapterNum > volume.ChapterEnd {
		endChapterNum = volume.ChapterEnd
	}
	actualBatchSize := endChapterNum - nextChapterNum + 1

	// Get existing assets for context (single queries, no N+1)
	characters, _ := s.characters.List(ctx, projectID)
	worldBible, _ := s.worldBibles.Get(ctx, projectID)
	glossaryBlock := s.glossary.BuildPromptBlock(ctx, projectID)
	foreshadowings, _ := s.foreshadowings.List(ctx, projectID)

	// Get ALL volumes for future context (single query)
	var allVolumes []models.Volume
	volRows, err := s.db.Query(ctx,
		`SELECT id, volume_num, title, chapter_start, chapter_end FROM volumes WHERE project_id = $1 ORDER BY volume_num`, projectID)
	if err == nil {
		defer volRows.Close()
		for volRows.Next() {
			var v models.Volume
			if err := volRows.Scan(&v.ID, &v.VolumeNum, &v.Title, &v.ChapterStart, &v.ChapterEnd); err == nil {
				allVolumes = append(allVolumes, v)
			}
		}
	}

	// Build previous volumes summary for continuity and future volumes boundary markers
	previousVolumesText := ""
	futureVolumesText := ""
	currentVolumeOutlineFromMaster := ""
	if len(allVolumes) > 1 {
		// ── Previous volumes: show completed chapter titles for story continuity ──
		var prevBuilder strings.Builder
		hasPrev := false
		for _, v := range allVolumes {
			if v.VolumeNum < volumeNum {
				if !hasPrev {
					prevBuilder.WriteString("\n---\n## 【前卷已完成剧情回顾】\n")
					prevBuilder.WriteString("以下是前面各卷已完成的故事进度，请确保本卷大纲与之衔接、不重复已有情节：\n\n")
					hasPrev = true
				}
				prevBuilder.WriteString(fmt.Sprintf("### %s（第%d～%d章）\n", v.Title, v.ChapterStart, v.ChapterEnd))
				prevRows, pErr := s.db.Query(ctx,
					`SELECT order_num, title FROM outlines WHERE project_id = $1 AND level = 'chapter' AND order_num >= $2 AND order_num <= $3 ORDER BY order_num`,
					projectID, v.ChapterStart, v.ChapterEnd)
				if pErr == nil {
					for prevRows.Next() {
						var oNum int
						var oTitle string
						if prevRows.Scan(&oNum, &oTitle) == nil {
							prevBuilder.WriteString(fmt.Sprintf("  第%d章：%s\n", oNum, oTitle))
						}
					}
					prevRows.Close()
				}
				prevBuilder.WriteString("\n")
			}
		}
		if hasPrev {
			previousVolumesText = prevBuilder.String()
		}

		// ── Future volumes: ONLY volume titles as boundary fence (no chapter details!) ──
		// Showing detailed future chapter outlines causes the AI to bleed future plot into current volume.
		var futureBuilder strings.Builder
		hasFuture := false
		for _, v := range allVolumes {
			if v.VolumeNum > volumeNum {
				if !hasFuture {
					futureBuilder.WriteString("\n---\n## 【后续卷目标题（禁入区域）】\n")
					futureBuilder.WriteString("以下卷目的剧情属于后续内容，本卷**严禁涉及**：\n")
					hasFuture = true
				}
				futureBuilder.WriteString(fmt.Sprintf("- 第%d卷：%s（第%d～%d章）\n", v.VolumeNum, v.Title, v.ChapterStart, v.ChapterEnd))
			}
		}
		if hasFuture {
			futureBuilder.WriteString("\n⚠️ 上述卷目的核心剧情、关键冲突、角色转变、能力突破等一律不得出现在本卷。仅允许极轻微的氛围暗示。\n")
			futureVolumesText = futureBuilder.String()
		}
	}

	// ── Extract current volume's portion from master outline ──
	// Try to find the volume-specific text to use as the primary guiding framework
	masterOutlineFullText := extractTextFromJSON(bp.MasterOutline)
	if masterOutlineFullText != "" {
		currentVolumeOutlineFromMaster = extractVolumeSection(masterOutlineFullText, volumeNum, volume.Title)
	}

	// Build foreshadowing timeline for this volume's chapter range
	foreshadowingText := ""
	if len(foreshadowings) > 0 {
		var fsBuilder strings.Builder
		fsBuilder.WriteString("\n---\n## 【伏笔时间线】\n")

		var toEmbed, toResolve, available []models.Foreshadowing
		for _, fs := range foreshadowings {
			if fs.PlannedEmbedChapter >= nextChapterNum && fs.PlannedEmbedChapter <= endChapterNum {
				toEmbed = append(toEmbed, fs)
			}
			if fs.PlannedResolveChapter >= nextChapterNum && fs.PlannedResolveChapter <= endChapterNum {
				toResolve = append(toResolve, fs)
			}
			if fs.Status == "planted" || fs.Status == "planned" {
				available = append(available, fs)
			}
		}

		if len(toEmbed) > 0 {
			fsBuilder.WriteString("\n### 本批次需要【植入】的伏笔：\n")
			for _, fs := range toEmbed {
				fsBuilder.WriteString(fmt.Sprintf("- 第%d章植入：%s（方式：%s，优先级：%d）\n",
					fs.PlannedEmbedChapter, fs.Content, fs.EmbedMethod, fs.Priority))
			}
		}
		if len(toResolve) > 0 {
			fsBuilder.WriteString("\n### 本批次需要【回收/揭示】的伏笔：\n")
			for _, fs := range toResolve {
				fsBuilder.WriteString(fmt.Sprintf("- 第%d章回收：%s\n",
					fs.PlannedResolveChapter, fs.Content))
			}
		}
		if len(available) > 0 {
			fsBuilder.WriteString("\n### 全部未完结伏笔一览（供参考）：\n")
			for _, fs := range available {
				embedInfo := ""
				if fs.PlannedEmbedChapter > 0 {
					embedInfo = fmt.Sprintf("，计划第%d章植入", fs.PlannedEmbedChapter)
				}
				resolveInfo := ""
				if fs.PlannedResolveChapter > 0 {
					resolveInfo = fmt.Sprintf("，计划第%d章回收", fs.PlannedResolveChapter)
				}
				fsBuilder.WriteString(fmt.Sprintf("- [%s] %s（优先级%d%s%s）\n",
					fs.Status, fs.Content, fs.Priority, embedInfo, resolveInfo))
			}
		}
		foreshadowingText = fsBuilder.String()
	}

	// Build context
	var ctxBuilder strings.Builder
	if worldBible != nil && len(worldBible.Content) > 4 {
		ctxBuilder.WriteString("## 【世界观设定】\n")
		ctxBuilder.WriteString(string(worldBible.Content))
		ctxBuilder.WriteString("\n\n")
	}
	if len(characters) > 0 {
		ctxBuilder.WriteString("## 【角色列表】\n")
		for i, ch := range characters {
			if i >= 20 {
				break
			}
			ctxBuilder.WriteString(fmt.Sprintf("- **%s**（%s）：%s\n", ch.Name, ch.RoleType, string(ch.Profile)))
		}
		ctxBuilder.WriteString("\n")
	}
	if glossaryBlock != "" {
		ctxBuilder.WriteString(glossaryBlock)
		ctxBuilder.WriteString("\n")
	}

	// Add existing chapter outlines before the generation range for continuity
	existingOutlinesText := ""
	if len(existingOutlinesMap) > 0 {
		var outlineBuilder strings.Builder
		outlineBuilder.WriteString("\n---\n## 【本卷已生成章节大纲】\n")
		outlineBuilder.WriteString("以下是本卷前面已经生成的章节大纲，请确保后续章节与之承接连贯：\n\n")

		// Only show outlines before the current generation start
		for ch := volume.ChapterStart; ch < nextChapterNum; ch++ {
			if outline, exists := existingOutlinesMap[ch]; exists {
				var contentData map[string]interface{}
				if err := json.Unmarshal(outline.Content, &contentData); err == nil {
					if events, ok := contentData["events"].([]interface{}); ok {
						outlineBuilder.WriteString(fmt.Sprintf("**第%d章：%s**\n", outline.ChapterNum, outline.Title))
						for _, event := range events {
							if eventStr, ok := event.(string); ok {
								outlineBuilder.WriteString(fmt.Sprintf("  - %s\n", eventStr))
							}
						}
						outlineBuilder.WriteString("\n")
					}
				}
			}
		}
		existingOutlinesText = outlineBuilder.String()
	}

	// Extract text from blueprint fields
	globalTimelineText := extractTextFromJSON(bp.GlobalTimeline)

	// Build the current volume's guiding outline
	currentVolumeGuide := ""
	if currentVolumeOutlineFromMaster != "" {
		currentVolumeGuide = fmt.Sprintf("\n**本卷核心定位（来自总纲）：** %s\n", currentVolumeOutlineFromMaster)
	} else if masterOutlineFullText != "" {
		// Fallback: show full master outline but wrapped with scope emphasis
		currentVolumeGuide = fmt.Sprintf("\n**整书总纲（仅参考本卷相关部分）：** %s\n", masterOutlineFullText)
	}

	// Build prompt with strong volume scoping
	prompt := fmt.Sprintf(`你是一位资深小说策划编辑，擅长%s类型的长篇小说规划。

当前任务：为《%s》的【%s】生成详细的章节大纲。

## ⚠️ 【本卷剧情范围锁定 — 最高优先级硬约束】
- **本卷：%s（第%d章～第%d章，共%d章）**
- **本次生成范围：第%d章～第%d章（%d章）**
- **绝对红线：本卷章节大纲只允许包含属于本卷范围的剧情事件，任何后续卷目的核心冲突、关键转折、角色蜕变、能力突破等一律不得出现。**
- **剧情进度控制：本卷的情节推进速度必须匹配本卷的章节容量（%d章），不可试图在本卷内完成整卷以外的剧情。**
%s
%s
---
## 项目世界观与角色
%s
%s
---
## 本卷框架
%s
**全局时间线（截至本卷）：** %s
%s%s
---
## 输出格式

请**严格以 JSON 格式**返回章节大纲数组，JSON 之外不得有任何文字：

{
  "chapter_outlines": [
    {"chapter_num": %d, "title": "第%d章标题", "events": ["事件1描述（50字内）", "事件2描述（50字内）"]},
    {"chapter_num": %d, "title": "第%d章标题", "events": ["事件1描述"]},
    ...（共%d个章节）
  ]
}

**约束要求：**
- ⚠️ 每章最多1～3个核心事件（绝对上限3个，不可超过），事件描述简洁（50字以内）
- 只描述实际发生的情节，不做总结或展望
- 章节事件应体现剧情推进：人物互动、冲突发展、信息揭露、场景转换
- 章节标题符合网文命名风格（如："初遇"、"暗流涌动"、"破局"）
- **必须与本卷前面已生成的章节承接连贯，剧情自然过渡**
- **严禁同一卷内出现情节雷同、重复或相似的章节**（如：多次出现"酒宴初遇"、"化险为夷"等类似桥段）
- **每章情节必须独特且多样化**：避免使用相同的冲突模式、场景设置或事件类型
- 确保与整书大纲和时间线保持一致
- 角色能力获得需符合时间线，不可一次性获得多项能力
- 【角色/道具来源约束】每章出现的角色必须来自角色列表或在本卷此前章节中已登场；新角色首次出场必须在events中注明身份来源（如"酒馆老板王五——镇上老住户"）。道具/法宝/武器首次出场必须注明来源（战利品/购买/赠予/祖传等），禁止凭空出现"一把剑""一件法宝"等无来源物品。
- 【伏笔约束】如果伏笔时间线中指定了本批次需要植入或回收的伏笔，必须在对应章节的events中体现（植入：安排暗示性/铺垫性事件；回收：安排揭示/呼应事件）
- 【节奏约束】
  * 卷首1-2章以铺垫、引入新线索为主，节奏稍缓
  * 卷中章节逐步加快节奏，冲突逐章升级
  * 卷末1-2章为本卷高潮或悬念收尾，节奏紧凑
  * 单章不可堆叠超过2个重大事件（如战斗+突破+获宝+拜师不可在同一章）
  * 日常/修炼/旅途类章节与高潮/冲突类章节应交替出现，避免连续多章都是打斗或连续多章都是日常
- 【卷边界硬约束 — 再次强调】
  * 本次输出的所有chapter_num必须在第%d章～第%d章范围内
  * 本卷只推进本卷应有的剧情线，不得"加速叙事"把后续卷的内容提前消费
  * 如果对后续卷有所铺垫，只允许用一句模糊暗示，不允许出现具体事件
  * 宁可本卷剧情推进偏慢、场景细节丰富，也不可贪多求快塞入过多事件
`,
		project.Genre,
		project.Title,
		volume.Title,
		// Volume scope lock section
		volume.Title, volume.ChapterStart, volume.ChapterEnd, totalChapters,
		nextChapterNum, endChapterNum, actualBatchSize,
		totalChapters,
		currentVolumeGuide,
		futureVolumesText,
		// World context
		ctxBuilder.String(),
		previousVolumesText,
		// Volume framework
		existingOutlinesText,
		globalTimelineText,
		foreshadowingText,
		"", // placeholder for additional volume context (unused now)
		// JSON format
		nextChapterNum, nextChapterNum,
		nextChapterNum+1, nextChapterNum+1,
		actualBatchSize,
		// Final boundary constraint
		nextChapterNum, endChapterNum,
	)

	// Call AI
	logger.Info("generating chapter outlines",
		zap.Int("next_chapter", nextChapterNum),
		zap.Int("end_chapter", endChapterNum),
		zap.Int("batch_size", actualBatchSize),
		zap.Int("existing_in_volume", len(existingOutlinesMap)),
		zap.Bool("is_regeneration", startChapter > 0))
	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task:     "chapter_outline_generation",
		Messages: []gateway.ChatMessage{{Role: "user", Content: prompt}},
	})
	if err != nil {
		logger.Error("AI generation failed", zap.Error(err))
		return fmt.Errorf("AI generation failed: %w", err)
	}

	content := extractBlueprintJSON(resp.Content)
	logger.Info("AI response length", zap.Int("content_length", len(content)))

	var parsed struct {
		ChapterOutlines []struct {
			ChapterNum int      `json:"chapter_num"`
			Title      string   `json:"title"`
			Events     []string `json:"events"`
		} `json:"chapter_outlines"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		previewLen := 500
		if len(content) < previewLen {
			previewLen = len(content)
		}
		logger.Error("failed to parse chapter outlines",
			zap.Error(err),
			zap.String("content_preview", content[:previewLen]))
		return fmt.Errorf("parse chapter outlines (JSON invalid): %w", err)
	}

	if len(parsed.ChapterOutlines) == 0 {
		return fmt.Errorf("no chapter outlines generated")
	}

	// ── Post-generation validation: filter chapters outside volume/batch range & cap events ──
	validOutlines := make([]struct {
		ChapterNum int      `json:"chapter_num"`
		Title      string   `json:"title"`
		Events     []string `json:"events"`
	}, 0, len(parsed.ChapterOutlines))
	const maxEventsPerChapter = 3
	for _, chOutline := range parsed.ChapterOutlines {
		if chOutline.ChapterNum < volume.ChapterStart || chOutline.ChapterNum > volume.ChapterEnd {
			logger.Warn("filtered out-of-volume chapter outline (AI generated chapter outside volume range)",
				zap.Int("chapter_num", chOutline.ChapterNum),
				zap.String("title", chOutline.Title),
				zap.Int("volume_start", volume.ChapterStart),
				zap.Int("volume_end", volume.ChapterEnd))
			continue
		}
		if chOutline.ChapterNum < nextChapterNum || chOutline.ChapterNum > endChapterNum {
			logger.Warn("filtered out-of-batch chapter outline",
				zap.Int("chapter_num", chOutline.ChapterNum),
				zap.Int("batch_start", nextChapterNum),
				zap.Int("batch_end", endChapterNum))
			continue
		}
		// Hard cap: max 3 events per chapter. Trim excess events to prevent word count overflow.
		if len(chOutline.Events) > maxEventsPerChapter {
			logger.Warn("trimmed excess events from chapter outline",
				zap.Int("chapter_num", chOutline.ChapterNum),
				zap.Int("original_events", len(chOutline.Events)),
				zap.Int("max_events", maxEventsPerChapter))
			chOutline.Events = chOutline.Events[:maxEventsPerChapter]
		}
		validOutlines = append(validOutlines, chOutline)
	}
	if len(validOutlines) < len(parsed.ChapterOutlines) {
		logger.Warn("filtered invalid chapter outlines",
			zap.Int("original_count", len(parsed.ChapterOutlines)),
			zap.Int("valid_count", len(validOutlines)))
	}
	parsed.ChapterOutlines = validOutlines

	if len(parsed.ChapterOutlines) == 0 {
		return fmt.Errorf("no valid chapter outlines after filtering (all generated chapters were outside volume range %d-%d)",
			volume.ChapterStart, volume.ChapterEnd)
	}

	// Insert into database
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, chOutline := range parsed.ChapterOutlines {
		title := chOutline.Title
		if title == "" {
			title = fmt.Sprintf("第%d章", chOutline.ChapterNum)
		}
		contentJSON, _ := json.Marshal(map[string]interface{}{"events": chOutline.Events})

		// Upsert to allow regeneration
		batch.Queue(
			`INSERT INTO outlines (id, project_id, level, order_num, title, content, created_at, updated_at)
			 VALUES ($1, $2, 'chapter', $3, $4, $5, NOW(), NOW())
			 ON CONFLICT (project_id, level, order_num) DO UPDATE SET
			     title = EXCLUDED.title,
			     content = EXCLUDED.content,
			     updated_at = NOW()`,
			uuid.New().String(), projectID, chOutline.ChapterNum, title, contentJSON)
	}

	br := tx.SendBatch(ctx, batch)
	for i := range parsed.ChapterOutlines {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return fmt.Errorf("insert chapter outline %d: %w", i, err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("batch close: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// Calculate progress (count total generated in volume after this batch)
	totalGeneratedNow := len(existingOutlinesMap) + len(parsed.ChapterOutlines)
	// Dedup: if we regenerated existing chapters, don't double-count
	for _, chOutline := range parsed.ChapterOutlines {
		if _, existed := existingOutlinesMap[chOutline.ChapterNum]; existed {
			totalGeneratedNow-- // was already counted in existingOutlinesMap
		}
	}
	remainingChapters := totalChapters - totalGeneratedNow
	logger.Info("chapter outlines generated successfully",
		zap.Int("generated_this_batch", len(parsed.ChapterOutlines)),
		zap.Int("total_generated_in_volume", totalGeneratedNow),
		zap.Int("remaining_in_volume", remainingChapters),
		zap.Int("chapter_range_start", nextChapterNum),
		zap.Int("chapter_range_end", endChapterNum))

	// Auto-assign foreshadowing planned chapters for any foreshadowings missing them.
	// Runs synchronously but errors are non-fatal — the outline task already succeeded.
	s.autoAssignForeshadowingTimings(ctx, projectID, logger)

	return nil
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
