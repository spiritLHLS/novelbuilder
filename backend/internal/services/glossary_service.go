package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

type GlossaryService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewGlossaryService(db *pgxpool.Pool, logger *zap.Logger) *GlossaryService {
	return &GlossaryService{db: db, logger: logger}
}

func (s *GlossaryService) List(ctx context.Context, projectID string) ([]models.GlossaryTerm, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, term, definition, aliases, category, created_at, updated_at
		 FROM glossary_terms WHERE project_id = $1 ORDER BY category, term`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list glossary: %w", err)
	}
	defer rows.Close()

	terms := []models.GlossaryTerm{}
	for rows.Next() {
		var t models.GlossaryTerm
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Term, &t.Definition,
			&t.Aliases, &t.Category, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		terms = append(terms, t)
	}
	return terms, rows.Err()
}

func (s *GlossaryService) Create(ctx context.Context, projectID string, req models.CreateGlossaryTermRequest) (*models.GlossaryTerm, error) {
	id := uuid.New().String()
	now := time.Now()

	aliases := req.Aliases
	if aliases == nil {
		aliases = []byte("[]")
	}

	_, err := s.db.Exec(ctx,
		`INSERT INTO glossary_terms (id, project_id, term, definition, aliases, category, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
		id, projectID, req.Term, req.Definition, aliases, req.Category, now)
	if err != nil {
		return nil, fmt.Errorf("create glossary_term: %w", err)
	}

	return &models.GlossaryTerm{
		ID: id, ProjectID: projectID, Term: req.Term,
		Definition: req.Definition, Aliases: aliases, Category: req.Category,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *GlossaryService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM glossary_terms WHERE id = $1`, id)
	return err
}

// BuildPromptBlock returns a formatted glossary section for LLM system prompts.
// Returns empty string when the project has no glossary terms.
func (s *GlossaryService) BuildPromptBlock(ctx context.Context, projectID string) string {
	terms, err := s.List(ctx, projectID)
	if err != nil || len(terms) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n## 术语表 / Glossary\n\n")
	sb.WriteString("以下是本项目的专有术语定义，请在后续创作中严格遵守：\n\n")

	prevCat := ""
	for _, t := range terms {
		if t.Category != prevCat {
			prevCat = t.Category
			if t.Category != "" {
				sb.WriteString(fmt.Sprintf("### %s\n", t.Category))
			}
		}
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", t.Term, t.Definition))
	}

	return sb.String()
}
