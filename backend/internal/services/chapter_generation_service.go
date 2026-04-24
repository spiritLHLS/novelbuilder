package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

func (s *ChapterService) generateChapter(ctx context.Context, projectID string, chapterNum int, req models.GenerateChapterRequest) (*models.Chapter, error) {
	ctx = contextWithLLMSession(ctx, req.LLMConfig, fmt.Sprintf("chapter_generation:%s:%d", projectID, chapterNum))

	// Idempotency guard: if this chapter already exists (e.g. due to a previous task
	// attempt that succeeded at the DB step but failed to mark the task as done),
	// return the existing chapter without re-generating or calling the AI again.
	if existing, _ := s.GetByProjectAndNum(ctx, projectID, chapterNum); existing != nil {
		s.logger.Info("chapter already exists, skipping generation (idempotent)",
			zap.String("project_id", projectID),
			zap.Int("chapter_num", chapterNum),
			zap.String("chapter_id", existing.ID))
		return existing, nil
	}

	// Enforce workflow gates
	if err := s.wf.CanGenerateNextChapter(ctx, projectID); err != nil {
		return nil, err
	}

	// Resolve word-count range (request > project default > sensible defaults).
	wordsMin := req.ChapterWordsMin
	wordsMax := req.ChapterWordsMax
	if wordsMin <= 0 {
		var pw int
		s.db.QueryRow(ctx, `SELECT chapter_words FROM projects WHERE id = $1`, projectID).Scan(&pw)
		if pw > 0 {
			wordsMin = pw * 2 / 3
			wordsMax = pw * 4 / 3
		} else {
			wordsMin, wordsMax = 2000, 3500
		}
	}
	if wordsMax <= 0 {
		wordsMax = 3500 // hard default cap
	}
	if wordsMin > wordsMax {
		wordsMin, wordsMax = wordsMax, wordsMin
	}
	// Enforce absolute ceiling: never allow max to exceed 3500 unless explicitly configured
	if wordsMax > 3500 && req.ChapterWordsMax <= 0 {
		wordsMax = 3500
	}

	// Pass resolved word-count limits into req so buildSystemPrompt can embed them as anchor constraints
	req.ChapterWordsMin = wordsMin
	req.ChapterWordsMax = wordsMax

	// Build system prompt using HEAD/MIDDLE/TAIL (Lost-in-Middle) layout
	systemPrompt := s.buildSystemPrompt(ctx, projectID, chapterNum, req)

	// Build user prompt for chapter generation
	var outlineEventsForPrompt string
	var outlineJSON json.RawMessage
	if s.db.QueryRow(ctx,
		`SELECT content FROM outlines WHERE project_id = $1 AND level = 'chapter' AND order_num = $2`,
		projectID, chapterNum).Scan(&outlineJSON) == nil {
		var outlineData map[string]interface{}
		if json.Unmarshal(outlineJSON, &outlineData) == nil {
			if events, ok := outlineData["events"].([]interface{}); ok && len(events) > 0 {
				var eventLines []string
				maxEvents := 3
				if len(events) < maxEvents {
					maxEvents = len(events)
				}
				for i := 0; i < maxEvents; i++ {
					if es, ok := events[i].(string); ok {
						eventLines = append(eventLines, fmt.Sprintf("  %d. %s", i+1, es))
					}
				}
				outlineEventsForPrompt = strings.Join(eventLines, "\n")
			}
		}
	}

	userPrompt := fmt.Sprintf(`请生成第 %d 章的完整内容。

生成参数：
- 叙事视角：%s
- POV角色：%s
- 目标节奏：%s
- 结尾钩子类型：%s
- 结尾钩子强度：%d
- 张力水平：%.1f

⚠️ 【本章必须完成的大纲事件 — 不可遗漏、不可自行添加】
%s
以上事件是本章的全部剧情内容，正文必须逐一展开上述每个事件对应的场景，禁止自行发明大纲以外的新事件、新冲突、新角色登场。

硬性要求：
1. ⚠️ 字数硬上限：正文不得超过 %d 字。当写作接近 %d 字时立即执行断章收尾；字数不足 %d 字时通过对话和动作细节充实。超出上限是严重错误。
2. 本章只展开上方大纲列出的事件（最多3件），事件展开优先用**对话和动作**推进（不要大段景物描写）
3. 从上一章结尾处自然承接，禁止复述前文，延续前一章的语言风格和叙述节奏
4. 严格锁定指定POV视角，不写该角色感知不到的信息
5. **强制断章要求**：章节必须在场景动作、对话或悬念的高点处戛然而止
   - 禁止任何收尾：不写总结段、展望段、升华段、情绪收束段
   - 禁止预告句式：【他知道XXX】【未来XXX】【更大的XXX即将到来】
   - 最后一段不超过2句话，必须是未完成的动作/对话/悬念
   - 参考网文断章：在读者最想知道下文时立即断开
6. 遵守系统提示中的全部反AI文风规则（禁用微微/缓缓/淡淡等）
7. **角色能力严格遵循时间线**：
   - 角色只能使用系统提示【角色状态】中已明确记载的能力/装备/身份
   - 禁止给角色突然出现未记录的新能力、新武器、新师承、新身份
   - 如本章大纲要求角色获得某项资源，必须写完整获得过程（至少200字场景）
   - 一章最多1次实力提升，禁止连续突破或批量获得资源
8. **角色和道具出场约束**：
   - 正文中出现的有名有姓的角色必须来自系统提示中的【角色状态】列表或本章大纲事件
   - 任何武器/法宝/道具首次出场必须有明确来源（大纲事件获得/战利品/NPC赠予/购买/祖传）
   - 禁止凭空出现没有来源的角色或道具`,
		chapterNum,
		req.NarrativeOrder, req.POVCharacter, req.TargetPace,
		req.EndHookType, req.EndHookStrength, req.TensionLevel,
		outlineEventsForPrompt,
		wordsMax, wordsMax, wordsMin)

	if req.ContextHint != "" {
		userPrompt += fmt.Sprintf("\n\n本章特别方向：%s", req.ContextHint)
	}
	userPrompt += "\n\n请直接输出章节正文，不要包含章节号、标题、元数据或任何标记。"

	resp, err := s.ai.ChatWithConfig(ctx, gateway.ChatRequest{
		Task: "chapter_generation",
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}, req.LLMConfig)
	if err != nil {
		return nil, fmt.Errorf("AI generation failed: %w", err)
	}

	chapterContent := resp.Content
	totalInputTokens := resp.InputTokens
	totalOutputTokens := resp.OutputTokens

	if humanized, hErr := s.humanizeContent(ctx, chapterContent, 0.7); hErr == nil {
		chapterContent = humanized
	} else {
		s.logger.Warn("humanizer skipped", zap.Error(hErr))
	}

	chapterContent, extraIn, extraOut, err := s.ensureChapterWordCount(ctx, systemPrompt, chapterContent, wordsMin, wordsMax, req.LLMConfig)
	if err != nil {
		return nil, err
	}
	totalInputTokens += extraIn
	totalOutputTokens += extraOut

	wordCount := utf8.RuneCountInString(chapterContent)
	summary := s.generateSummary(ctx, chapterContent)
	title := s.generateTitle(ctx, chapterContent, chapterNum)

	var volumeID *string
	s.db.QueryRow(ctx,
		`SELECT id FROM volumes WHERE project_id = $1 AND chapter_start <= $2 AND chapter_end >= $2`,
		projectID, chapterNum).Scan(&volumeID)

	chID := uuid.New().String()
	genParams, _ := json.Marshal(req)
	var ch models.Chapter
	err = s.db.QueryRow(ctx,
		`INSERT INTO chapters (id, project_id, volume_id, chapter_num, title, content, word_count, summary,
		 gen_params, input_tokens, output_tokens, status, version, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'draft', 1, NOW(), NOW())
		 ON CONFLICT (project_id, chapter_num) DO NOTHING
		 RETURNING id, project_id, volume_id, chapter_num, title, content, word_count, COALESCE(summary, ''),
		 COALESCE(gen_params, '{}'), COALESCE(quality_report, '{}'), COALESCE(originality_score, 0),
		 COALESCE(genre_compliance_score, 1.0), COALESCE(genre_violations, '[]'),
		 status, version, COALESCE(review_comment, ''), created_at, updated_at`,
		chID, projectID, volumeID, chapterNum, title, chapterContent, wordCount, summary, genParams,
		totalInputTokens, totalOutputTokens).Scan(
		&ch.ID, &ch.ProjectID, &ch.VolumeID, &ch.ChapterNum, &ch.Title, &ch.Content,
		&ch.WordCount, &ch.Summary, &ch.GenParams, &ch.QualityReport, &ch.OriginalityScore,
		&ch.GenreComplianceScore, &ch.GenreViolations,
		&ch.Status, &ch.Version, &ch.ReviewComment, &ch.CreatedAt, &ch.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, qErr := s.GetByProjectAndNum(ctx, projectID, chapterNum)
		if qErr != nil || existing == nil {
			return nil, fmt.Errorf("save chapter: conflict but fetch also failed: %w", qErr)
		}
		s.logger.Info("chapter insert skipped due to concurrent insert, returning existing",
			zap.String("project_id", projectID),
			zap.Int("chapter_num", chapterNum))
		return existing, nil
	}
	if err != nil {
		return nil, fmt.Errorf("save chapter: %w", err)
	}
	_ = s.CreateSnapshot(ctx, ch.ID, "generated", "initial generated snapshot")

	s.rdb.Set(ctx, fmt.Sprintf("chapter_summary:%s:%d", projectID, chapterNum), summary, 7*24*time.Hour)
	s.rdb.Set(ctx, fmt.Sprintf("chapter_content:%s:%d", projectID, chapterNum), chapterContent, 24*time.Hour)

	go func(pid, cid, content string, cNum int) {
		sCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.logChapterSimilarity(sCtx, pid, cid, content, cNum)
	}(projectID, ch.ID, chapterContent, chapterNum)

	if s.rag != nil {
		go func(pid string, cNum int, sum string) {
			rCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = s.rag.StoreEmbedding(rCtx, pid, "chapter_summaries", sum, "chapter", fmt.Sprintf("ch_%d", cNum), map[string]interface{}{
				"chapter_num": cNum,
			})
		}(projectID, chapterNum, summary)
	}

	if s.originality != nil {
		go func() {
			auditCtx := context.Background()
			if _, err := s.originality.AuditChapter(auditCtx, ch.ID, projectID, chapterContent); err != nil {
				s.logger.Warn("originality audit failed", zap.Error(err))
			}
		}()
	}

	if s.propagation != nil {
		go s.propagation.RecordChapterDependencies(context.Background(), projectID, ch.ID)
	}

	if s.webhook != nil {
		go s.webhook.Fire(context.Background(), projectID, "chapter_generated", map[string]any{
			"chapter_id":  ch.ID,
			"chapter_num": ch.ChapterNum,
			"word_count":  ch.WordCount,
		})
	}

	go func(pid, cid, content string) {
		sCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.settleChapterState(sCtx, pid, cid, content)
	}(projectID, ch.ID, chapterContent)

	s.logger.Info("chapter generated",
		zap.String("project_id", projectID),
		zap.Int("chapter_num", chapterNum),
		zap.Int("word_count", wordCount))

	return &ch, nil
}

func (s *ChapterService) regenerateChapter(ctx context.Context, id string, req models.GenerateChapterRequest) (*models.Chapter, error) {
	var projectID string
	var chapterNum int
	err := s.db.QueryRow(ctx,
		`SELECT project_id, chapter_num FROM chapters WHERE id = $1`, id).Scan(&projectID, &chapterNum)
	if err != nil {
		return nil, fmt.Errorf("chapter not found: %w", err)
	}
	ctx = contextWithLLMSession(ctx, req.LLMConfig, fmt.Sprintf("chapter_regeneration:%s:%d", projectID, chapterNum))
	_ = s.CreateSnapshot(ctx, id, "before_regenerate", "before chapter regenerate")

	wordsMin := req.ChapterWordsMin
	wordsMax := req.ChapterWordsMax
	if wordsMin <= 0 {
		var pw int
		s.db.QueryRow(ctx, `SELECT chapter_words FROM projects WHERE id = $1`, projectID).Scan(&pw)
		if pw > 0 {
			wordsMin = pw * 2 / 3
			wordsMax = pw * 4 / 3
		} else {
			wordsMin, wordsMax = 2000, 3500
		}
	}
	if wordsMax <= 0 {
		wordsMax = 3500
	}
	if wordsMin > wordsMax {
		wordsMin, wordsMax = wordsMax, wordsMin
	}
	if wordsMax > 3500 && req.ChapterWordsMax <= 0 {
		wordsMax = 3500
	}

	req.ChapterWordsMin = wordsMin
	req.ChapterWordsMax = wordsMax

	systemPrompt := s.buildSystemPrompt(ctx, projectID, chapterNum, req)

	var regenOutlineEventsForPrompt string
	var regenOutlineJSON json.RawMessage
	if s.db.QueryRow(ctx,
		`SELECT content FROM outlines WHERE project_id = $1 AND level = 'chapter' AND order_num = $2`,
		projectID, chapterNum).Scan(&regenOutlineJSON) == nil {
		var outlineData map[string]interface{}
		if json.Unmarshal(regenOutlineJSON, &outlineData) == nil {
			if events, ok := outlineData["events"].([]interface{}); ok && len(events) > 0 {
				var eventLines []string
				maxEvents := 3
				if len(events) < maxEvents {
					maxEvents = len(events)
				}
				for i := 0; i < maxEvents; i++ {
					if es, ok := events[i].(string); ok {
						eventLines = append(eventLines, fmt.Sprintf("  %d. %s", i+1, es))
					}
				}
				regenOutlineEventsForPrompt = strings.Join(eventLines, "\n")
			}
		}
	}

	userPrompt := fmt.Sprintf(`请生成第 %d 章的完整内容。

生成参数：
- 叙事视角：%s
- POV角色：%s
- 目标节奏：%s
- 结尾钩子类型：%s
- 结尾钩子强度：%d
- 张力水平：%.1f

⚠️ 【本章必须完成的大纲事件 — 不可遗漏、不可自行添加】
%s
以上事件是本章的全部剧情内容，正文必须逐一展开上述每个事件对应的场景，禁止自行发明大纲以外的新事件、新冲突、新角色登场。

硬性要求：
1. ⚠️ 字数硬上限：正文不得超过 %d 字。当写作接近 %d 字时立即执行断章收尾；字数不足 %d 字时通过对话和动作细节充实。超出上限是严重错误。
2. 本章只展开上方大纲列出的事件（最多3件），事件展开优先用**对话和动作**推进（不要大段景物描写）
3. 从上一章结尾处自然承接，禁止复述前文，延续前一章的语言风格和叙述节奏
4. 严格锁定指定POV视角，不写该角色感知不到的信息
5. **强制断章要求**：章节必须在场景动作、对话或悬念的高点处戛然而止
   - 禁止任何收尾：不写总结段、展望段、升华段、情绪收束段
   - 禁止预告句式：【他知道XXX】【未来XXX】【更大的XXX即将到来】
   - 最后一段不超过2句话，必须是未完成的动作/对话/悬念
   - 参考网文断章：在读者最想知道下文时立即断开
6. 遵守系统提示中的全部反AI文风规则（禁用微微/缓缓/淡淡等）
7. **角色能力严格遵循时间线**：
   - 角色只能使用系统提示【角色状态】中已明确记载的能力/装备/身份
   - 禁止给角色突然出现未记录的新能力、新武器、新师承、新身份
   - 如本章大纲要求角色获得某项资源，必须写完整获得过程（至少200字场景）
   - 一章最多1次实力提升，禁止连续突破或批量获得资源
8. **角色和道具出场约束**：
   - 正文中出现的有名有姓的角色必须来自系统提示中的【角色状态】列表或本章大纲事件
   - 任何武器/法宝/道具首次出场必须有明确来源（大纲事件获得/战利品/NPC赠予/购买/祖传）
   - 禁止凭空出现没有来源的角色或道具`,
		chapterNum,
		req.NarrativeOrder, req.POVCharacter, req.TargetPace,
		req.EndHookType, req.EndHookStrength, req.TensionLevel,
		regenOutlineEventsForPrompt,
		wordsMax, wordsMax, wordsMin)
	if req.ContextHint != "" {
		userPrompt += fmt.Sprintf("\n\n本章特别方向：%s", req.ContextHint)
	}
	userPrompt += "\n\n请直接输出章节正文，不要包含章节号、标题、元数据或任何标记。"

	resp, err := s.ai.ChatWithConfig(ctx, gateway.ChatRequest{
		Task: "chapter_regeneration",
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}, req.LLMConfig)
	if err != nil {
		return nil, fmt.Errorf("AI regeneration failed: %w", err)
	}

	chapterContent := resp.Content
	totalInputTokens := resp.InputTokens
	totalOutputTokens := resp.OutputTokens
	if humanized, hErr := s.humanizeContent(ctx, chapterContent, 0.7); hErr == nil {
		chapterContent = humanized
	}
	chapterContent, extraIn, extraOut, err := s.ensureChapterWordCount(ctx, systemPrompt, chapterContent, wordsMin, wordsMax, req.LLMConfig)
	if err != nil {
		return nil, err
	}
	totalInputTokens += extraIn
	totalOutputTokens += extraOut

	s.db.Exec(ctx,
		`UPDATE chapters SET input_tokens = input_tokens + $1, output_tokens = output_tokens + $2 WHERE id = $3`,
		totalInputTokens, totalOutputTokens, id)

	updated, err := s.UpdateContent(ctx, id, chapterContent, "draft")
	if err != nil {
		return nil, fmt.Errorf("update regenerated chapter: %w", err)
	}
	_ = s.CreateSnapshot(ctx, id, "regenerated", "after chapter regenerate")

	if s.originality != nil {
		go func(chID, pid, content string) {
			if _, err := s.originality.AuditChapter(context.Background(), chID, pid, content); err != nil {
				s.logger.Warn("originality audit failed (regenerate)", zap.Error(err))
			}
		}(updated.ID, updated.ProjectID, updated.Content)
	}

	go func(pid, cid, content string) {
		sCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.settleChapterState(sCtx, pid, cid, content)
	}(projectID, updated.ID, updated.Content)

	return updated, nil
}

func (s *ChapterService) ensureChapterWordCount(
	ctx context.Context,
	systemPrompt, content string,
	wordsMin, wordsMax int,
	llmConfig map[string]interface{},
) (string, int, int, error) {
	if wordsMin <= 0 || wordsMax <= 0 {
		return content, 0, 0, nil
	}
	current := utf8.RuneCountInString(content)
	if current >= wordsMin && current <= wordsMax {
		return content, 0, 0, nil
	}

	adjusted := content
	totalInputTokens := 0
	totalOutputTokens := 0

	for attempt := 0; attempt < 2; attempt++ {
		current = utf8.RuneCountInString(adjusted)
		if current >= wordsMin && current <= wordsMax {
			return adjusted, totalInputTokens, totalOutputTokens, nil
		}
		if current > wordsMax {
			s.logger.Warn("chapter over word limit, returning as-is (no post-hoc compression)",
				zap.Int("length", current),
				zap.Int("max", wordsMax))
			return adjusted, totalInputTokens, totalOutputTokens, nil
		}

		resp, err := s.ai.ChatWithConfig(ctx, gateway.ChatRequest{
			Task: "chapter_length_adjustment",
			Messages: []gateway.ChatMessage{
				{Role: "system", Content: systemPrompt + "\n\n补充规则：字数范围是硬约束，如果正文超出范围必须压缩，如果不足范围必须扩写。压缩时优先删减重复的心理描写、冗余环境描写、多余的过渡句；扩写时优先补充场景细节、角色微表情和感官描写，不要添加新事件。结尾不得添加总结/展望段。不得输出解释，只输出修订后的完整正文。"},
				{Role: "user", Content: fmt.Sprintf("当前正文约 %d 字，目标区间是 %d-%d 字。请对下面正文做扩写，使最终长度严格落在目标区间内，同时保留本章核心剧情、人物关系和结尾功能。保持反AI文风规则（禁止使用微微缓缓淡淡等AI高频词）。只输出修订后的完整正文。\n\n%s", current, wordsMin, wordsMax, adjusted)},
			},
		}, llmConfig)
		if err != nil {
			return content, totalInputTokens, totalOutputTokens, fmt.Errorf("adjust chapter length: %w", err)
		}

		adjusted = resp.Content
		totalInputTokens += resp.InputTokens
		totalOutputTokens += resp.OutputTokens
	}

	current = utf8.RuneCountInString(adjusted)
	if current < wordsMin || current > wordsMax {
		s.logger.Warn("chapter length still out of range after adjustment, using best-effort content",
			zap.Int("length", current),
			zap.Int("min", wordsMin),
			zap.Int("max", wordsMax),
		)
		return adjusted, totalInputTokens, totalOutputTokens, nil
	}

	return adjusted, totalInputTokens, totalOutputTokens, nil
}

func (s *ChapterService) logChapterSimilarity(ctx context.Context, projectID, chapterID, content string, chapterNum int) {
	if chapterNum <= 1 {
		return
	}
	rows, err := s.db.Query(ctx,
		`SELECT id, chapter_num, content
		 FROM chapters
		 WHERE project_id = $1 AND chapter_num < $2 AND chapter_num >= $3
		 ORDER BY chapter_num DESC`,
		projectID, chapterNum, max(1, chapterNum-10))
	if err != nil {
		s.logger.Debug("similarity check: failed to load recent chapters", zap.Error(err))
		return
	}
	defer rows.Close()

	newGrams := buildNgramSet(content, 4)
	if len(newGrams) == 0 {
		return
	}

	batch := &pgx.Batch{}
	for rows.Next() {
		var prevID string
		var prevNum int
		var prevContent string
		if rows.Scan(&prevID, &prevNum, &prevContent) != nil {
			continue
		}
		prevGrams := buildNgramSet(prevContent, 4)
		if len(prevGrams) == 0 {
			continue
		}
		sim := jaccardSimilarity(newGrams, prevGrams)
		batch.Queue(
			`INSERT INTO chapter_similarity_log (project_id, chapter_a_id, chapter_b_id, similarity_score, detection_method, created_at)
			 VALUES ($1, $2, $3, $4, 'ngram_jaccard', NOW())
			 ON CONFLICT DO NOTHING`,
			projectID, chapterID, prevID, sim)
		if sim > 0.4 {
			s.logger.Warn("high chapter similarity detected",
				zap.String("project_id", projectID),
				zap.Int("new_chapter", chapterNum),
				zap.Int("prev_chapter", prevNum),
				zap.Float64("similarity", sim))
		}
	}
	if batch.Len() > 0 {
		br := s.db.SendBatch(ctx, batch)
		defer br.Close()
		for i := 0; i < batch.Len(); i++ {
			br.Exec()
		}
	}
}

func buildNgramSet(text string, n int) map[string]struct{} {
	runes := []rune(text)
	filtered := make([]rune, 0, len(runes))
	for _, r := range runes {
		if r != '\n' && r != '\r' && r != ' ' && r != '\t' {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) < n {
		return nil
	}
	set := make(map[string]struct{}, len(filtered)-n+1)
	for i := 0; i <= len(filtered)-n; i++ {
		set[string(filtered[i:i+n])] = struct{}{}
	}
	return set
}

func jaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
