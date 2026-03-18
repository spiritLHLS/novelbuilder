package services

// EditPropagationService implements a cascade-edit propagation pipeline inspired by
// the Re3 (Retrieve-Rewrite-Rerank) and PEARL (Planning-Editing-And-Reviewing)
// paradigms for maintaining narrative consistency across a novel.
//
// High-level flow:
//  1. When a chapter is generated, RecordChapterDependencies asynchronously records
//     which world_bible/blueprint/characters it was built from.
//  2. When the user edits any entity they call CreateChangeEventWithAnalysis, which:
//     a. Persists the change event (old/new snapshot + summary) in a short TX.
//     b. Finds affected chapters via the dependency graph (falling back to all
//        chapters for the project if no deps are tracked yet).
//     c. Issues ONE LLM call OUTSIDE any open transaction to determine which of
//        those chapters are actually impacted and what the patch instruction is.
//     d. Persists the resulting PatchPlan + PatchItems in a short TX.
//  3. The user reviews the plan (approve / skip each item).
//  4. ExecutePatchItem performs a targeted LLM rewrite for every approved chapter
//     item OUTSIDE a transaction, then commits the new content in a short TX.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// EditPropagationService manages the change propagation pipeline.
type EditPropagationService struct {
	db     *pgxpool.Pool
	ai     *gateway.AIGateway
	logger *zap.Logger
}

// NewEditPropagationService constructs the service.
func NewEditPropagationService(db *pgxpool.Pool, ai *gateway.AIGateway, logger *zap.Logger) *EditPropagationService {
	return &EditPropagationService{db: db, ai: ai, logger: logger}
}

// ─── internal types ───────────────────────────────────────────────────────────

type impactCandidate struct {
	ID         string
	ChapterNum int
	Title      string
	Summary    string
}

// impactResult matches the JSON the LLM is asked to return.
type impactResult struct {
	ItemID            string `json:"item_id"`
	ItemType          string `json:"item_type"`
	Affected          bool   `json:"affected"`
	ImpactDescription string `json:"impact_description"`
	PatchInstruction  string `json:"patch_instruction"`
}

// ─── public API ───────────────────────────────────────────────────────────────

// RecordChapterDependencies records which world_bible/blueprint/characters a
// freshly generated chapter used as context. Called in a goroutine after every
// chapter save; errors are only logged, never propagated.
func (s *EditPropagationService) RecordChapterDependencies(ctx context.Context, projectID, chapterID string) {
	b := &pgx.Batch{}

	var wbID string
	if s.db.QueryRow(ctx,
		`SELECT id FROM world_bibles WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&wbID) == nil {
		b.Queue(
			`INSERT INTO content_dependencies
			     (project_id, dependent_type, dependent_id, source_type, source_id)
			 VALUES ($1,'chapter',$2,'world_bible',$3)
			 ON CONFLICT ON CONSTRAINT uq_content_dep DO NOTHING`,
			projectID, chapterID, wbID)
	}

	var bpID string
	if s.db.QueryRow(ctx,
		`SELECT id FROM book_blueprints WHERE project_id = $1 AND status = 'approved'
		 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&bpID) == nil {
		b.Queue(
			`INSERT INTO content_dependencies
			     (project_id, dependent_type, dependent_id, source_type, source_id)
			 VALUES ($1,'chapter',$2,'blueprint',$3)
			 ON CONFLICT ON CONSTRAINT uq_content_dep DO NOTHING`,
			projectID, chapterID, bpID)
	}

	rows, err := s.db.Query(ctx,
		`SELECT id FROM characters WHERE project_id = $1`, projectID)
	if err == nil {
		for rows.Next() {
			var cid string
			if rows.Scan(&cid) == nil {
				id := cid // capture loop var
				b.Queue(
					`INSERT INTO content_dependencies
					     (project_id, dependent_type, dependent_id, source_type, source_id)
					 VALUES ($1,'chapter',$2,'character',$3)
					 ON CONFLICT ON CONSTRAINT uq_content_dep DO NOTHING`,
					projectID, chapterID, id)
			}
		}
		rows.Close()
	}

	if b.Len() == 0 {
		return
	}
	br := s.db.SendBatch(ctx, b)
	for i := 0; i < b.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			s.logger.Warn("record chapter dep failed",
				zap.Int("index", i), zap.Error(err))
		}
	}
	if err := br.Close(); err != nil {
		s.logger.Warn("chapter dep batch close", zap.Error(err))
	}
}

// CreateChangeEventWithAnalysis persists a change event and produces an
// AI-backed PatchPlan. The LLM call runs OUTSIDE any database transaction.
func (s *EditPropagationService) CreateChangeEventWithAnalysis(
	ctx context.Context,
	projectID string,
	req models.CreateChangeEventRequest,
) (*models.PatchPlan, error) {
	// ── 1. Persist change event (short tx) ───────────────────────────────────
	eventID := uuid.New().String()
	if _, err := s.db.Exec(ctx,
		`INSERT INTO change_events
		     (id, project_id, entity_type, entity_id, change_summary, old_snapshot, new_snapshot)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		eventID, projectID,
		req.EntityType, req.EntityID, req.ChangeSummary,
		nullJSON(req.OldSnapshot), nullJSON(req.NewSnapshot),
	); err != nil {
		return nil, fmt.Errorf("insert change_event: %w", err)
	}

	// ── 2. Find candidate chapters (two-phase: dep graph → all chapters) ─────
	candidates := s.findCandidates(ctx, projectID, req.EntityType, req.EntityID)
	if len(candidates) == 0 {
		return s.createEmptyPlan(ctx, eventID, projectID, "项目中暂无章节需要更新")
	}

	// ── 3. LLM impact analysis (NO open transaction) ─────────────────────────
	impactItems, summary, err := s.analyzeImpact(ctx, req, candidates)
	if err != nil {
		s.logger.Warn("impact analysis failed; falling back to mark-all",
			zap.Error(err))
		for _, c := range candidates {
			impactItems = append(impactItems, impactResult{
				ItemID:            c.ID,
				ItemType:          "chapter",
				Affected:          true,
				ImpactDescription: "自动分析失败，请人工审核",
				PatchInstruction:  req.ChangeSummary,
			})
		}
		summary = "自动分析失败，已将所有相关章节标记为待检查"
	}

	// Keep only actually-affected items; sort by chapter order.
	affected := filterAffected(impactItems)
	sort.Slice(affected, func(i, j int) bool {
		return chapterNumOf(affected[i].ItemID, candidates) <
			chapterNumOf(affected[j].ItemID, candidates)
	})

	// ── 4. Persist plan + items (short tx) ───────────────────────────────────
	planID := uuid.New().String()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx,
		`INSERT INTO patch_plans
		     (id, change_event_id, project_id, impact_summary, total_items, done_items, status)
		 VALUES ($1,$2,$3,$4,$5,0,'ready')`,
		planID, eventID, projectID, summary, len(affected),
	); err != nil {
		return nil, fmt.Errorf("insert patch_plan: %w", err)
	}

	if len(affected) > 0 {
		itemBatch := &pgx.Batch{}
		for i, it := range affected {
			itemBatch.Queue(
				`INSERT INTO patch_items
				     (id, plan_id, item_type, item_id, item_order,
				      impact_description, patch_instruction)
				 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
				uuid.New().String(),
				planID, it.ItemType, it.ItemID, i+1,
				it.ImpactDescription, it.PatchInstruction,
			)
		}
		br := tx.SendBatch(ctx, itemBatch)
		for i := 0; i < itemBatch.Len(); i++ {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return nil, fmt.Errorf("insert patch_item %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return nil, fmt.Errorf("patch_items batch close: %w", err)
		}
	}

	if _, err = tx.Exec(ctx,
		`UPDATE change_events SET status = 'analyzed', updated_at = NOW() WHERE id = $1`,
		eventID,
	); err != nil {
		return nil, fmt.Errorf("update event status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return s.GetPlan(ctx, planID)
}

// GetPlan returns a PatchPlan with all its PatchItems (single JOIN-free query pair).
func (s *EditPropagationService) GetPlan(ctx context.Context, planID string) (*models.PatchPlan, error) {
	var p models.PatchPlan
	if err := s.db.QueryRow(ctx,
		`SELECT id, change_event_id, project_id, impact_summary,
		        total_items, done_items, status, created_at, updated_at
		 FROM patch_plans WHERE id = $1`, planID,
	).Scan(&p.ID, &p.ChangeEventID, &p.ProjectID, &p.ImpactSummary,
		&p.TotalItems, &p.DoneItems, &p.Status, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("get plan: %w", err)
	}

	rows, err := s.db.Query(ctx,
		`SELECT id, plan_id, item_type, item_id, item_order,
		        impact_description, patch_instruction,
		        status, result_snapshot, created_at, updated_at
		 FROM patch_items WHERE plan_id = $1 ORDER BY item_order`, planID)
	if err != nil {
		return nil, fmt.Errorf("get items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var it models.PatchItem
		var rs []byte
		if err := rows.Scan(
			&it.ID, &it.PlanID, &it.ItemType, &it.ItemID, &it.ItemOrder,
			&it.ImpactDescription, &it.PatchInstruction,
			&it.Status, &rs, &it.CreatedAt, &it.UpdatedAt,
		); err != nil {
			return nil, err
		}
		it.ResultSnapshot = json.RawMessage(rs)
		p.Items = append(p.Items, it)
	}
	return &p, nil
}

// ListChangeEvents returns change events for a project, newest-first.
func (s *EditPropagationService) ListChangeEvents(ctx context.Context, projectID string) ([]models.ChangeEvent, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, entity_type, entity_id, change_summary,
		        old_snapshot, new_snapshot, status, created_at, updated_at
		 FROM change_events WHERE project_id = $1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var out []models.ChangeEvent
	for rows.Next() {
		var ev models.ChangeEvent
		var old, newSnap []byte
		if err := rows.Scan(
			&ev.ID, &ev.ProjectID, &ev.EntityType, &ev.EntityID, &ev.ChangeSummary,
			&old, &newSnap, &ev.Status, &ev.CreatedAt, &ev.UpdatedAt,
		); err != nil {
			return nil, err
		}
		ev.OldSnapshot = json.RawMessage(old)
		ev.NewSnapshot = json.RawMessage(newSnap)
		out = append(out, ev)
	}
	return out, nil
}

// UpdatePatchItemStatus sets the status of a single patch item (approve/skip).
func (s *EditPropagationService) UpdatePatchItemStatus(ctx context.Context, itemID, status string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE patch_items SET status = $1, updated_at = NOW() WHERE id = $2`, status, itemID)
	return err
}

// ExecutePatchItem runs the targeted LLM rewrite for an approved chapter item.
// The LLM call runs OUTSIDE any transaction; the DB write is a short atomic TX.
func (s *EditPropagationService) ExecutePatchItem(ctx context.Context, itemID string) error {
	// Load item metadata
	var item models.PatchItem
	if err := s.db.QueryRow(ctx,
		`SELECT id, plan_id, item_type, item_id, patch_instruction
		 FROM patch_items WHERE id = $1`, itemID,
	).Scan(&item.ID, &item.PlanID, &item.ItemType, &item.ItemID, &item.PatchInstruction); err != nil {
		return fmt.Errorf("get item: %w", err)
	}

	// Non-chapter items (outline/foreshadowing) have no auto-rewrite; just mark done.
	if item.ItemType != "chapter" {
		_, err := s.db.Exec(ctx,
			`UPDATE patch_items SET status = 'done', updated_at = NOW() WHERE id = $1`, itemID)
		if err != nil {
			return err
		}
		s.db.Exec(ctx, // nolint:errcheck
			`UPDATE patch_plans SET done_items = done_items + 1, updated_at = NOW()
			 WHERE id = $1`, item.PlanID)
		return nil
	}

	// Fetch chapter content for rewrite
	var content string
	if err := s.db.QueryRow(ctx,
		`SELECT content FROM chapters WHERE id = $1`, item.ItemID,
	).Scan(&content); err != nil {
		return fmt.Errorf("get chapter content: %w", err)
	}

	// Mark as executing so the UI can show progress
	s.db.Exec(ctx, // nolint:errcheck
		`UPDATE patch_items SET status = 'executing', updated_at = NOW() WHERE id = $1`, itemID)

	// ── Targeted LLM rewrite (NO open DB transaction) ────────────────────────
	userMsg := fmt.Sprintf(
		"请根据以下修改说明，对章节内容中涉及的段落进行精确修改，其余内容保持不变。返回完整章节内容。\n\n"+
			"修改说明：\n%s\n\n原章节内容：\n%s",
		item.PatchInstruction, content)

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task: "propagation_patch",
		Messages: []gateway.ChatMessage{
			{
				Role:    "system",
				Content: "你是专业小说编辑，请精准完成指定段落的修改，保持其余内容原封不动，返回完整章节。",
			},
			{Role: "user", Content: userMsg},
		},
	})
	if err != nil {
		s.db.Exec(ctx, // nolint:errcheck
			`UPDATE patch_items SET status = 'failed', updated_at = NOW() WHERE id = $1`, itemID)
		return fmt.Errorf("LLM rewrite: %w", err)
	}
	newContent := resp.Content

	preview := newContent
	if len(preview) > 200 {
		preview = preview[:200]
	}
	resultSnap, _ := json.Marshal(map[string]string{"preview": preview})

	// ── Short TX: update chapter + item + plan counter ───────────────────────
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx,
		`UPDATE chapters
		 SET content = $1, word_count = char_length($1), updated_at = NOW()
		 WHERE id = $2`,
		newContent, item.ItemID,
	); err != nil {
		return fmt.Errorf("update chapter: %w", err)
	}

	if _, err = tx.Exec(ctx,
		`UPDATE patch_items
		 SET status = 'done', result_snapshot = $1, updated_at = NOW()
		 WHERE id = $2`,
		resultSnap, itemID,
	); err != nil {
		return fmt.Errorf("update patch_item: %w", err)
	}

	if _, err = tx.Exec(ctx,
		`UPDATE patch_plans SET done_items = done_items + 1, updated_at = NOW()
		 WHERE id = $1`, item.PlanID,
	); err != nil {
		return fmt.Errorf("update plan counter: %w", err)
	}

	// Auto-close plan once everything is done
	tx.Exec(ctx, // nolint:errcheck
		`UPDATE patch_plans SET status = 'done', updated_at = NOW()
		 WHERE id = $1 AND done_items >= total_items`, item.PlanID)

	return tx.Commit(ctx)
}

// ─── internal helpers ─────────────────────────────────────────────────────────

// findCandidates queries chapters via the dependency graph first.
// Falls back to all project chapters if no tracked deps exist yet
// (e.g. chapters generated before the propagation system was introduced).
func (s *EditPropagationService) findCandidates(
	ctx context.Context,
	projectID, entityType, entityID string,
) []impactCandidate {
	rows, err := s.db.Query(ctx,
		`SELECT ch.id, ch.chapter_num, ch.title, COALESCE(ch.summary,'')
		 FROM chapters ch
		 JOIN content_dependencies dep ON dep.dependent_id = ch.id
		 WHERE ch.project_id = $1
		   AND dep.source_type = $2 AND dep.source_id = $3
		 ORDER BY ch.chapter_num`,
		projectID, entityType, entityID)
	var out []impactCandidate
	if err == nil {
		for rows.Next() {
			var c impactCandidate
			rows.Scan(&c.ID, &c.ChapterNum, &c.Title, &c.Summary)
			out = append(out, c)
		}
		rows.Close()
	}
	if len(out) > 0 {
		return out
	}

	// Fallback: all chapters
	rows, err = s.db.Query(ctx,
		`SELECT id, chapter_num, title, COALESCE(summary,'')
		 FROM chapters WHERE project_id = $1 ORDER BY chapter_num`,
		projectID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var c impactCandidate
		rows.Scan(&c.ID, &c.ChapterNum, &c.Title, &c.Summary)
		out = append(out, c)
	}
	return out
}

// analyzeImpact issues a single LLM call to assess which candidates are impacted
// and generates a targeted patch instruction for each one.
func (s *EditPropagationService) analyzeImpact(
	ctx context.Context,
	req models.CreateChangeEventRequest,
	candidates []impactCandidate,
) ([]impactResult, string, error) {
	var list strings.Builder
	for i, c := range candidates {
		list.WriteString(fmt.Sprintf(
			"[%d] 第%d章《%s》\nID: %s\n摘要: %s\n\n",
			i+1, c.ChapterNum, c.Title, c.ID, c.Summary))
	}

	oldStr := "（无）"
	if len(req.OldSnapshot) > 0 && string(req.OldSnapshot) != "null" {
		oldStr = string(req.OldSnapshot)
	}
	newStr := "（无）"
	if len(req.NewSnapshot) > 0 && string(req.NewSnapshot) != "null" {
		newStr = string(req.NewSnapshot)
	}

	prompt := fmt.Sprintf(
		`你是小说连贯性分析专家。以下是一处修改，请分析哪些章节受到影响并需要更新。

## 修改信息
实体类型：%s
变更摘要：%s
旧内容：%s
新内容：%s

## 候选章节列表
%s

## 输出要求
仅返回JSON数组，格式如下，不包含任何其他文字：
[{"item_id":"<ID>","item_type":"chapter","affected":true,"impact_description":"<原因>","patch_instruction":"<具体修改指令>"}]

不受影响的章节设 "affected":false，impact_description和patch_instruction留空字符串。`,
		req.EntityType, req.ChangeSummary, oldStr, newStr, list.String())

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task: "impact_analysis",
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: "请严格按照JSON数组格式输出，不包含任何额外文字或markdown代码块。"},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, "", err
	}

	raw := extractJSONArray(resp.Content)
	var results []impactResult
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		return nil, "", fmt.Errorf("parse impact JSON: %w (raw snippet: %.200s)", err, raw)
	}

	n := 0
	for _, r := range results {
		if r.Affected {
			n++
		}
	}
	summary := fmt.Sprintf("「%s」影响 %d/%d 个章节需要更新", req.ChangeSummary, n, len(candidates))
	return results, summary, nil
}

func (s *EditPropagationService) createEmptyPlan(
	ctx context.Context, eventID, projectID, summary string,
) (*models.PatchPlan, error) {
	planID := uuid.New().String()
	if _, err := s.db.Exec(ctx,
		`INSERT INTO patch_plans
		     (id, change_event_id, project_id, impact_summary, total_items, done_items, status)
		 VALUES ($1,$2,$3,$4,0,0,'done')`,
		planID, eventID, projectID, summary,
	); err != nil {
		return nil, fmt.Errorf("insert empty plan: %w", err)
	}
	s.db.Exec(ctx, // nolint:errcheck
		`UPDATE change_events SET status = 'done', updated_at = NOW() WHERE id = $1`, eventID)
	return s.GetPlan(ctx, planID)
}

// ─── pure functions ───────────────────────────────────────────────────────────

func filterAffected(items []impactResult) []impactResult {
	var out []impactResult
	for _, it := range items {
		if it.Affected {
			out = append(out, it)
		}
	}
	return out
}

func chapterNumOf(id string, candidates []impactCandidate) int {
	for _, c := range candidates {
		if c.ID == id {
			return c.ChapterNum
		}
	}
	return 9999
}

var jsonArrayRe = regexp.MustCompile(`(?s)\[.*\]`)

// extractJSONArray tries to peel a bare JSON array from an LLM response that
// may be wrapped in markdown code fences or explanatory prose.
func extractJSONArray(s string) string {
	if m := jsonArrayRe.FindString(s); m != "" {
		return m
	}
	return s
}

// nullJSON converts an empty/null json.RawMessage to a nil interface{}
// so pgx stores NULL instead of an empty string in JSONB columns.
func nullJSON(v json.RawMessage) interface{} {
	if len(v) == 0 || string(v) == "null" {
		return nil
	}
	return []byte(v)
}
