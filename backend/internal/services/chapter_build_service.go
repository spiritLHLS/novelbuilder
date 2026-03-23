package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

func (s *ChapterService) settleChapterState(ctx context.Context, projectID, chapterID, content string) {
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
	for charRows.Next() {
		var ci charInfo
		charRows.Scan(&ci.id, &ci.name)
		chars = append(chars, ci)
	}
	charRows.Close()

	type fsInfo struct {
		id      string
		content string
	}
	var foreshadowings []fsInfo
	fsRows, err := s.db.Query(ctx,
		`SELECT id, content FROM foreshadowings WHERE project_id = $1 AND status IN ('planned','planted')`, projectID)
	if err != nil {
		s.logger.Warn("settle: failed to load foreshadowings", zap.Error(err))
		return
	}
	for fsRows.Next() {
		var fi fsInfo
		fsRows.Scan(&fi.id, &fi.content)
		foreshadowings = append(foreshadowings, fi)
	}
	fsRows.Close()

	if len(chars) == 0 && len(foreshadowings) == 0 {
		return
	}

	// ── 2. Build structured prompt for LLM settlement analysis ──────────────
	charNames := make([]string, len(chars))
	for i, c := range chars {
		charNames[i] = c.name
	}
	fsContents := make([]string, len(foreshadowings))
	for i, f := range foreshadowings {
		fsContents[i] = f.content
	}

	truncated := content
	if utf8.RuneCountInString(truncated) > 4000 {
		runes := []rune(truncated)
		truncated = string(runes[:4000])
	}

	systemMsg := `你是小说状态追踪系统。分析章节内容，提取状态变化。必须以纯JSON格式回复，不要包含其他文本。`
	userMsg := fmt.Sprintf(`角色列表：%v
待解决伏笔：%v

章节内容（节选）：
%s

请分析后输出如下JSON（只输出JSON，不要解释）：
{
  "character_states": [
    {"name": "角色名（必须来自上面的列表）", "current_state": "状态描述"}
  ],
  "resolved_foreshadowings": [
    "已解决的伏笔内容（必须来自上面的列表，精确匹配）"
  ]
}`, charNames, fsContents, truncated)

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
			`UPDATE foreshadowings SET status = 'resolved', updated_at = NOW() WHERE id = $1`,
			fid)
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
		zap.Int("fs_resolved", len(settlement.ResolvedForeshadowings)))
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

	// ===== Genre-specific rules (injected after constitution) =====
	var genreRules, langConstraints, rhythmRules string
	s.db.QueryRow(ctx,
		`SELECT gt.rules_content, gt.language_constraints, gt.rhythm_rules
		 FROM genre_templates gt
		 JOIN projects p ON p.genre = gt.genre
		 WHERE p.id = $1
		 LIMIT 1`, projectID).
		Scan(&genreRules, &langConstraints, &rhythmRules)
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

	// Foreshadowing status
	type promptForeshadowing struct {
		Content           string
		EmbedMethod       string
		Status            string
		Priority          int
		EmbedChapterNum   int
		ResolveChapterNum int
	}
	sb.WriteString("=== 本章可用伏笔 ===\n")
	if fsRows, fsErr := s.db.Query(ctx,
		`SELECT f.content,
		        COALESCE(f.embed_method, ''),
		        f.status,
		        f.priority,
		        COALESCE(ec.chapter_num, 0) AS embed_chapter_num,
		        COALESCE(rc.chapter_num, 0) AS resolve_chapter_num
		 FROM foreshadowings f
		 LEFT JOIN chapters ec ON ec.id = f.embed_chapter_id
		 LEFT JOIN chapters rc ON rc.id = f.resolve_chapter_id
		 WHERE f.project_id = $1
		 ORDER BY f.priority DESC, f.created_at ASC`,
		projectID); fsErr != nil {
		s.logger.Warn("failed to load foreshadowings for prompt", zap.Error(fsErr))
	} else {
		available := make([]promptForeshadowing, 0)
		futureCount := 0
		for fsRows.Next() {
			var item promptForeshadowing
			if err := fsRows.Scan(&item.Content, &item.EmbedMethod, &item.Status, &item.Priority, &item.EmbedChapterNum, &item.ResolveChapterNum); err != nil {
				continue
			}
			if item.EmbedChapterNum > 0 && item.EmbedChapterNum > chapterNum {
				futureCount++
				continue
			}
			available = append(available, item)
		}
		fsRows.Close()
		if len(available) == 0 {
			sb.WriteString("- 本章无必须强行植入的既有伏笔，可优先稳住开场、人物关系与当前冲突。\n")
		} else {
			for _, item := range available {
				resolveNote := "未指定回收章"
				if item.ResolveChapterNum > 0 {
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
	sb.WriteString("\n=== 章节推进约束 ===\n")
	sb.WriteString("- 只推进本章大纲明确要求的事件，不要提前展开后续章节的设定、底牌、关系反转或高潮信息。\n")
	sb.WriteString("- 未出现在“本章可用伏笔”里的后续设定，一律不能提前明示、解释、兑现。\n")
	sb.WriteString("- 若需要铺垫后续内容，只能做轻量暗示，不能让角色在本章就把后续阶段的问题直接解决。\n")
	if chapterNum == 1 {
		sb.WriteString("- 第一章优先完成开场氛围、主角处境、核心矛盾引子，避免把中后期设定一次性打满。\n")
	}
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

	// ===== Glossary injection (InkOS-inspired) =====
	if s.glossary != nil {
		glossaryBlock := s.glossary.BuildPromptBlock(ctx, projectID)
		if glossaryBlock != "" {
			sb.WriteString(glossaryBlock)
		}
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

	resp, err := s.httpClient.Do(req)
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
