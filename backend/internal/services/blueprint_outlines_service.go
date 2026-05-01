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
