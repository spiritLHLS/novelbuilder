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

// ResourceLedgerService manages the story resource ledger inspired by InkOS particle_ledger.
// It tracks named resources (items, currency, relationships, etc.) and their per-chapter deltas.
type ResourceLedgerService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewResourceLedgerService(db *pgxpool.Pool, logger *zap.Logger) *ResourceLedgerService {
	return &ResourceLedgerService{db: db, logger: logger}
}

func (s *ResourceLedgerService) List(ctx context.Context, projectID string) ([]models.StoryResource, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, name, category, quantity, unit, description, holder, created_at, updated_at
		 FROM story_resources WHERE project_id = $1 ORDER BY category, name`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list story_resources: %w", err)
	}
	defer rows.Close()

	var resources []models.StoryResource
	for rows.Next() {
		var r models.StoryResource
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Category,
			&r.Quantity, &r.Unit, &r.Description, &r.Holder, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, rows.Err()
}

func (s *ResourceLedgerService) Get(ctx context.Context, id string) (*models.StoryResource, error) {
	var r models.StoryResource
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, name, category, quantity, unit, description, holder, created_at, updated_at
		 FROM story_resources WHERE id = $1`, id).Scan(
		&r.ID, &r.ProjectID, &r.Name, &r.Category,
		&r.Quantity, &r.Unit, &r.Description, &r.Holder, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get story_resource: %w", err)
	}
	return &r, nil
}

func (s *ResourceLedgerService) Create(ctx context.Context, projectID string, req models.CreateStoryResourceRequest) (*models.StoryResource, error) {
	id := uuid.New().String()
	now := time.Now()

	_, err := s.db.Exec(ctx,
		`INSERT INTO story_resources (id, project_id, name, category, quantity, unit, description, holder, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)`,
		id, projectID, req.Name, req.Category, req.Quantity, req.Unit, req.Description, req.Holder, now)
	if err != nil {
		return nil, fmt.Errorf("create story_resource: %w", err)
	}

	return &models.StoryResource{
		ID: id, ProjectID: projectID, Name: req.Name,
		Category: req.Category, Quantity: req.Quantity,
		Unit: req.Unit, Description: req.Description, Holder: req.Holder,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *ResourceLedgerService) Update(ctx context.Context, id string, name, description string) (*models.StoryResource, error) {
	var r models.StoryResource
	err := s.db.QueryRow(ctx,
		`UPDATE story_resources SET
		   name = COALESCE(NULLIF($1,''), name),
		   description = COALESCE(NULLIF($2,''), description),
		   updated_at = NOW()
		 WHERE id = $3
		 RETURNING id, project_id, name, category, quantity, unit, description, holder, created_at, updated_at`,
		name, description, id).Scan(
		&r.ID, &r.ProjectID, &r.Name, &r.Category,
		&r.Quantity, &r.Unit, &r.Description, &r.Holder, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update story_resource: %w", err)
	}
	return &r, nil
}

func (s *ResourceLedgerService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM story_resources WHERE id = $1`, id)
	return err
}

// RecordChange records a delta for a resource in a chapter and updates quantity atomically.
func (s *ResourceLedgerService) RecordChange(ctx context.Context, resourceID string, req models.RecordResourceChangeRequest) (*models.ResourceChange, error) {
	changeID := uuid.New().String()
	now := time.Now()

	// Convert empty string to nil for nullable FK
	var chapterID *string
	if req.ChapterID != "" {
		chapterID = &req.ChapterID
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`UPDATE story_resources SET quantity = quantity + $1, updated_at = $2 WHERE id = $3`,
		req.Delta, now, resourceID)
	if err != nil {
		return nil, fmt.Errorf("update resource quantity: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO story_resource_changes (id, resource_id, chapter_id, delta, reason, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		changeID, resourceID, chapterID, req.Delta, req.Reason, now)
	if err != nil {
		return nil, fmt.Errorf("insert resource_change: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &models.ResourceChange{
		ID: changeID, ResourceID: resourceID, ChapterID: chapterID,
		Delta: req.Delta, Reason: req.Reason, CreatedAt: now,
	}, nil
}

func (s *ResourceLedgerService) ListChanges(ctx context.Context, resourceID string) ([]models.ResourceChange, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, resource_id, chapter_id, delta, reason, created_at
		 FROM story_resource_changes WHERE resource_id = $1
		 ORDER BY created_at DESC`, resourceID)
	if err != nil {
		return nil, fmt.Errorf("list resource_changes: %w", err)
	}
	defer rows.Close()

	var changes []models.ResourceChange
	for rows.Next() {
		var c models.ResourceChange
		if err := rows.Scan(&c.ID, &c.ResourceID, &c.ChapterID, &c.Delta, &c.Reason, &c.CreatedAt); err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	return changes, rows.Err()
}
