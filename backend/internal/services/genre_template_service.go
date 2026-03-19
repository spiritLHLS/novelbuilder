package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// GenreTemplateService manages the genre_templates table.
type GenreTemplateService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewGenreTemplateService(db *pgxpool.Pool, logger *zap.Logger) *GenreTemplateService {
	return &GenreTemplateService{db: db, logger: logger}
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
		var extraJSON []byte
		if err := rows.Scan(&t.ID, &t.Genre, &t.RulesContent, &t.LanguageConstraints, &t.RhythmRules,
			&extraJSON, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.AuditDimensionsExtra = extraJSON
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *GenreTemplateService) Get(ctx context.Context, genre string) (*models.GenreTemplate, error) {
	var t models.GenreTemplate
	var extraJSON []byte
	err := s.db.QueryRow(ctx,
		`SELECT id, genre, rules_content, language_constraints, rhythm_rules, audit_dimensions_extra, created_at, updated_at
		 FROM genre_templates WHERE genre = $1`, genre).
		Scan(&t.ID, &t.Genre, &t.RulesContent, &t.LanguageConstraints, &t.RhythmRules,
			&extraJSON, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
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
	var rawExtra []byte
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
			&rawExtra, &t.CreatedAt, &t.UpdatedAt)
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
