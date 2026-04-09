package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

type PromptPresetService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewPromptPresetService(db *pgxpool.Pool, logger *zap.Logger) *PromptPresetService {
	return &PromptPresetService{db: db, logger: logger}
}

// List returns presets for a project plus all global presets.
// If projectID is nil, only global presets are returned.
func (s *PromptPresetService) List(ctx context.Context, projectID *string) ([]models.PromptPreset, error) {
	var (
		queryStr string
		args     []any
	)
	if projectID != nil {
		queryStr = `SELECT id, project_id, name, description, category, content, variables, is_global, sort_order, created_at, updated_at
			 FROM prompt_presets
			 WHERE project_id = $1 OR is_global = TRUE
			 ORDER BY is_global DESC, sort_order ASC, name ASC`
		args = []any{*projectID}
	} else {
		queryStr = `SELECT id, project_id, name, description, category, content, variables, is_global, sort_order, created_at, updated_at
			 FROM prompt_presets WHERE is_global = TRUE
			 ORDER BY sort_order ASC, name ASC`
	}

	rows, err := s.db.Query(ctx, queryStr, args...)
	if err != nil {
		return nil, fmt.Errorf("list prompt_presets: %w", err)
	}
	defer rows.Close()

	var presets []models.PromptPreset
	for rows.Next() {
		var p models.PromptPreset
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.Name, &p.Description, &p.Category,
			&p.Content, &p.Variables, &p.IsGlobal, &p.SortOrder,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		presets = append(presets, p)
	}
	return presets, rows.Err()
}

func (s *PromptPresetService) Get(ctx context.Context, id string) (*models.PromptPreset, error) {
	var p models.PromptPreset
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, name, description, category, content, variables, is_global, sort_order, created_at, updated_at
		 FROM prompt_presets WHERE id = $1`, id).Scan(
		&p.ID, &p.ProjectID, &p.Name, &p.Description, &p.Category,
		&p.Content, &p.Variables, &p.IsGlobal, &p.SortOrder,
		&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get prompt_preset: %w", err)
	}
	return &p, nil
}

func (s *PromptPresetService) Create(ctx context.Context, projectID *string, req models.CreatePromptPresetRequest) (*models.PromptPreset, error) {
	id := uuid.New().String()
	now := time.Now()

	variables := req.Variables
	if variables == nil {
		variables = []byte("[]")
	}

	_, err := s.db.Exec(ctx,
		`INSERT INTO prompt_presets (id, project_id, name, description, category, content, variables, is_global, sort_order, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)`,
		id, projectID, req.Name, req.Description, req.Category,
		req.Content, variables, req.IsGlobal, req.SortOrder, now)
	if err != nil {
		return nil, fmt.Errorf("create prompt_preset: %w", err)
	}

	return &models.PromptPreset{
		ID: id, ProjectID: projectID, Name: req.Name,
		Description: req.Description, Category: req.Category,
		Content: req.Content, Variables: variables,
		IsGlobal: req.IsGlobal, SortOrder: req.SortOrder,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *PromptPresetService) Update(ctx context.Context, id string, req models.UpdatePromptPresetRequest) (*models.PromptPreset, error) {
	now := time.Now()

	var p models.PromptPreset
	err := s.db.QueryRow(ctx,
		`UPDATE prompt_presets SET
		   name = COALESCE(NULLIF($1,''), name),
		   description = COALESCE(NULLIF($2,''), description),
		   category = COALESCE(NULLIF($3,''), category),
		   content = COALESCE(NULLIF($4,''), content),
		   variables = COALESCE($5, variables),
		   is_global = COALESCE($6, is_global),
		   sort_order = COALESCE($7, sort_order),
		   updated_at = $8
		 WHERE id = $9
		 RETURNING id, project_id, name, description, category, content, variables, is_global, sort_order, created_at, updated_at`,
		req.Name, req.Description, req.Category, req.Content,
		req.Variables, req.IsGlobal, req.SortOrder, now, id).Scan(
		&p.ID, &p.ProjectID, &p.Name, &p.Description, &p.Category,
		&p.Content, &p.Variables, &p.IsGlobal, &p.SortOrder,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update prompt_preset: %w", err)
	}
	return &p, nil
}

func (s *PromptPresetService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM prompt_presets WHERE id = $1`, id)
	return err
}
