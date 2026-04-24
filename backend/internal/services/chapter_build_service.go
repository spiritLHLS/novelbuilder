package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

func (s *ChapterService) settleChapterState(ctx context.Context, projectID, chapterID, content string) {
	ctx = contextWithLLMSession(ctx, nil, fmt.Sprintf("chapter_state:%s", chapterID))

	// ── 1. Fetch all characters and active foreshadowings in two queries (no N+1) ──
	type charInfo struct {
		id   string
		name string
	}
	var chars []charInfo
	charRows, err := s.db.Query(ctx,
		`SELECT id, name FROM characters WHERE project_id = $1`, projectID)
	if err != nil {
		s.logger.Warn("settle: failed to load characters", zap.Error(err))
		return
	}
	defer charRows.Close()
	for charRows.Next() {
		var ci charInfo
		if err := charRows.Scan(&ci.id, &ci.name); err != nil {
			continue
		}
		chars = append(chars, ci)
	}

	type fsInfo struct {
		id      string
		content string
		status  string
	}
	var foreshadowings []fsInfo
	fsRows, err := s.db.Query(ctx,
		`SELECT id, content, status FROM foreshadowings WHERE project_id = $1 AND status IN ('planned','planted')`, projectID)
	if err != nil {
		s.logger.Warn("settle: failed to load foreshadowings", zap.Error(err))
		return
	}
	defer fsRows.Close()
	for fsRows.Next() {
		var fi fsInfo
		if err := fsRows.Scan(&fi.id, &fi.content, &fi.status); err != nil {
			continue
		}
		foreshadowings = append(foreshadowings, fi)
	}

	if len(chars) == 0 && len(foreshadowings) == 0 {
		return
	}

	// ── 2. Build structured prompt for LLM settlement analysis ──────────────
	charNames := make([]string, len(chars))
	for i, c := range chars {
		charNames[i] = c.name
	}
	fsContents := make([]string, len(foreshadowings))
	fsStatusMap := make(map[string]string, len(foreshadowings))
	for i, f := range foreshadowings {
		fsContents[i] = fmt.Sprintf("[%s] %s", f.status, f.content)
		fsStatusMap[f.content] = f.status
	}

	truncated := content
	if utf8.RuneCountInString(truncated) > 4000 {
		runes := []rune(truncated)
		truncated = string(runes[:4000])
	}

	systemMsg := `你是小说状态追踪系统。分析章节内容，提取状态变化。必须以纯JSON格式回复，不要包含其他文本。`
	userMsg := fmt.Sprintf(`角色列表：%v
待处理伏笔（带状态标记）：%v

章节内容（节选）：
%s

请分析后输出如下JSON（只输出JSON，不要解释）：
{
  "character_states": [
    {"name": "角色名（必须来自上面的列表）", "current_state": "状态描述"}
  ],
  "planted_foreshadowings": [
    "本章中被植入/铺垫的伏笔内容（必须来自上面状态为planned的伏笔，精确匹配原文）"
  ],
  "resolved_foreshadowings": [
    "本章中被揭示/回收的伏笔内容（必须来自上面状态为planted的伏笔，精确匹配原文）"
  ]
}

说明：
- planted_foreshadowings：指本章首次在文中出现暗示或铺垫的伏笔（从planned变为planted）
- resolved_foreshadowings：指本章明确揭示或解决了之前已植入的伏笔（从planted变为resolved）
- 只有文中确实出现了相关描写才能标记，不要猜测`, charNames, fsContents, truncated)

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task:      "state_settlement",
		MaxTokens: 800,
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: systemMsg},
			{Role: "user", Content: userMsg},
		},
	})
	if err != nil {
		s.logger.Warn("settle: LLM call failed", zap.Error(err))
		return
	}

	// ── 3. Parse LLM response ────────────────────────────────────────────────
	var settlement struct {
		CharacterStates []struct {
			Name         string `json:"name"`
			CurrentState string `json:"current_state"`
		} `json:"character_states"`
		PlantedForeshadowings  []string `json:"planted_foreshadowings"`
		ResolvedForeshadowings []string `json:"resolved_foreshadowings"`
	}
	raw := extractJSON(resp.Content)
	if err := json.Unmarshal([]byte(raw), &settlement); err != nil {
		s.logger.Warn("settle: failed to parse LLM response", zap.Error(err), zap.String("raw", resp.Content[:min(len(resp.Content), 200)]))
		return
	}

	// ── 4. Build lookup maps from fetched data (O(n) not O(n²)) ─────────────
	charByName := make(map[string]string, len(chars))
	for _, c := range chars {
		charByName[c.name] = c.id
	}
	fsByContent := make(map[string]string, len(foreshadowings))
	for _, f := range foreshadowings {
		fsByContent[f.content] = f.id
	}

	// ── 5. Apply all updates in a single pgx.Batch (one short transaction) ───
	batch := &pgx.Batch{}
	for _, cs := range settlement.CharacterStates {
		cid, ok := charByName[cs.Name]
		if !ok || cs.CurrentState == "" {
			continue
		}
		stateJSON, _ := json.Marshal(map[string]string{"summary": cs.CurrentState})
		batch.Queue(
			`UPDATE characters SET current_state = $1, updated_at = NOW() WHERE id = $2`,
			stateJSON, cid)
	}
	for _, fsContent := range settlement.ResolvedForeshadowings {
		fid, ok := fsByContent[fsContent]
		if !ok {
			continue
		}
		batch.Queue(
			`UPDATE foreshadowings SET status = 'resolved', resolve_chapter_id = $2, updated_at = NOW() WHERE id = $1`,
			fid, chapterID)
	}
	for _, fsContent := range settlement.PlantedForeshadowings {
		fid, ok := fsByContent[fsContent]
		if !ok {
			continue
		}
		// Only mark as planted if currently planned (don't re-plant already planted ones)
		batch.Queue(
			`UPDATE foreshadowings SET status = 'planted', embed_chapter_id = $2, updated_at = NOW() WHERE id = $1 AND status = 'planned'`,
			fid, chapterID)
	}

	if batch.Len() == 0 {
		return
	}

	br := s.db.SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			s.logger.Warn("settle: batch exec failed", zap.Error(err))
		}
	}
	s.logger.Info("chapter state settled",
		zap.String("chapter_id", chapterID),
		zap.Int("char_updates", len(settlement.CharacterStates)),
		zap.Int("fs_planted", len(settlement.PlantedForeshadowings)),
		zap.Int("fs_resolved", len(settlement.ResolvedForeshadowings)))

	// ── 6. Auto-extract potential new foreshadowings from chapter content ─────
	s.extractAutoForeshadowings(ctx, projectID, chapterID, content)

	// ── 7. Update volume arc summary ──────────────────────────────────────────
	s.updateVolumeArcSummary(ctx, projectID, chapterID, content)
}

// extractAutoForeshadowings uses the LLM to detect potential foreshadowing hooks
// planted in the generated chapter text, and inserts them as auto_extracted foreshadowings.
func (s *ChapterService) extractAutoForeshadowings(ctx context.Context, projectID, chapterID, content string) {
	truncated := content
	if utf8.RuneCountInString(truncated) > 3000 {
		runes := []rune(truncated)
		truncated = string(runes[:3000])
	}

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task:      "foreshadowing_extraction",
		MaxTokens: 500,
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: `你是伏笔探测系统。从章节中找出作者有意或无意埋下的可以在后续章节回收的伏笔线索。只输出JSON数组，不要输出其他文字。
每个元素格式：{"content":"伏笔内容简述","embed_method":"explicit或implicit","priority":1-10}
如果没有发现任何伏笔则返回空数组[]。最多返回3条。`},
			{Role: "user", Content: truncated},
		},
	})
	if err != nil {
		s.logger.Debug("extractAutoForeshadowings: LLM call failed", zap.Error(err))
		return
	}

	raw := extractJSON(resp.Content)
	var hooks []struct {
		Content     string `json:"content"`
		EmbedMethod string `json:"embed_method"`
		Priority    int    `json:"priority"`
	}
	if err := json.Unmarshal([]byte(raw), &hooks); err != nil {
		s.logger.Debug("extractAutoForeshadowings: parse failed", zap.Error(err))
		return
	}

	for _, h := range hooks {
		if h.Content == "" {
			continue
		}
		if h.Priority < 1 {
			h.Priority = 3
		}
		if h.EmbedMethod == "" {
			h.EmbedMethod = "implicit"
		}
		s.db.Exec(ctx,
			`INSERT INTO foreshadowings (project_id, content, embed_method, priority, status, embed_chapter_id, origin)
			 VALUES ($1, $2, $3, $4, 'planted', $5, 'auto_extracted')
			 ON CONFLICT DO NOTHING`,
			projectID, h.Content, h.EmbedMethod, h.Priority, chapterID)
	}
	if len(hooks) > 0 {
		s.logger.Info("auto-extracted foreshadowings",
			zap.String("chapter_id", chapterID),
			zap.Int("count", len(hooks)))
	}
}

// updateVolumeArcSummary appends the chapter summary to the running volume arc summary.
func (s *ChapterService) updateVolumeArcSummary(ctx context.Context, projectID, chapterID, content string) {
	var volumeID string
	var chapterNum int
	err := s.db.QueryRow(ctx,
		`SELECT COALESCE(volume_id::text, ''), chapter_num FROM chapters WHERE id = $1`, chapterID).
		Scan(&volumeID, &chapterNum)
	if err != nil || volumeID == "" {
		return
	}

	summary := s.generateSummary(ctx, content)
	if summary == "" {
		return
	}

	// Upsert volume arc summary
	s.db.Exec(ctx,
		`INSERT INTO volume_arc_summaries (project_id, volume_id, summary, key_events, last_chapter_num)
		 VALUES ($1, $2::uuid, $3, '', $4)
		 ON CONFLICT (project_id, volume_id) DO UPDATE
		 SET summary = volume_arc_summaries.summary || E'\n' || $3,
		     last_chapter_num = $4,
		     updated_at = NOW()`,
		projectID, volumeID, fmt.Sprintf("第%d章：%s", chapterNum, summary), chapterNum)
}

// buildSystemPrompt constructs the system prompt using the Lost-in-Middle layout:
// HEAD: world bible + constitution + character states (high attention)
// MIDDLE: previous summaries + foreshadowing status (lower attention)
// TAIL: current outline + tension target + generation params (high attention)
func (s *ChapterService) buildSystemPrompt(ctx context.Context, projectID string, chapterNum int, req models.GenerateChapterRequest) string {
	var sb strings.Builder

	// ===== ANCHOR: Word count hard limit (highest priority, placed first for maximum LLM attention) =====
	if req.ChapterWordsMin > 0 && req.ChapterWordsMax > 0 {
		sb.WriteString(fmt.Sprintf("⚠️【字数硬约束 — 最高优先级】本章正文须控制在 %d～%d 字以内。当写作接近 %d 字时，无论剧情进展如何，必须立即执行断章收尾（用对话、动作或悬念断开），绝不允许超出 %d 字上限。超出字数上限是严重错误。⚠️\n\n", req.ChapterWordsMin, req.ChapterWordsMax, req.ChapterWordsMax, req.ChapterWordsMax))
	}

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

	// ===== Genre-specific rules (injected after constitution) =====
	var genreRules, langConstraints, rhythmRules, projectGenre string
	s.db.QueryRow(ctx,
		`SELECT gt.rules_content, gt.language_constraints, gt.rhythm_rules, p.genre
		 FROM genre_templates gt
		 JOIN projects p ON p.genre = gt.genre
		 WHERE p.id = $1
		 LIMIT 1`, projectID).
		Scan(&genreRules, &langConstraints, &rhythmRules, &projectGenre)
	if genreRules != "" || langConstraints != "" || rhythmRules != "" {
		sb.WriteString("=== 题材专属规则 ===\n")
		if genreRules != "" {
			sb.WriteString(genreRules)
			sb.WriteString("\n")
		}
		if langConstraints != "" {
			sb.WriteString("【语言约束】")
			sb.WriteString(langConstraints)
			sb.WriteString("\n")
		}
		if rhythmRules != "" {
			sb.WriteString("【节奏规范】")
			sb.WriteString(rhythmRules)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	// Genre exclusion: explicitly ban elements that don't belong to this genre
	if projectGenre == "" {
		s.db.QueryRow(ctx, `SELECT genre FROM projects WHERE id = $1`, projectID).Scan(&projectGenre)
	}
	if exclusionBlock := buildGenreExclusionBlock(projectGenre); exclusionBlock != "" {
		sb.WriteString(exclusionBlock)
		sb.WriteString("\n\n")
	}

	// Character states
	sb.WriteString("=== 角色状态 ===\n")
	if charRows, charErr := s.db.Query(ctx,
		`SELECT name, role_type, profile, current_state FROM characters WHERE project_id = $1`, projectID); charErr != nil {
		s.logger.Warn("failed to load characters for prompt", zap.Error(charErr))
	} else {
		for charRows.Next() {
			var name, roleType string
			var profile, state json.RawMessage
			charRows.Scan(&name, &roleType, &profile, &state)
			sb.WriteString(fmt.Sprintf("- %s（%s）: %s\n", name, roleType, string(profile)))
			if state != nil {
				sb.WriteString(fmt.Sprintf("  当前状态：%s\n", string(state)))
			}
		}
		charRows.Close()
	}
	sb.WriteString("\n")

	// ===== MIDDLE: Previous Summaries (RecurrentGPT sliding window) =====
	sb.WriteString("=== 前文摘要（记忆窗口）===\n")
	windowSize := 5
	startChapter := chapterNum - windowSize
	if startChapter < 1 {
		startChapter = 1
	}
	if startChapter < chapterNum {
		// Use MGET to fetch all window summaries in a single Redis round trip (no N+1).
		summaryKeys := make([]string, 0, chapterNum-startChapter)
		for i := startChapter; i < chapterNum; i++ {
			summaryKeys = append(summaryKeys, fmt.Sprintf("chapter_summary:%s:%d", projectID, i))
		}
		vals, mgetErr := s.rdb.MGet(ctx, summaryKeys...).Result()
		if mgetErr == nil {
			for idx, val := range vals {
				if val != nil {
					if s, ok := val.(string); ok && s != "" {
						sb.WriteString(fmt.Sprintf("第%d章摘要：%s\n", startChapter+idx, s))
					}
				}
			}
		}
	}
	sb.WriteString("\n")

	// ===== Anti-Repetition Event Digest (prevents 10-30 chapter similar plots) =====
	// Fetch key events from last 15 chapters' outlines in a single query to detect repetition patterns
	{
		antiRepRows, antiRepErr := s.db.Query(ctx,
			`SELECT o.order_num, o.content
			 FROM outlines o
			 WHERE o.project_id = $1 AND o.level = 'chapter'
			   AND o.order_num >= $2 AND o.order_num < $3
			 ORDER BY o.order_num DESC
			 LIMIT 15`,
			projectID, max(1, chapterNum-15), chapterNum)
		if antiRepErr == nil {
			var usedEvents []string
			for antiRepRows.Next() {
				var oNum int
				var oContent json.RawMessage
				if antiRepRows.Scan(&oNum, &oContent) != nil {
					continue
				}
				var oData map[string]interface{}
				if json.Unmarshal(oContent, &oData) == nil {
					if events, ok := oData["events"].([]interface{}); ok {
						for _, ev := range events {
							if es, ok := ev.(string); ok && es != "" {
								usedEvents = append(usedEvents, fmt.Sprintf("第%d章：%s", oNum, es))
							}
						}
					}
				}
			}
			antiRepRows.Close()
			if len(usedEvents) > 0 {
				sb.WriteString("=== 近期已使用的剧情事件（禁止重复）===\n")
				sb.WriteString("以下事件已在近期章节中展开过，本章禁止出现雷同或高度相似的情节设置：\n")
				shown := usedEvents
				if len(shown) > 20 {
					shown = shown[:20]
				}
				for _, ue := range shown {
					sb.WriteString(fmt.Sprintf("  ✗ %s\n", ue))
				}
				sb.WriteString("如发现本章大纲事件与以上已有事件雷同（如相同的偶遇、相同的打斗模式、相同的交易场景），必须用不同的切入角度、不同的场景环境、不同的人物反应来处理，避免读者产生\"又来了\"的感觉。\n\n")
			}
		}
	}

	// ===== Qdrant Narrative context retrieval (plot-relevant past content) =====
	if s.rag != nil {
		// Query for plot-relevant narrative context using the current chapter outline
		var outlineQuery string
		var outJSON json.RawMessage
		if s.db.QueryRow(ctx,
			`SELECT content FROM outlines WHERE project_id = $1 AND level = 'chapter' AND order_num = $2`,
			projectID, chapterNum).Scan(&outJSON) == nil {
			outlineQuery = string(outJSON)
		}
		if outlineQuery == "" {
			outlineQuery = fmt.Sprintf("第%d章 %s", chapterNum, req.TargetPace)
		}
		narrativeHits, nErr := s.rag.SearchSensory(ctx, projectID, outlineQuery, "chapter_summaries", 3)
		if nErr == nil && len(narrativeHits) > 0 {
			sb.WriteString("=== 相关历史情节（语义检索）===\n")
			for i, hit := range narrativeHits {
				sb.WriteString(fmt.Sprintf("【关联片段%d】%s\n", i+1, hit))
			}
			sb.WriteString("请确保本章内容与以上历史情节保持连贯但不重复。\n\n")
		}
	}

	// Foreshadowing status
	type promptForeshadowing struct {
		Content               string
		EmbedMethod           string
		Status                string
		Priority              int
		EmbedChapterNum       int
		ResolveChapterNum     int
		PlannedEmbedChapter   int
		PlannedResolveChapter int
	}
	sb.WriteString("=== 本章可用伏笔 ===\n")
	if fsRows, fsErr := s.db.Query(ctx,
		`SELECT f.content,
		        COALESCE(f.embed_method, ''),
		        f.status,
		        f.priority,
		        COALESCE(ec.chapter_num, 0) AS embed_chapter_num,
		        COALESCE(rc.chapter_num, 0) AS resolve_chapter_num,
		        COALESCE(f.planned_embed_chapter, 0),
		        COALESCE(f.planned_resolve_chapter, 0)
		 FROM foreshadowings f
		 LEFT JOIN chapters ec ON ec.id = f.embed_chapter_id
		 LEFT JOIN chapters rc ON rc.id = f.resolve_chapter_id
		 WHERE f.project_id = $1
		 ORDER BY f.priority DESC, f.created_at ASC`,
		projectID); fsErr != nil {
		s.logger.Warn("failed to load foreshadowings for prompt", zap.Error(fsErr))
	} else {
		available := make([]promptForeshadowing, 0)
		mustEmbed := make([]promptForeshadowing, 0)
		mustResolve := make([]promptForeshadowing, 0)
		futureCount := 0
		for fsRows.Next() {
			var item promptForeshadowing
			if err := fsRows.Scan(&item.Content, &item.EmbedMethod, &item.Status, &item.Priority,
				&item.EmbedChapterNum, &item.ResolveChapterNum,
				&item.PlannedEmbedChapter, &item.PlannedResolveChapter); err != nil {
				continue
			}
			// Check if this chapter is the planned embed chapter
			if item.PlannedEmbedChapter == chapterNum && (item.Status == "planned" || item.Status == "") {
				mustEmbed = append(mustEmbed, item)
			}
			// Check if this chapter is the planned resolve chapter
			if item.PlannedResolveChapter == chapterNum && (item.Status == "planted" || item.Status == "active") {
				mustResolve = append(mustResolve, item)
			}
			if item.EmbedChapterNum > 0 && item.EmbedChapterNum > chapterNum {
				futureCount++
				continue
			}
			if item.PlannedEmbedChapter > chapterNum && item.Status == "planned" {
				futureCount++
				continue
			}
			available = append(available, item)
		}
		fsRows.Close()

		// Show must-embed foreshadowings prominently
		if len(mustEmbed) > 0 {
			sb.WriteString("【本章必须植入的伏笔】（在场景/对话中自然埋入，不可遗漏）\n")
			for _, item := range mustEmbed {
				sb.WriteString(fmt.Sprintf("  ★ %s（方式：%s，优先级P%d）\n", item.Content, item.EmbedMethod, item.Priority))
			}
			sb.WriteString("\n")
		}
		// Show must-resolve foreshadowings prominently
		if len(mustResolve) > 0 {
			sb.WriteString("【本章必须回收/揭示的伏笔】（在剧情中安排呼应/揭露/确认）\n")
			for _, item := range mustResolve {
				sb.WriteString(fmt.Sprintf("  ★ %s（当前状态：%s）\n", item.Content, item.Status))
			}
			sb.WriteString("\n")
		}

		if len(available) == 0 && len(mustEmbed) == 0 && len(mustResolve) == 0 {
			sb.WriteString("- 本章无必须强行植入的既有伏笔，可优先稳住开场、人物关系与当前冲突。\n")
		} else if len(available) > 0 {
			sb.WriteString("【已植入可供参考的伏笔】\n")
			for _, item := range available {
				resolveNote := "未指定回收章"
				if item.PlannedResolveChapter > 0 {
					resolveNote = fmt.Sprintf("计划第%d章回收", item.PlannedResolveChapter)
				} else if item.ResolveChapterNum > 0 {
					resolveNote = fmt.Sprintf("预计第%d章后回收", item.ResolveChapterNum)
				}
				sb.WriteString(fmt.Sprintf("- [%s] P%d %s（植入方式：%s；%s）\n",
					item.Status, item.Priority, item.Content, item.EmbedMethod, resolveNote))
			}
		}
		if futureCount > 0 {
			sb.WriteString(fmt.Sprintf("- 另有 %d 条后续章节伏笔尚未到登场时机，禁止提前明示、解释或兑现。\n", futureCount))
		}
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

	// Next chapter outline preview (for transition setup)
	// Only show if the next chapter is within the same volume to prevent cross-volume leakage
	var nextOutlineContent json.RawMessage
	var nextChapterTitle string
	nextChapterInSameVolume := false
	var curVolEnd int
	if s.db.QueryRow(ctx,
		`SELECT chapter_end FROM volumes WHERE project_id = $1
		 AND chapter_start <= $2 AND chapter_end >= $2`,
		projectID, chapterNum).Scan(&curVolEnd) == nil {
		nextChapterInSameVolume = (chapterNum + 1) <= curVolEnd
	}
	if nextChapterInSameVolume && s.db.QueryRow(ctx,
		`SELECT title, content FROM outlines WHERE project_id = $1 AND level = 'chapter' AND order_num = $2`,
		projectID, chapterNum+1).Scan(&nextChapterTitle, &nextOutlineContent) == nil {
		sb.WriteString("\n=== 下一章概要（仅供过渡铺垫参考，不可提前展开）===\n")
		sb.WriteString(fmt.Sprintf("第%d章：%s\n", chapterNum+1, nextChapterTitle))
		var nextData map[string]interface{}
		if json.Unmarshal(nextOutlineContent, &nextData) == nil {
			if events, ok := nextData["events"].([]interface{}); ok && len(events) > 0 {
				if firstEvent, ok := events[0].(string); ok {
					sb.WriteString(fmt.Sprintf("开场事件提示：%s\n", firstEvent))
				}
			}
		}
		sb.WriteString("注意：仅在末尾做轻微过渡暗示即可，不可在本章中实际展开下一章的内容。\n")
	} else if !nextChapterInSameVolume {
		sb.WriteString("\n=== 本章为本卷最后一章或接近末尾 ===\n")
		sb.WriteString("本章是本卷末段，应收束本卷冲突线并留下适当悬念引入下一卷，但不可展开下一卷的具体剧情。\n")
	}

	// Volume position pacing guidance
	var volChapterStart, volChapterEnd int
	var volTitle string
	if s.db.QueryRow(ctx,
		`SELECT title, chapter_start, chapter_end FROM volumes WHERE project_id = $1
		 AND chapter_start <= $2 AND chapter_end >= $2`,
		projectID, chapterNum).Scan(&volTitle, &volChapterStart, &volChapterEnd) == nil {
		volTotal := volChapterEnd - volChapterStart + 1
		posInVol := chapterNum - volChapterStart + 1
		progress := float64(posInVol) / float64(volTotal)
		sb.WriteString("\n=== 卷内节奏定位 ===\n")
		sb.WriteString(fmt.Sprintf("当前卷：%s（第%d～%d章，共%d章），本章为本卷第%d章\n", volTitle, volChapterStart, volChapterEnd, volTotal, posInVol))
		if progress <= 0.2 {
			sb.WriteString(fmt.Sprintf("当前处于本卷开头（第%d/%d章），节奏应偏缓：重铺垫、建场景、引矛盾。避免重大冲突爆发。\n", posInVol, volTotal))
		} else if progress <= 0.7 {
			sb.WriteString(fmt.Sprintf("当前处于本卷中段（第%d/%d章），节奏中等偏快：推进主线、制造冲突、角色发展。\n", posInVol, volTotal))
		} else if progress <= 0.9 {
			sb.WriteString(fmt.Sprintf("当前处于本卷高潮段（第%d/%d章），节奏紧凑：冲突激化、真相揭示、关键对决。\n", posInVol, volTotal))
		} else {
			sb.WriteString(fmt.Sprintf("当前处于本卷收尾（第%d/%d章），节奏：收束当前冲突但留下悬念引入下一卷。\n", posInVol, volTotal))
		}
	}
	sb.WriteString("\n=== 章节推进约束（硬规则）===\n")
	sb.WriteString("- 本章只推进【当前章节大纲】中明确列出的事件，禁止自行添加大纲中没有的新事件、新冲突、新转折。\n")
	sb.WriteString("- 大纲中列出的每个事件都必须在正文中有对应的场景展开，不可遗漏大纲事件。\n")
	sb.WriteString("- 禁止提前展开后续章节的设定、底牌、关系反转或高潮信息。\n")
	sb.WriteString("- 未出现在【本章可用伏笔】里的后续设定，一律不能提前明示、解释、兑现。\n")
	sb.WriteString("- 若需要铺垫后续内容，只能做轻量暗示（一笔带过的细节、角色一闪而逝的念头），不能让角色在本章就把后续阶段的问题直接解决。\n")
	sb.WriteString("- 【信息密度控制】本章最多推进大纲中的 1～3 件剧情事件（绝对上限3件）；事件展开以**对话交锋和人物动作反应**为主要手段，而非大段场景描写——每次景物/环境描写控制在3～4句以内；大纲要求超过3件事时只完成最关键的2～3件，其余顺延。\n")
	sb.WriteString("- 【卷内剧情边界】本章属于当前卷的范围，只处理本卷应有的剧情线。禁止在本章引入或解决属于后续卷的核心冲突、关键真相或角色重大转变。\n")
	sb.WriteString("- 【角色/道具出场约束】\n")
	sb.WriteString("  · 正文中出现的每个角色必须来自上方【角色状态】列表或在大纲事件中有明确提及\n")
	sb.WriteString("  · 禁止凭空出现【角色状态】和大纲中都未提及的新角色（路人/群众描写除外）\n")
	sb.WriteString("  · 武器/法宝/道具首次出场必须有来源说明（战利品/购买/NPC赠予/祖传/大纲事件获得）\n")
	sb.WriteString("  · 禁止正文突然出现「他拿出一把XX」「她取出一件XX」等无来源的道具描写\n")
	sb.WriteString("  · 如果大纲事件要求获得新道具，必须写出完整的获得过程（至少100字场景）\n")
	if chapterNum == 1 {
		sb.WriteString("- 【第一章：主角姓名揭露】主角名字必须在第一章内通过他人称呼、自我介绍或心理活动等方式出现，读者读完第一章必须知道主角叫什么；全章不得仅用\"他\"/\"她\"/\"少年\"/\"年轻人\"等代称而不揭露名字。\n")
		sb.WriteString("- 【第一章：开篇节奏】开篇须直接进入动作、对话或具体冲突，前300字内发生至少一件具体事件；禁止以大段环境描写、倒叙身世或世界观铺垫作为开场。\n")
		sb.WriteString("- 第一章重点在主角当前处境和核心矛盾引子，避免把中后期设定一次性打满。\n")
	}
	sb.WriteString("\n")

	// ===== Anti-AI Writing Craft Rules =====
	sb.WriteString("=== 写作手法硬规则（反AI痕迹）===\n")
	sb.WriteString("【章节结尾 - 强制断章，拒绝收尾】\n")
	sb.WriteString("核心原则：网文章节是连载碎片，单章无需完整叙事弧线。必须在情节高点或悬念处强行断开。\n")
	sb.WriteString("\n严格禁止：\n")
	sb.WriteString("- 任何形式的总结段、展望段、心理独白升华段、情绪收束段\n")
	sb.WriteString("- 预告性句式：【他/她知道XXX】【未来XXX】【更大的XXX即将到来】【这只是开始】【命运的齿轮开始转动】【新的篇章即将展开】\n")
	sb.WriteString("- 场景完整收尾：【夜深了，XX回到房间】【一切归于平静】【故事还在继续】\n")
	sb.WriteString("- 给读者交代感的任何尝试（人是活的，章节要在悬念中断）\n")
	sb.WriteString("\n正确断章范式（参考经典网文）：\n")
	sb.WriteString("1. 对话断章：【他冷冷道：你敢！】- 在威胁/质问处戛然而止\n")
	sb.WriteString("2. 动作断章：【他的手，已经按在了剑柄上。】- 在动作触发前一刻断开\n")
	sb.WriteString("3. 信息断章：【门外传来的脚步声，不是一个人。】- 在关键信息揭露后立即断\n")
	sb.WriteString("4. 冲突断章：【两人对视，空气仿佛凝固。】- 在冲突即将爆发时断\n")
	sb.WriteString("5. 悬念断章：【他忽然意识到，那封信里少了一个字。】- 在谜题出现后立即断\n")
	sb.WriteString("\n技术要求：\n")
	sb.WriteString("- 最后一句必须是：未完成动作 / 未回答问题 / 未解决冲突 / 突发转折\n")
	sb.WriteString("- 允许在对话中途断章（甚至在一句话说到一半）\n")
	sb.WriteString("- 允许在场景描写到50%时突然断开\n")
	sb.WriteString("- 最后一段不得超过2句话，且必须制造紧张感或悬念\n")
	sb.WriteString("\n【叙事视角与时间线】\n")
	sb.WriteString("- 锁定POV：如果指定了主视角角色，整章只能写该角色能感知到的信息（所见、所闻、所想）。不得插入该角色不可能知道的信息、其他角色的内心独白、或全知叙事者的评论。\n")
	sb.WriteString("- 视角切换需要明确的场景分隔（空行或 *** 分隔符），不可在同一段内跳切视角。\n")
	sb.WriteString("- 时间过渡用场景切换、具体动作或环境变化暗示（如：走出酒楼时，街上已经亮起了灯笼），禁止使用【时间飞逝】【不知不觉间】【转眼就到了】等AI惯用过渡词。\n")
	sb.WriteString("\n【反AI文风规则】\n")
	sb.WriteString("- 禁止使用以下AI高频词与句式：\n")
	sb.WriteString("  · 【不禁】【微微】【缓缓】【淡淡】【默默】过度使用（每章每个词最多出现1次）\n")
	sb.WriteString("  · 【一股XXX涌上心头】【心中暗道】【嘴角勾起一抹弧度】【眼中闪过一丝XXX】\n")
	sb.WriteString("  · 连续三句以上使用【他/她+动词+了】的相同句式\n")
	sb.WriteString("  · 【仿佛】【似乎】【好像】在同一段中出现超过1次\n")
	sb.WriteString("  · 排比式心理描写（如：他想到了A，想到了B，想到了C）\n")
	sb.WriteString("  · 段落开头堆叠环境描写超过3句再进入剧情\n")
	sb.WriteString("- 优先使用：\n")
	sb.WriteString("  · 短句与长句交替，制造阅读节奏（如3个短句后跟1个长句，或反之）\n")
	sb.WriteString("  · 对话中夹叙夹议，用角色小动作打断对话（摸鼻子、移开视线、手指在桌上敲）\n")
	sb.WriteString("  · 省略主语的连续动作句（如：推开门，扫了一眼，径直走到角落坐下。）\n")
	sb.WriteString("  · 五感混用描写（声音→触感→画面交替），避免纯视觉化叙述\n")
	sb.WriteString("  · 用具体数字和细节代替模糊形容（如：三步远、半碗饭的功夫，而非：不远处、片刻之后）\n")
	sb.WriteString("  · 让角色通过行动和对话展示性格，而非旁白说明性格（Show Don't Tell）\n")
	sb.WriteString("\n【节奏与密度】\n")
	sb.WriteString("- 本章叙事时间跨度不宜超过一天（除非大纲明确要求跨多日），场景越集中，细节越饱满，AI味越低。\n")
	sb.WriteString("- 【网文叙事重心】每个场景的核心是**对话交锋**（至少2-4轮人物互动），景物/环境描写仅作辅助，单次不超过3～4句，全章环境描写总量不超过正文15%；禁止全篇纯心理独白或连续多段景物描写。\n")
	sb.WriteString("- 【描写克制原则】优先通过人物行为、对话、内心独白（短句）推动剧情，背景描写、景物铺陈仅在必要时点缀；网文读者阅读速度快，大段描写会导致跳读。每段描写超过4句须立即接入对话或动作。\n")
	sb.WriteString("- 战斗/紧张场景使用碎片化短句加速节奏；日常/铺垫场景可适当使用长句，但环境描写上限不变。\n")
	sb.WriteString("- 角色不能在一章之内完成态度大反转，情绪变化需要事件驱动且有过渡。\n")
	sb.WriteString("\n【角色成长与能力获得规则】\n")
	sb.WriteString("- 严格遵守时间线：角色只能使用【角色状态】中已明确拥有的能力、武器、装备、身份\n")
	sb.WriteString("- 禁止凭空给角色新能力：不得出现【角色状态】中未记载的技能、法宝、境界、身份、师承关系\n")
	sb.WriteString("- 能力获得必须有明确过程：\n")
	sb.WriteString("  · 如果大纲要求本章角色获得某项能力/装备，必须写出**完整的获得过程**（奇遇场景、战利品来源、NPC赠予、任务奖励等）\n")
	sb.WriteString("  · 禁止用【不知何时】【早已】【此前】等模糊时间词追溯角色已拥有未记录的资源\n")
	sb.WriteString("  · 获得过程不能一笔带过，需占用至少1个完整场景（200字以上）\n")
	sb.WriteString("- 实力提升必须有代价：\n")
	sb.WriteString("  · 境界突破需要明确的触发条件（生死战斗、顿悟契机、资源消耗）\n")
	sb.WriteString("  · 一章内最多1次实力提升事件，禁止【连续突破】【接连获得】\n")
	sb.WriteString("  · 获得强大资源需要付出代价（受伤、消耗寿命、承诺、失去某物）\n")
	sb.WriteString("- 新获得的能力/装备需要在本章后续剧情中有所体现（不能拿了就结束本章）\n")
	sb.WriteString("- 如本章无获得事件，严格禁止在描写中暗示角色拥有新能力/装备\n")
	sb.WriteString("\n")

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
			sb.WriteString("\n请从上一章结尾自然承接，不要重述已发生的事件，不要重复上一章最后的场景描述。\n")
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
	if req.ChapterWordsMin > 0 || req.ChapterWordsMax > 0 {
		narrativeSection.WriteString(fmt.Sprintf("目标字数：%d-%d 字  ", req.ChapterWordsMin, req.ChapterWordsMax))
	}
	if narrativeSection.Len() > 0 {
		sb.WriteString("=== 生成参数 ===\n")
		sb.WriteString(narrativeSection.String())
		sb.WriteString("\n\n")
	}

	// ===== Glossary injection (InkOS-inspired) =====
	if s.glossary != nil {
		glossaryBlock := s.glossary.BuildPromptBlock(ctx, projectID)
		if glossaryBlock != "" {
			sb.WriteString(glossaryBlock)
		}
	}

	sb.WriteString("\n你是一位专注网文创作的作者，文字干净利落，以对话和动作驱动剧情节奏，克制使用景物描写。请严格遵守世界观设定和宪法规则，保持角色性格一致性。【字数硬上限再次提醒：严格不超过上方规定字数上限，宁可断章偏早，绝不超出上限】。严格遵守上述所有反AI写作规则。")

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
	raw, err := doRetriableJSONRequest(ctx, s.httpClient, s.logger, "POST /humanize", func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.sidecarURL+"/humanize", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return text, fmt.Errorf("humanizer unreachable: %w", err)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return text, fmt.Errorf("decode humanizer response: %w", err)
	}
	if result.Text == "" {
		return text, nil
	}
	return result.Text, nil
}

func (s *ChapterService) generateSummary(ctx context.Context, content string) string {
	// Truncate for summary generation to avoid token overflow
	// Use more content for better context capture (3000 -> 4000)
	truncated := content
	if utf8.RuneCountInString(truncated) > 4000 {
		runes := []rune(truncated)
		truncated = string(runes[:4000])
	}

	// Enhanced summary prompt for better cross-chapter continuity
	systemPrompt := `你是长篇小说编辑。生成章节摘要时必须记录：
1. 核心剧情：本章推进的1-3件事（具体动作、对话要点）
2. 角色状态：情绪变化、关系变化、位置移动
3. 悬念/伏笔：未解决的问题、埋下的线索
4. 结尾方式：最后一幕的场景和断章点（对话/动作/悬念）
5. 语言风格：句式特征（长短句比例、是否有方言/俚语、叙述节奏快慢）

摘要控制在300-400字。重点是为下一章提供承接依据，而非给读者看的提要。`

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task:      "summarization",
		MaxTokens: 800, // Increased from 500 for more detailed summary
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: truncated},
		},
	})
	if err != nil {
		s.logger.Warn("summary generation failed", zap.Error(err))
		// Fallback: take the first 300 characters (increased from 200)
		runes := []rune(content)
		if len(runes) > 300 {
			return string(runes[:300]) + "..."
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
