package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// ============================================================
// World Bible Service
// ============================================================

type WorldBibleService struct {
	db     *pgxpool.Pool
	ai     *gateway.AIGateway
	logger *zap.Logger
}

func NewWorldBibleService(db *pgxpool.Pool, ai *gateway.AIGateway, logger *zap.Logger) *WorldBibleService {
	return &WorldBibleService{db: db, ai: ai, logger: logger}
}

func (s *WorldBibleService) Get(ctx context.Context, projectID string) (*models.WorldBible, error) {
	var wb models.WorldBible
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, content, migration_source, version, created_at, updated_at
		 FROM world_bibles WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&wb.ID, &wb.ProjectID, &wb.Content, &wb.MigrationSource,
		&wb.Version, &wb.CreatedAt, &wb.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &wb, err
}

func (s *WorldBibleService) Update(ctx context.Context, projectID string, content json.RawMessage) (*models.WorldBible, error) {
	var wb models.WorldBible
	err := s.db.QueryRow(ctx,
		`UPDATE world_bibles SET content = $1, version = version + 1
		 WHERE project_id = $2
		 RETURNING id, project_id, content, migration_source, version, created_at, updated_at`,
		content, projectID).Scan(&wb.ID, &wb.ProjectID, &wb.Content, &wb.MigrationSource,
		&wb.Version, &wb.CreatedAt, &wb.UpdatedAt)
	return &wb, err
}

func (s *WorldBibleService) GetConstitution(ctx context.Context, projectID string) (*models.WorldBibleConstitution, error) {
	var wbc models.WorldBibleConstitution
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, immutable_rules, mutable_rules, forbidden_anchors, version, created_at, updated_at
		 FROM world_bible_constitutions WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&wbc.ID, &wbc.ProjectID, &wbc.ImmutableRules, &wbc.MutableRules,
		&wbc.ForbiddenAnchors, &wbc.Version, &wbc.CreatedAt, &wbc.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &wbc, err
}

func (s *WorldBibleService) UpdateConstitution(ctx context.Context, projectID string, immutable, mutable, forbidden json.RawMessage) (*models.WorldBibleConstitution, error) {
	var wbc models.WorldBibleConstitution
	// Atomic UPSERT: requires UNIQUE (project_id) constraint added in migration.
	err := s.db.QueryRow(ctx,
		`INSERT INTO world_bible_constitutions (project_id, immutable_rules, mutable_rules, forbidden_anchors)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (project_id) DO UPDATE SET
		     immutable_rules  = EXCLUDED.immutable_rules,
		     mutable_rules    = EXCLUDED.mutable_rules,
		     forbidden_anchors = EXCLUDED.forbidden_anchors,
		     version          = world_bible_constitutions.version + 1
		 RETURNING id, project_id, immutable_rules, mutable_rules, forbidden_anchors, version, created_at, updated_at`,
		projectID, immutable, mutable, forbidden).Scan(&wbc.ID, &wbc.ProjectID, &wbc.ImmutableRules,
		&wbc.MutableRules, &wbc.ForbiddenAnchors, &wbc.Version, &wbc.CreatedAt, &wbc.UpdatedAt)
	return &wbc, err
}

// ============================================================
// Character Service
// ============================================================

type CharacterService struct {
	db     *pgxpool.Pool
	ai     *gateway.AIGateway
	logger *zap.Logger
}

func NewCharacterService(db *pgxpool.Pool, ai *gateway.AIGateway, logger *zap.Logger) *CharacterService {
	return &CharacterService{db: db, ai: ai, logger: logger}
}

func (s *CharacterService) List(ctx context.Context, projectID string) ([]models.Character, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, name, role_type, profile, COALESCE(current_state, '{}'), COALESCE(voice_collection, ''), created_at, updated_at
		 FROM characters WHERE project_id = $1 ORDER BY created_at`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chars []models.Character
	for rows.Next() {
		var c models.Character
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.RoleType, &c.Profile,
			&c.CurrentState, &c.VoiceCollection, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		chars = append(chars, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list characters rows: %w", err)
	}
	return chars, nil
}

func (s *CharacterService) Get(ctx context.Context, id string) (*models.Character, error) {
	var c models.Character
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, name, role_type, profile, COALESCE(current_state, '{}'), COALESCE(voice_collection, ''), created_at, updated_at
		 FROM characters WHERE id = $1`, id).Scan(
		&c.ID, &c.ProjectID, &c.Name, &c.RoleType, &c.Profile,
		&c.CurrentState, &c.VoiceCollection, &c.CreatedAt, &c.UpdatedAt)
	return &c, err
}

func (s *CharacterService) Create(ctx context.Context, projectID string, name, roleType string, profile json.RawMessage) (*models.Character, error) {
	var c models.Character
	err := s.db.QueryRow(ctx,
		`INSERT INTO characters (project_id, name, role_type, profile) VALUES ($1, $2, $3, $4)
		 RETURNING id, project_id, name, role_type, profile, current_state, COALESCE(voice_collection, ''), created_at, updated_at`,
		projectID, name, roleType, profile).Scan(
		&c.ID, &c.ProjectID, &c.Name, &c.RoleType, &c.Profile,
		&c.CurrentState, &c.VoiceCollection, &c.CreatedAt, &c.UpdatedAt)
	return &c, err
}

func (s *CharacterService) Update(ctx context.Context, id string, name, roleType string, profile json.RawMessage) (*models.Character, error) {
	var c models.Character
	err := s.db.QueryRow(ctx,
		`UPDATE characters SET name = $1, role_type = $2, profile = $3
		 WHERE id = $4
		 RETURNING id, project_id, name, role_type, profile, COALESCE(current_state, '{}'), COALESCE(voice_collection, ''), created_at, updated_at`,
		name, roleType, profile, id).Scan(
		&c.ID, &c.ProjectID, &c.Name, &c.RoleType, &c.Profile,
		&c.CurrentState, &c.VoiceCollection, &c.CreatedAt, &c.UpdatedAt)
	return &c, err
}

func (s *CharacterService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM characters WHERE id = $1`, id)
	return err
}

// ============================================================
// Outline Service
// ============================================================

type OutlineService struct {
	db     *pgxpool.Pool
	ai     *gateway.AIGateway
	logger *zap.Logger
}

func NewOutlineService(db *pgxpool.Pool, ai *gateway.AIGateway, logger *zap.Logger) *OutlineService {
	return &OutlineService{db: db, ai: ai, logger: logger}
}

func (s *OutlineService) List(ctx context.Context, projectID string) ([]models.Outline, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, level, parent_id, order_num, title, content, tension_target, created_at, updated_at
		 FROM outlines WHERE project_id = $1 ORDER BY order_num`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outlines []models.Outline
	for rows.Next() {
		var o models.Outline
		if err := rows.Scan(&o.ID, &o.ProjectID, &o.Level, &o.ParentID, &o.OrderNum,
			&o.Title, &o.Content, &o.TensionTarget, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		outlines = append(outlines, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list outlines rows: %w", err)
	}
	return outlines, nil
}

func (s *OutlineService) Create(ctx context.Context, projectID, level string, parentID *string, orderNum int, title string, content json.RawMessage, tension float64) (*models.Outline, error) {
	var o models.Outline
	err := s.db.QueryRow(ctx,
		`INSERT INTO outlines (project_id, level, parent_id, order_num, title, content, tension_target)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, project_id, level, parent_id, order_num, title, content, tension_target, created_at, updated_at`,
		projectID, level, parentID, orderNum, title, content, tension).Scan(
		&o.ID, &o.ProjectID, &o.Level, &o.ParentID, &o.OrderNum,
		&o.Title, &o.Content, &o.TensionTarget, &o.CreatedAt, &o.UpdatedAt)
	return &o, err
}

func (s *OutlineService) Update(ctx context.Context, id string, title string, content json.RawMessage, tension float64) (*models.Outline, error) {
	var o models.Outline
	err := s.db.QueryRow(ctx,
		`UPDATE outlines SET title = $1, content = $2, tension_target = $3
		 WHERE id = $4
		 RETURNING id, project_id, level, parent_id, order_num, title, content, tension_target, created_at, updated_at`,
		title, content, tension, id).Scan(
		&o.ID, &o.ProjectID, &o.Level, &o.ParentID, &o.OrderNum,
		&o.Title, &o.Content, &o.TensionTarget, &o.CreatedAt, &o.UpdatedAt)
	return &o, err
}

func (s *OutlineService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM outlines WHERE id = $1`, id)
	return err
}

// ============================================================
// Foreshadowing Service
// ============================================================

type ForeshadowingService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewForeshadowingService(db *pgxpool.Pool, logger *zap.Logger) *ForeshadowingService {
	return &ForeshadowingService{db: db, logger: logger}
}

func (s *ForeshadowingService) List(ctx context.Context, projectID string) ([]models.Foreshadowing, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, content, embed_chapter_id, resolve_chapter_id,
		        COALESCE(embed_method, ''), COALESCE(resolve_method, ''), priority, status, COALESCE(tags, '{}'), created_at, updated_at
		 FROM foreshadowings WHERE project_id = $1 ORDER BY priority DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.Foreshadowing
	for rows.Next() {
		var f models.Foreshadowing
		if err := rows.Scan(&f.ID, &f.ProjectID, &f.Content, &f.EmbedChapterID, &f.ResolveChapterID,
			&f.EmbedMethod, &f.ResolveMethod, &f.Priority, &f.Status, &f.Tags, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list foreshadowings rows: %w", err)
	}
	return list, nil
}

func (s *ForeshadowingService) Create(ctx context.Context, projectID, content, embedMethod string, priority int) (*models.Foreshadowing, error) {
	var f models.Foreshadowing
	err := s.db.QueryRow(ctx,
		`INSERT INTO foreshadowings (project_id, content, embed_method, priority) VALUES ($1, $2, $3, $4)
		 RETURNING id, project_id, content, embed_chapter_id, resolve_chapter_id,
		           COALESCE(embed_method, ''), COALESCE(resolve_method, ''), priority, status, COALESCE(tags, '{}'), created_at, updated_at`,
		projectID, content, embedMethod, priority).Scan(
		&f.ID, &f.ProjectID, &f.Content, &f.EmbedChapterID, &f.ResolveChapterID,
		&f.EmbedMethod, &f.ResolveMethod, &f.Priority, &f.Status, &f.Tags, &f.CreatedAt, &f.UpdatedAt)
	return &f, err
}

func (s *ForeshadowingService) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := s.db.Exec(ctx, `UPDATE foreshadowings SET status = $1 WHERE id = $2`, status, id)
	return err
}

func (s *ForeshadowingService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM foreshadowings WHERE id = $1`, id)
	return err
}

// ============================================================
// Volume Service
// ============================================================

type VolumeService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewVolumeService(db *pgxpool.Pool, logger *zap.Logger) *VolumeService {
	return &VolumeService{db: db, logger: logger}
}

func (s *VolumeService) List(ctx context.Context, projectID string) ([]models.Volume, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, volume_num, COALESCE(title, ''), blueprint_id, status,
		        COALESCE(chapter_start, 0), COALESCE(chapter_end, 0), COALESCE(review_comment, ''), created_at, updated_at
		 FROM volumes WHERE project_id = $1 ORDER BY volume_num`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vols []models.Volume
	for rows.Next() {
		var v models.Volume
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.VolumeNum, &v.Title, &v.BlueprintID,
			&v.Status, &v.ChapterStart, &v.ChapterEnd, &v.ReviewComment, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		vols = append(vols, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list volumes rows: %w", err)
	}
	return vols, nil
}

func (s *VolumeService) SubmitReview(ctx context.Context, id string) error {
	// Check all chapters in this volume are approved
	var unapproved int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM chapters c JOIN volumes v ON c.volume_id = v.id
		 WHERE v.id = $1 AND c.status != 'approved'`, id).Scan(&unapproved)
	if err != nil {
		return err
	}
	if unapproved > 0 {
		return fmt.Errorf("volume has %d unapproved chapters", unapproved)
	}

	_, err = s.db.Exec(ctx, `UPDATE volumes SET status = 'pending_review' WHERE id = $1 AND status = 'draft'`, id)
	return err
}

func (s *VolumeService) Approve(ctx context.Context, id, comment string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE volumes SET status = 'approved', review_comment = $1 WHERE id = $2 AND status = 'pending_review'`,
		comment, id)
	return err
}

func (s *VolumeService) Reject(ctx context.Context, id, comment string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE volumes SET status = 'rejected', review_comment = $1 WHERE id = $2 AND status = 'pending_review'`,
		comment, id)
	return err
}

// ============================================================
// Quality Check Service (4-role review chain)
// ============================================================

type QualityService struct {
	db     *pgxpool.Pool
	ai     *gateway.AIGateway
	logger *zap.Logger
}

func NewQualityService(db *pgxpool.Pool, ai *gateway.AIGateway, logger *zap.Logger) *QualityService {
	return &QualityService{db: db, ai: ai, logger: logger}
}

func (s *QualityService) RunFullCheck(ctx context.Context, chapterID string) (*models.QualityReport, error) {
	// Get chapter content
	var content, projectID string
	err := s.db.QueryRow(ctx,
		`SELECT content, project_id FROM chapters WHERE id = $1`, chapterID).Scan(&content, &projectID)
	if err != nil {
		return nil, err
	}

	report := &models.QualityReport{
		WorldConsistency: true,
		CharConsistency:  true,
		TimeConsistency:  true,
		Pass:             true,
	}

	// Run 4-role review chain in parallel conceptually, but sequentially for reliability
	// Role 1: Senior Editor (retention & pacing)
	editorIssues, _ := s.reviewAsEditor(ctx, content)
	report.Issues = append(report.Issues, editorIssues...)

	// Role 2: Loyal Reader (detect AI-ness)
	readerIssues, _ := s.reviewAsReader(ctx, content)
	report.Issues = append(report.Issues, readerIssues...)

	// Role 3: Logic Reviewer (consistency)
	logicIssues, _ := s.reviewAsLogicReviewer(ctx, content, projectID)
	report.Issues = append(report.Issues, logicIssues...)

	// Role 4: Anti-AI Expert (AI detection)
	aiIssues, aiScore := s.reviewAsAntiAIExpert(ctx, content)
	report.Issues = append(report.Issues, aiIssues...)
	report.AIScoreEstimate = aiScore

	// Calculate overall score
	criticalCount := 0
	for _, issue := range report.Issues {
		if issue.Severity == "critical" {
			criticalCount++
		}
	}
	report.OverallScore = float64(100 - criticalCount*15 - len(report.Issues)*5)
	if report.OverallScore < 0 {
		report.OverallScore = 0
	}
	report.Pass = report.OverallScore >= 60 && report.AIScoreEstimate <= 40

	// Save report to chapter
	reportJSON, _ := json.Marshal(report)
	_, err = s.db.Exec(ctx,
		`UPDATE chapters SET quality_report = $1 WHERE id = $2`,
		reportJSON, chapterID)

	return report, err
}

func (s *QualityService) reviewAsEditor(ctx context.Context, content string) ([]models.QualityIssue, error) {
	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: `你是一位资深网文编辑，从读者留存和阅读体验角度审核章节质量。

请检查以下方面并以JSON数组返回问题列表：
1. 节奏是否拖沓或过快
2. 爽点是否充足（每千字至少1个小爽点）
3. 章末钩子是否有力（读者是否想看下一章）
4. 描写是否过度或不足
5. 对话是否自然有趣

返回格式：[{"type": "pacing|hook|description|dialogue", "severity": "critical|warning|info", "location": "第X段", "message": "问题描述", "suggestion": "改进建议"}]
只返回JSON数组，不要其他文字。`},
			{Role: "user", Content: content},
		},
		TaskType:    "review_chain",
		MaxTokens:   2000,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, err
	}
	return parseIssues(resp.Content), nil
}

func (s *QualityService) reviewAsReader(ctx context.Context, content string) ([]models.QualityIssue, error) {
	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: `你是一位资深网文读者，善于识别AI生成的文本"味道"。

请检查以下AI味特征：
1. 对话是否每句都完整规范（人类对话有省略、打断、答非所问）
2. 情绪是否直白表达（"他感到难过"是AI味，用行为/感官暗示是人味）
3. 叙事是否过于线性（缺少插叙、倒叙、闪回）
4. 句子长度是否过于均匀（人类写作有极短句和极长句交替）
5. 主语是否全程在场（人类中文写作常省略主语）
6. 是否使用了"他心想"/"想到这里"等AI常用过渡

返回格式：[{"type": "ai_smell", "severity": "critical|warning|info", "location": "第X段/第X句", "message": "问题描述", "suggestion": "具体改写方向"}]
只返回JSON数组。`},
			{Role: "user", Content: content},
		},
		TaskType:    "review_chain",
		MaxTokens:   2000,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, err
	}
	return parseIssues(resp.Content), nil
}

func (s *QualityService) reviewAsLogicReviewer(ctx context.Context, content, projectID string) ([]models.QualityIssue, error) {
	// Get world bible for consistency check
	var worldContent string
	s.db.QueryRow(ctx,
		`SELECT content::text FROM world_bibles WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&worldContent)

	// Get character profiles
	var charInfo []string
	if rows, err := s.db.Query(ctx,
		`SELECT name, profile::text FROM characters WHERE project_id = $1`, projectID); err != nil {
		s.logger.Warn("logic reviewer: failed to load characters", zap.Error(err))
	} else {
		for rows.Next() {
			var name, profile string
			if rows.Scan(&name, &profile) == nil {
				charInfo = append(charInfo, fmt.Sprintf("%s: %s", name, profile))
			}
		}
		rows.Close()
	}

	systemPrompt := fmt.Sprintf(`你是一位逻辑审稿人，专门检查小说的世界观、人物、时间线一致性。

世界观设定参考：
%s

角色信息：
%s

请检查以下方面：
1. 角色行为是否符合其性格设定
2. 世界观规则是否被违反
3. 时间线是否有矛盾
4. 物品/能力/状态是否前后一致

返回格式：[{"type": "world|character|timeline|logic", "severity": "critical|warning|info", "location": "第X段", "message": "问题描述", "suggestion": "修正建议"}]
只返回JSON数组。`, worldContent, strings.Join(charInfo, "\n"))

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: content},
		},
		TaskType:    "review_chain",
		MaxTokens:   2000,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, err
	}
	return parseIssues(resp.Content), nil
}

func (s *QualityService) reviewAsAntiAIExpert(ctx context.Context, content string) ([]models.QualityIssue, float64) {
	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: `你是一位反AI检测专家，精通各种AI生成文本的检测方法。

请从以下维度分析这段文本的AI特征：
1. **困惑度评估**：词汇选择是否过于"安全"（高概率词多=AI味重）
2. **爆发度评估**：句子长度的变异系数（CV）是否过低（AI生成句长均匀）
3. **逻辑指纹**：是否存在"问题→分析→结论"的线性逻辑结构
4. **对话特征**：对话是否每句完整（AI）vs有省略打断（人类）
5. **标记段落**：标出AI特征最明显的具体段落

返回格式（必须严格JSON）：
{
  "ai_score": 0-100的数字(越高越像AI),
  "issues": [{"type": "ai_detection", "severity": "critical|warning|info", "location": "第X段第Y句", "message": "具体AI特征描述", "suggestion": "具体改写为XXX"}]
}
只返回JSON。`},
			{Role: "user", Content: content},
		},
		TaskType:    "review_chain",
		MaxTokens:   2000,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, 50
	}

	// Parse response
	respContent := resp.Content
	if idx := strings.Index(respContent, "{"); idx >= 0 {
		if endIdx := strings.LastIndex(respContent, "}"); endIdx >= 0 {
			respContent = respContent[idx : endIdx+1]
		}
	}

	var result struct {
		AIScore float64               `json:"ai_score"`
		Issues  []models.QualityIssue `json:"issues"`
	}
	if err := json.Unmarshal([]byte(respContent), &result); err != nil {
		return nil, 50
	}

	return result.Issues, result.AIScore
}

// VocabFatigueReport analyzes word frequency across all chapters for vocabulary fatigue detection.
// Inspired by InkOS vocab fatigue detection.
func (s *QualityService) VocabFatigueReport(ctx context.Context, projectID string, topN int) (*models.VocabFatigueReport, error) {
	rows, err := s.db.Query(ctx,
		`SELECT content, chapter_num FROM chapters WHERE project_id = $1 AND status != 'rejected'
		 ORDER BY chapter_num`, projectID)
	if err != nil {
		return nil, fmt.Errorf("vocab fatigue: %w", err)
	}
	defer rows.Close()

	wordChapters := make(map[string]map[int]bool)
	wordTotal := make(map[string]int)
	totalChapters := 0
	chapterRe := regexp.MustCompile(`[\p{Han}]+|[a-zA-Z]+`)

	for rows.Next() {
		var content string
		var chapterNum int
		if err := rows.Scan(&content, &chapterNum); err != nil {
			continue
		}
		totalChapters++
		words := chapterRe.FindAllString(content, -1)
		seen := make(map[string]bool)
		for _, w := range words {
			// normalize: lowercase, skip non-English single chars and short words
			if len([]rune(w)) < 2 && !isChinese(w) {
				continue
			}
			w = strings.ToLower(w)
			wordTotal[w]++
			if !seen[w] {
				seen[w] = true
				if wordChapters[w] == nil {
					wordChapters[w] = make(map[int]bool)
				}
				wordChapters[w][chapterNum] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type stat struct {
		word  string
		total int
		chaps int
	}
	var stats []stat
	for w, total := range wordTotal {
		if total < 3 {
			continue // ignore rare words
		}
		stats = append(stats, stat{w, total, len(wordChapters[w])})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].total > stats[j].total
	})
	if len(stats) > topN {
		stats = stats[:topN]
	}

	result := make([]models.VocabFatigueStat, 0, len(stats))
	for _, s := range stats {
		freq := 0.0
		if totalChapters > 0 {
			freq = float64(s.total) / float64(totalChapters)
		}
		result = append(result, models.VocabFatigueStat{
			Word:                s.word,
			TotalCount:          s.total,
			ChaptersAppeared:    s.chaps,
			FrequencyPerChapter: freq,
		})
	}

	return &models.VocabFatigueReport{
		ProjectID:     projectID,
		TopWords:      result,
		TotalChapters: totalChapters,
		AnalyzedAt:    time.Now(),
	}, nil
}

func isChinese(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func parseIssues(content string) []models.QualityIssue {
	// Try to extract JSON array
	content = strings.TrimSpace(content)
	if idx := strings.Index(content, "["); idx >= 0 {
		if endIdx := strings.LastIndex(content, "]"); endIdx >= 0 {
			content = content[idx : endIdx+1]
		}
	}

	var issues []models.QualityIssue
	if err := json.Unmarshal([]byte(content), &issues); err != nil {
		return nil
	}
	return issues
}

// ============================================================
// Reference Material Service
// ============================================================

type ReferenceService struct {
	db         *pgxpool.Pool
	sidecarURL string
	rag        *RAGService
	logger     *zap.Logger
}

func NewReferenceService(db *pgxpool.Pool, sidecarURL string, rag *RAGService, logger *zap.Logger) *ReferenceService {
	return &ReferenceService{db: db, sidecarURL: sidecarURL, rag: rag, logger: logger}
}

func (s *ReferenceService) Create(ctx context.Context, projectID, title, author, genre, filePath string) (*models.ReferenceMaterial, error) {
	var ref models.ReferenceMaterial
	err := s.db.QueryRow(ctx,
		`INSERT INTO reference_materials (project_id, title, author, genre, file_path, status)
		 VALUES ($1, $2, $3, $4, $5, 'processing')
		 RETURNING id, project_id, title, author, genre, file_path, status, created_at`,
		projectID, title, author, genre, filePath).Scan(
		&ref.ID, &ref.ProjectID, &ref.Title, &ref.Author, &ref.Genre, &ref.FilePath,
		&ref.Status, &ref.CreatedAt)
	return &ref, err
}

func (s *ReferenceService) Get(ctx context.Context, id string) (*models.ReferenceMaterial, error) {
	var ref models.ReferenceMaterial
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, title, author, genre, COALESCE(file_path, ''),
		        COALESCE(style_layer, '{}'), COALESCE(narrative_layer, '{}'), COALESCE(atmosphere_layer, '{}'),
		        COALESCE(migration_config, '{}'), COALESCE(style_collection, ''), status, created_at,
		        sample_texts
		 FROM reference_materials WHERE id = $1`, id).Scan(
		&ref.ID, &ref.ProjectID, &ref.Title, &ref.Author, &ref.Genre, &ref.FilePath,
		&ref.StyleLayer, &ref.NarrativeLayer, &ref.AtmosphereLayer,
		&ref.MigrationConfig, &ref.StyleCollection, &ref.Status, &ref.CreatedAt,
		&ref.SampleTexts)
	return &ref, err
}

func (s *ReferenceService) List(ctx context.Context, projectID string) ([]models.ReferenceMaterial, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, title, author, genre, COALESCE(file_path, ''),
		        COALESCE(style_layer, '{}'), COALESCE(narrative_layer, '{}'), COALESCE(atmosphere_layer, '{}'),
		        COALESCE(migration_config, '{}'), COALESCE(style_collection, ''), status, created_at,
		        sample_texts
		 FROM reference_materials WHERE project_id = $1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []models.ReferenceMaterial
	for rows.Next() {
		var ref models.ReferenceMaterial
		if err := rows.Scan(&ref.ID, &ref.ProjectID, &ref.Title, &ref.Author, &ref.Genre, &ref.FilePath,
			&ref.StyleLayer, &ref.NarrativeLayer, &ref.AtmosphereLayer,
			&ref.MigrationConfig, &ref.StyleCollection, &ref.Status, &ref.CreatedAt,
			&ref.SampleTexts); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (s *ReferenceService) UpdateMigrationConfig(ctx context.Context, id string, config json.RawMessage) error {
	_, err := s.db.Exec(ctx,
		`UPDATE reference_materials SET migration_config = $1 WHERE id = $2`,
		config, id)
	return err
}

func (s *ReferenceService) UpdateAnalysis(ctx context.Context, id string, style, narrative, atmosphere json.RawMessage) error {
	_, err := s.db.Exec(ctx,
		`UPDATE reference_materials SET style_layer = $1, narrative_layer = $2, atmosphere_layer = $3, status = 'completed'
		 WHERE id = $4`,
		style, narrative, atmosphere, id)
	return err
}

// IngestSamples vectorises style and sensory text samples extracted from a reference material
// and stores them in the vector_store. It first clears any existing vectors for that source,
// then caches the raw sample texts in reference_materials.sample_texts for future rebuilds.
func (s *ReferenceService) IngestSamples(ctx context.Context, projectID, refID string, styleSamples, sensorySamples []string) error {
	if s.rag == nil {
		return nil
	}

	// Persist sample texts so rebuild doesn't require re-reading files
	type sampleCache struct {
		Style   []string `json:"style"`
		Sensory []string `json:"sensory"`
	}
	cacheJSON, _ := json.Marshal(sampleCache{Style: styleSamples, Sensory: sensorySamples})
	s.db.Exec(ctx, `UPDATE reference_materials SET sample_texts = $1 WHERE id = $2`, cacheJSON, refID)

	// Delete old vectors for this reference before re-inserting
	if err := s.rag.DeleteBySourceID(ctx, projectID, refID); err != nil {
		s.logger.Warn("failed to clear old vectors for reference", zap.String("ref_id", refID), zap.Error(err))
	}

	meta := map[string]interface{}{"ref_id": refID}

	// Build batch items for all samples (one DB round-trip + bounded concurrent embedding calls)
	items := make([]BatchEmbedItem, 0, len(styleSamples)+len(sensorySamples))
	for _, sample := range styleSamples {
		items = append(items, BatchEmbedItem{
			Collection: "style_samples",
			Content:    sample,
			SourceType: "reference",
			SourceID:   refID,
			Metadata:   meta,
		})
	}
	for _, sample := range sensorySamples {
		items = append(items, BatchEmbedItem{
			Collection: "sensory_samples",
			Content:    sample,
			SourceType: "reference",
			SourceID:   refID,
			Metadata:   meta,
		})
	}
	return s.rag.StoreEmbeddingBatch(ctx, projectID, items)
}

// RebuildProject re-ingests all cached samples for every completed reference in a project.
// Returns the number of reference materials processed.
func (s *ReferenceService) RebuildProject(ctx context.Context, projectID string) (int, error) {
	if s.rag == nil {
		return 0, fmt.Errorf("RAG service not configured")
	}

	// Clear ALL vectors for the project so we start fresh
	if err := s.rag.DeleteForProject(ctx, projectID); err != nil {
		return 0, fmt.Errorf("clear project vectors: %w", err)
	}

	// Fetch all completed references that have cached samples (single query)
	rows, err := s.db.Query(ctx,
		`SELECT id, sample_texts FROM reference_materials
		 WHERE project_id = $1 AND status = 'completed' AND sample_texts IS NOT NULL`,
		projectID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type sampleCache struct {
		Style   []string `json:"style"`
		Sensory []string `json:"sensory"`
	}

	// Collect all batch items across all references before hitting the DB again
	var allItems []BatchEmbedItem
	rebuilt := 0
	for rows.Next() {
		var refID string
		var samplesRaw []byte
		if err := rows.Scan(&refID, &samplesRaw); err != nil {
			continue
		}
		var cache sampleCache
		if err := json.Unmarshal(samplesRaw, &cache); err != nil {
			continue
		}
		meta := map[string]interface{}{"ref_id": refID}
		for _, sample := range cache.Style {
			allItems = append(allItems, BatchEmbedItem{
				Collection: "style_samples",
				Content:    sample,
				SourceType: "reference",
				SourceID:   refID,
				Metadata:   meta,
			})
		}
		for _, sample := range cache.Sensory {
			allItems = append(allItems, BatchEmbedItem{
				Collection: "sensory_samples",
				Content:    sample,
				SourceType: "reference",
				SourceID:   refID,
				Metadata:   meta,
			})
		}
		rebuilt++
	}
	rows.Close()

	if err := s.rag.StoreEmbeddingBatch(ctx, projectID, allItems); err != nil {
		return rebuilt, err
	}
	return rebuilt, nil
}
