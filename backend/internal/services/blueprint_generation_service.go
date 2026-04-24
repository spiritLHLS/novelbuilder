package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

func (s *BlueprintService) doGenerateBlueprintWork(ctx context.Context, projectID, bpID, runID string, project models.Project, req models.GenerateBlueprintRequest) error {
	logger := s.logger.With(
		zap.String("project_id", projectID),
		zap.String("blueprint_id", bpID),
	)
	logger.Info("blueprint generation: gathering existing project data")

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
		if project.TargetWords > 0 {
			volumeCount = project.TargetWords / 100000
		}
		if volumeCount < 4 {
			volumeCount = 4
		}
	}

	chapterWordsMin := req.ChapterWordsMin
	chapterWordsMax := req.ChapterWordsMax
	if chapterWordsMin <= 0 {
		chapterWordsMin = 2000
	}
	if chapterWordsMax <= 0 {
		chapterWordsMax = project.ChapterWords
		if chapterWordsMax <= 0 {
			chapterWordsMax = 3500
		}
	}
	if chapterWordsMin > chapterWordsMax {
		chapterWordsMin, chapterWordsMax = chapterWordsMax, chapterWordsMin
	}
	avgChapterWords := (chapterWordsMin + chapterWordsMax) / 2

	var sb strings.Builder
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

	if existingWB != nil && len(existingWB.Content) > 4 {
		sb.WriteString("## 【现有世界观设定（请在此基础上延伸扩展，禁止修改已有字段值）】\n")
		sb.WriteString(string(existingWB.Content))
		sb.WriteString("\n\n")
	}

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

	if glossaryBlock != "" {
		sb.WriteString(glossaryBlock)
		sb.WriteString("\n")
	}

	contextSection := sb.String()
	worldBibleFields := buildWorldBibleFieldsHint(genre)

	hasExistingData := existingWB != nil || len(existingChars) > 0 || len(existingForeshadowings) > 0 || len(existingOutlines) > 0
	taskInstruction := "生成一套全新的完整整书资产包"
	if hasExistingData {
		taskInstruction = "在现有素材基础上完成并扩展整书资产包（不要删改用户已有内容，只补全缺失部分并新增内容）"
	}

	targetWordsWan := project.TargetWords / 10000
	if targetWordsWan == 0 {
		targetWordsWan = 50
	}
	estimatedTotalChapters := 0
	if avgChapterWords > 0 && project.TargetWords > 0 {
		estimatedTotalChapters = project.TargetWords / avgChapterWords
	}
	if estimatedTotalChapters <= 0 {
		estimatedTotalChapters = volumeCount * 30
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

	logger.Info("blueprint generation: writing data to database")
	tx, txErr := s.db.Begin(ctx)
	if txErr != nil {
		return fmt.Errorf("begin transaction: %w", txErr)
	}
	defer tx.Rollback(ctx)

	if len(parsed.WorldBible) > 4 {
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

	if len(parsed.Characters) > 0 {
		chBatch := &pgx.Batch{}
		for _, ch := range parsed.Characters {
			profileJSON := ch.Profile
			if len(profileJSON) == 0 {
				profileJSON = json.RawMessage(`{}`)
			}
			if len(profileJSON) > 0 && profileJSON[0] == '"' {
				var str string
				if json.Unmarshal(profileJSON, &str) == nil {
					profileJSON, _ = json.Marshal(map[string]string{"description": str})
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

	if len(parsed.Volumes) > 0 {
		newVolumeCount := len(parsed.Volumes)
		if _, err := tx.Exec(ctx,
			`UPDATE chapters SET volume_id = NULL
			 WHERE volume_id IN (
			     SELECT id FROM volumes WHERE project_id = $1 AND volume_num > $2
			 )`,
			projectID, newVolumeCount); err != nil {
			return fmt.Errorf("nullify excess chapter volume refs: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM volumes WHERE project_id = $1 AND volume_num > $2`,
			projectID, newVolumeCount); err != nil {
			return fmt.Errorf("delete excess volumes: %w", err)
		}
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

	if len(parsed.ChapterOutlines) > 0 {
		if _, err := tx.Exec(ctx,
			`DELETE FROM outlines WHERE project_id = $1 AND level = 'chapter'`,
			projectID); err != nil {
			return fmt.Errorf("delete existing chapter outlines: %w", err)
		}

		outlineBatch := &pgx.Batch{}
		for _, chOutline := range parsed.ChapterOutlines {
			title := chOutline.Title
			if title == "" {
				title = fmt.Sprintf("第%d章", chOutline.ChapterNum)
			}
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

	if _, err := tx.Exec(ctx,
		`UPDATE projects SET status = 'blueprint_generated', updated_at = NOW() WHERE id = $1`, projectID); err != nil {
		return fmt.Errorf("update project status: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit blueprint transaction: %w", err)
	}

	stepID, _ := s.wf.CreateStep(ctx, runID, "blueprint", "blueprint_gate", 0)
	s.wf.MarkStepGenerated(ctx, stepID, bpID)

	logger.Info("blueprint generation: completed successfully",
		zap.Int("new_characters", len(parsed.Characters)),
		zap.Int("new_foreshadowings", len(parsed.Foreshadowings)),
		zap.Int("volumes", len(parsed.Volumes)))
	return nil
}

func (s *BlueprintService) generateChapterOutlines(ctx context.Context, projectID string, volumeNum int, batchSize int, startChapter int) error {
	logger := s.logger.With(zap.String("project_id", projectID), zap.Int("volume_num", volumeNum), zap.Int("start_chapter", startChapter))

	var project models.Project
	if err := s.db.QueryRow(ctx, `SELECT id, title, genre, description, style_description FROM projects WHERE id = $1`, projectID).
		Scan(&project.ID, &project.Title, &project.Genre, &project.Description, &project.StyleDescription); err != nil {
		return fmt.Errorf("get project: %w", err)
	}

	bp, err := s.Get(ctx, projectID)
	if err != nil {
		return fmt.Errorf("get blueprint: %w", err)
	}
	if bp == nil {
		return fmt.Errorf("no blueprint found")
	}

	var volume models.Volume
	if err := s.db.QueryRow(ctx,
		`SELECT id, volume_num, title, chapter_start, chapter_end FROM volumes WHERE project_id = $1 AND volume_num = $2`,
		projectID, volumeNum).Scan(&volume.ID, &volume.VolumeNum, &volume.Title, &volume.ChapterStart, &volume.ChapterEnd); err != nil {
		return fmt.Errorf("get volume: %w", err)
	}

	totalChapters := volume.ChapterEnd - volume.ChapterStart + 1

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

	var nextChapterNum, endChapterNum int
	if startChapter > 0 {
		nextChapterNum = startChapter
		if nextChapterNum < volume.ChapterStart {
			nextChapterNum = volume.ChapterStart
		}
	} else {
		nextChapterNum = volume.ChapterStart
		for ch := volume.ChapterStart; ch <= volume.ChapterEnd; ch++ {
			if _, exists := existingOutlinesMap[ch]; !exists {
				nextChapterNum = ch
				break
			}
		}
	}

	if startChapter == 0 && len(existingOutlinesMap) >= totalChapters {
		logger.Info("all chapters already generated, use start_chapter to regenerate")
		return fmt.Errorf("all chapters already generated, set start_chapter to regenerate specific chapters")
	}

	if batchSize <= 0 {
		batchSize = 10
	}
	endChapterNum = nextChapterNum + batchSize - 1
	if endChapterNum > volume.ChapterEnd {
		endChapterNum = volume.ChapterEnd
	}
	actualBatchSize := endChapterNum - nextChapterNum + 1

	characters, _ := s.characters.List(ctx, projectID)
	worldBible, _ := s.worldBibles.Get(ctx, projectID)
	glossaryBlock := s.glossary.BuildPromptBlock(ctx, projectID)
	foreshadowings, _ := s.foreshadowings.List(ctx, projectID)

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

	previousVolumesText := ""
	futureVolumesText := ""
	currentVolumeOutlineFromMaster := ""
	if len(allVolumes) > 1 {
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

	masterOutlineFullText := extractTextFromJSON(bp.MasterOutline)
	if masterOutlineFullText != "" {
		currentVolumeOutlineFromMaster = extractVolumeSection(masterOutlineFullText, volumeNum, volume.Title)
	}

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

	existingOutlinesText := ""
	if len(existingOutlinesMap) > 0 {
		var outlineBuilder strings.Builder
		outlineBuilder.WriteString("\n---\n## 【本卷已生成章节大纲】\n")
		outlineBuilder.WriteString("以下是本卷前面已经生成的章节大纲，请确保后续章节与之承接连贯：\n\n")

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

	globalTimelineText := extractTextFromJSON(bp.GlobalTimeline)

	currentVolumeGuide := ""
	if currentVolumeOutlineFromMaster != "" {
		currentVolumeGuide = fmt.Sprintf("\n**本卷核心定位（来自总纲）：** %s\n", currentVolumeOutlineFromMaster)
	} else if masterOutlineFullText != "" {
		currentVolumeGuide = fmt.Sprintf("\n**整书总纲（仅参考本卷相关部分）：** %s\n", masterOutlineFullText)
	}

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
- 【自生伏笔】即使没有预定义的伏笔，也应在每2-3章中安排至少1个新的悬念/暗示性事件（如角色的异常行为、不经意的发现、背景中的反常细节），供后续章节回收或发展
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
		volume.Title, volume.ChapterStart, volume.ChapterEnd, totalChapters,
		nextChapterNum, endChapterNum, actualBatchSize,
		totalChapters,
		currentVolumeGuide,
		futureVolumesText,
		ctxBuilder.String(),
		previousVolumesText,
		existingOutlinesText,
		globalTimelineText,
		foreshadowingText,
		"",
		nextChapterNum, nextChapterNum,
		nextChapterNum+1, nextChapterNum+1,
		actualBatchSize,
		nextChapterNum, endChapterNum,
	)

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

	totalGeneratedNow := len(existingOutlinesMap) + len(parsed.ChapterOutlines)
	for _, chOutline := range parsed.ChapterOutlines {
		if _, existed := existingOutlinesMap[chOutline.ChapterNum]; existed {
			totalGeneratedNow--
		}
	}
	remainingChapters := totalChapters - totalGeneratedNow
	logger.Info("chapter outlines generated successfully",
		zap.Int("generated_this_batch", len(parsed.ChapterOutlines)),
		zap.Int("total_generated_in_volume", totalGeneratedNow),
		zap.Int("remaining_in_volume", remainingChapters),
		zap.Int("chapter_range_start", nextChapterNum),
		zap.Int("chapter_range_end", endChapterNum))

	s.autoAssignForeshadowingTimings(ctx, projectID, logger)

	return nil
}
