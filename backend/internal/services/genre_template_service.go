package services

import (
	"context"
	"encoding/json"
	"github.com/novelbuilder/backend/internal/database"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
	"time"
)

// GenreTemplateService manages the genre_templates table.
type GenreTemplateService struct {
	db     *database.DB
	logger *zap.Logger
}

func NewGenreTemplateService(db *database.DB, logger *zap.Logger) *GenreTemplateService {
	return &GenreTemplateService{db: db, logger: logger}
}

func (s *GenreTemplateService) EnsureDefaults(ctx context.Context) error {
	defaults := []models.UpsertGenreTemplateRequest{
		{
			RulesContent:        "世界观、力量体系、代价与成长阶段必须自洽；战斗描写要体现规则而非单纯数值碾压。",
			LanguageConstraints: "用语宏大但保持可读；人名、地名、功法名保持同一体系。",
			RhythmRules:         "铺垫段落可舒展，冲突段落用更短句；每章至少有清晰信息增量。",
		},
		{
			RulesContent:        "人物关系、职业、金钱和城市生活逻辑要贴近现实；爽点需要可验证的动机和代价。",
			LanguageConstraints: "对话贴近当代中文语境，生活细节多样，不默认咖啡、排骨、日料等模板物件。",
			RhythmRules:         "对话密度较高，章节中段推进选择，结尾保留具体悬念。",
		},
		{
			RulesContent:        "技术设定、资源约束、组织结构和风险后果保持一致，不用万能科技解决冲突。",
			LanguageConstraints: "术语准确并少量重复巩固，避免把解释写成百科条目。",
			RhythmRules:         "概念解释后必须进入行动或冲突，避免连续静态说明。",
		},
		{
			RulesContent:        "官职、礼仪、地理、战争和制度要符合所设时代；架空改动要提前给出规则。",
			LanguageConstraints: "称谓和口吻统一，慎用明显现代词汇。",
			RhythmRules:         "政治/战争段落压缩信息密度，日常段落服务人物选择。",
		},
		{
			RulesContent:        "线索、误导、证据和揭示顺序必须可回溯，不能靠临时新增设定解谜。",
			LanguageConstraints: "描述克制，避免直接替读者下结论。",
			RhythmRules:         "每章至少推进一个线索状态：出现、反证、变形或回收。",
		},
		{
			RulesContent:        "亲密关系推进必须依赖具体互动和选择，不靠误会模板或情绪突变。",
			LanguageConstraints: "情绪表达要具体，少用泛化心跳、眼眶、沉默模板。",
			RhythmRules:         "关系升温和冲突交替出现，结尾落在行动或未说出口的信息上。",
		},
	}
	genres := []string{"玄幻", "都市", "科幻", "历史", "悬疑", "言情"}
	batch := &database.Batch{}
	for i, genre := range genres {
		req := defaults[i]
		batch.Queue(
			`INSERT INTO genre_templates (genre, rules_content, language_constraints, rhythm_rules, audit_dimensions_extra, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, '{}'::jsonb, NOW(), NOW())
			 ON CONFLICT (genre) DO NOTHING`,
			genre, req.RulesContent, req.LanguageConstraints, req.RhythmRules,
		)
	}
	br := s.db.SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (s *GenreTemplateService) List(ctx context.Context) ([]models.GenreTemplate, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, genre, rules_content, language_constraints, rhythm_rules, audit_dimensions_extra, created_at, updated_at
		 FROM genre_templates ORDER BY genre`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.GenreTemplate
	for rows.Next() {
		var t models.GenreTemplate
		var extraJSON json.RawMessage
		if err := rows.Scan(&t.ID, &t.Genre, &t.RulesContent, &t.LanguageConstraints, &t.RhythmRules,
			rawJSONScanner{dst: &extraJSON}, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.AuditDimensionsExtra = extraJSON
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *GenreTemplateService) Get(ctx context.Context, genre string) (*models.GenreTemplate, error) {
	var t models.GenreTemplate
	var extraJSON json.RawMessage
	err := s.db.QueryRow(ctx,
		`SELECT id, genre, rules_content, language_constraints, rhythm_rules, audit_dimensions_extra, created_at, updated_at
		 FROM genre_templates WHERE genre = $1`, genre).
		Scan(&t.ID, &t.Genre, &t.RulesContent, &t.LanguageConstraints, &t.RhythmRules,
			rawJSONScanner{dst: &extraJSON}, &t.CreatedAt, &t.UpdatedAt)
	if err == database.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.AuditDimensionsExtra = extraJSON
	return &t, nil
}

func (s *GenreTemplateService) Upsert(ctx context.Context, genre string, req models.UpsertGenreTemplateRequest) (*models.GenreTemplate, error) {
	extraJSON := req.AuditDimensionsExtra
	if extraJSON == nil {
		extraJSON = json.RawMessage(`{}`)
	}

	var t models.GenreTemplate
	var rawExtra json.RawMessage
	now := time.Now()
	err := s.db.QueryRow(ctx,
		`INSERT INTO genre_templates (genre, rules_content, language_constraints, rhythm_rules, audit_dimensions_extra, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)
		 ON CONFLICT (genre) DO UPDATE
		    SET rules_content        = EXCLUDED.rules_content,
		        language_constraints = EXCLUDED.language_constraints,
		        rhythm_rules         = EXCLUDED.rhythm_rules,
		        audit_dimensions_extra = EXCLUDED.audit_dimensions_extra,
		        updated_at           = EXCLUDED.updated_at
		 RETURNING id, genre, rules_content, language_constraints, rhythm_rules, audit_dimensions_extra, created_at, updated_at`,
		genre, req.RulesContent, req.LanguageConstraints, req.RhythmRules, extraJSON, now).
		Scan(&t.ID, &t.Genre, &t.RulesContent, &t.LanguageConstraints, &t.RhythmRules,
			rawJSONScanner{dst: &rawExtra}, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.AuditDimensionsExtra = rawExtra
	return &t, nil
}

func (s *GenreTemplateService) Delete(ctx context.Context, genre string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM genre_templates WHERE genre = $1`, genre)
	return err
}
