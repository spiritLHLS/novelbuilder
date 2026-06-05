package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/database"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

type PromptPresetService struct {
	db     *database.DB
	logger *zap.Logger
}

func NewPromptPresetService(db *database.DB, logger *zap.Logger) *PromptPresetService {
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

	presets := make([]models.PromptPreset, 0)
	for rows.Next() {
		var p models.PromptPreset
		var variables json.RawMessage
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.Name, &p.Description, &p.Category,
			&p.Content, rawJSONScanner{dst: &variables}, &p.IsGlobal, &p.SortOrder,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Variables = variables
		presets = append(presets, p)
	}
	return presets, rows.Err()
}

func (s *PromptPresetService) Get(ctx context.Context, id string) (*models.PromptPreset, error) {
	var p models.PromptPreset
	var variables json.RawMessage
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, name, description, category, content, variables, is_global, sort_order, created_at, updated_at
		 FROM prompt_presets WHERE id = $1`, id).Scan(
		&p.ID, &p.ProjectID, &p.Name, &p.Description, &p.Category,
		&p.Content, rawJSONScanner{dst: &variables}, &p.IsGlobal, &p.SortOrder,
		&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, database.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get prompt_preset: %w", err)
	}
	p.Variables = variables
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
	var variables json.RawMessage
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
		&p.Content, rawJSONScanner{dst: &variables}, &p.IsGlobal, &p.SortOrder,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update prompt_preset: %w", err)
	}
	p.Variables = variables
	return &p, nil
}

func (s *PromptPresetService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM prompt_presets WHERE id = $1`, id)
	return err
}

func buildPromptPresetPromptBlock(ctx context.Context, db *database.DB, projectID string, categories ...string) string {
	if db == nil || strings.TrimSpace(projectID) == "" {
		return ""
	}
	categorySet := make(map[string]bool, len(categories))
	for _, category := range categories {
		category = strings.TrimSpace(category)
		if category != "" {
			categorySet[category] = true
		}
	}

	rows, err := db.Query(ctx,
		`SELECT name, category, content, variables, is_global
		   FROM prompt_presets
		  WHERE project_id = $1 OR is_global = TRUE
		  ORDER BY is_global DESC, sort_order ASC, name ASC`,
		projectID)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var sb strings.Builder
	count := 0
	for rows.Next() {
		var name, category, content string
		var variables json.RawMessage
		var isGlobal bool
		if err := rows.Scan(&name, &category, &content, rawJSONScanner{dst: &variables}, &isGlobal); err != nil {
			continue
		}
		if len(categorySet) > 0 && !categorySet[category] {
			continue
		}
		content = strings.TrimSpace(renderPromptPresetContent(content, variables))
		if content == "" {
			continue
		}
		if count == 0 {
			sb.WriteString("=== 可复用提示词预设（用户配置，必须遵守）===\n")
			sb.WriteString("以下规则来自全局或当前项目的提示词预设，优先级高于通用写作习惯；如与项目级创作规则冲突，以项目级创作规则为准。\n")
		}
		scope := "项目"
		if isGlobal {
			scope = "全局"
		}
		sb.WriteString(fmt.Sprintf("【%s / %s / %s】\n%s\n", scope, category, name, content))
		count++
	}
	if count == 0 {
		return ""
	}
	sb.WriteString("\n")
	return sb.String()
}

func renderPromptPresetContent(content string, variables json.RawMessage) string {
	if len(variables) == 0 {
		return content
	}
	values := map[string]string{}
	if err := json.Unmarshal(variables, &values); err != nil {
		return content
	}
	rendered := content
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		rendered = strings.ReplaceAll(rendered, "{{"+key+"}}", value)
	}
	return rendered
}
