package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// ============================================================
// Project Service
// ============================================================

type ProjectService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewProjectService(db *pgxpool.Pool, logger *zap.Logger) *ProjectService {
	return &ProjectService{db: db, logger: logger}
}

func (s *ProjectService) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

// DB exposes the underlying connection pool for ad-hoc queries by handler layers.
func (s *ProjectService) DB() *pgxpool.Pool {
	return s.db
}

func (s *ProjectService) List(ctx context.Context) ([]models.Project, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, title, genre, description, style_description, target_words, chapter_words, status,
		        COALESCE(project_type, 'original'), continuation_ref_id, COALESCE(continuation_start_chapter, 1),
		        created_at, updated_at
		 FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(
			&p.ID, &p.Title, &p.Genre, &p.Description, &p.StyleDescription,
			&p.TargetWords, &p.ChapterWords, &p.Status,
			&p.ProjectType, &p.ContinuationRefID, &p.ContinuationStartChapter,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list projects rows: %w", err)
	}
	return projects, nil
}

func (s *ProjectService) Create(ctx context.Context, req models.CreateProjectRequest) (*models.Project, error) {
	id := uuid.New().String()
	if req.TargetWords <= 0 {
		req.TargetWords = 500000
	}
	if req.ChapterWords <= 0 {
		req.ChapterWords = 3000
	}
	if req.ProjectType == "" {
		req.ProjectType = "original"
	}
	if req.ProjectType == "continuation" && req.ContinuationStartChapter <= 0 {
		req.ContinuationStartChapter = 1
	}
	var p models.Project
	err := s.db.QueryRow(ctx,
		`INSERT INTO projects (id, title, genre, description, style_description, target_words, chapter_words, status,
		                       project_type, continuation_ref_id, continuation_start_chapter, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'draft', $8, $9, $10, NOW(), NOW())
		 RETURNING id, title, genre, description, style_description, target_words, chapter_words, status,
		           COALESCE(project_type, 'original'), continuation_ref_id, COALESCE(continuation_start_chapter, 1),
		           created_at, updated_at`,
		id, req.Title, req.Genre, req.Description, req.StyleDescription, req.TargetWords, req.ChapterWords,
		req.ProjectType, req.ContinuationRefID, req.ContinuationStartChapter,
	).Scan(
		&p.ID, &p.Title, &p.Genre, &p.Description, &p.StyleDescription,
		&p.TargetWords, &p.ChapterWords, &p.Status,
		&p.ProjectType, &p.ContinuationRefID, &p.ContinuationStartChapter,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return &p, nil
}

func (s *ProjectService) Get(ctx context.Context, id string) (*models.Project, error) {
	var p models.Project
	err := s.db.QueryRow(ctx,
		`SELECT id, title, genre, description, style_description, target_words, chapter_words, status,
		        COALESCE(project_type, 'original'), continuation_ref_id, COALESCE(continuation_start_chapter, 1),
		        created_at, updated_at
		 FROM projects WHERE id = $1`, id,
	).Scan(
		&p.ID, &p.Title, &p.Genre, &p.Description, &p.StyleDescription,
		&p.TargetWords, &p.ChapterWords, &p.Status,
		&p.ProjectType, &p.ContinuationRefID, &p.ContinuationStartChapter,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return &p, nil
}

func (s *ProjectService) Update(ctx context.Context, id string, req models.CreateProjectRequest) (*models.Project, error) {
	if req.TargetWords <= 0 {
		req.TargetWords = 500000
	}
	if req.ChapterWords <= 0 {
		req.ChapterWords = 3000
	}
	var p models.Project
	err := s.db.QueryRow(ctx,
		`UPDATE projects
		 SET title = $1, genre = $2, description = $3, style_description = $4,
		     target_words = $5, chapter_words = $6, updated_at = NOW()
		 WHERE id = $7
		 RETURNING id, title, genre, description, style_description, target_words, chapter_words, status,
		           COALESCE(project_type, 'original'), continuation_ref_id, COALESCE(continuation_start_chapter, 1),
		           created_at, updated_at`,
		req.Title, req.Genre, req.Description, req.StyleDescription,
		req.TargetWords, req.ChapterWords, id,
	).Scan(
		&p.ID, &p.Title, &p.Genre, &p.Description, &p.StyleDescription,
		&p.TargetWords, &p.ChapterWords, &p.Status,
		&p.ProjectType, &p.ContinuationRefID, &p.ContinuationStartChapter,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return &p, nil
}

func (s *ProjectService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return err
}

func (s *ProjectService) UpdateFanfic(ctx context.Context, id string, fanficMode *string, sourceText string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE projects SET fanfic_mode = $2, fanfic_source_text = $3, updated_at = NOW() WHERE id = $1`,
		id, fanficMode, sourceText)
	return err
}

func (s *ProjectService) SetAutoWrite(ctx context.Context, id string, enabled bool, intervalMinutes int) error {
	_, err := s.db.Exec(ctx,
		`UPDATE projects SET auto_write_enabled = $2, auto_write_interval = $3, updated_at = NOW() WHERE id = $1`,
		id, enabled, intervalMinutes)
	return err
}

// SetContinuationMode updates a project to continuation mode with the given reference ID and start chapter.
func (s *ProjectService) SetContinuationMode(ctx context.Context, id string, refID string, startChapter int) error {
	if startChapter <= 0 {
		startChapter = 1
	}
	_, err := s.db.Exec(ctx,
		`UPDATE projects
		 SET project_type = 'continuation', continuation_ref_id = $2, continuation_start_chapter = $3, updated_at = NOW()
		 WHERE id = $1`,
		id, refID, startChapter)
	if err != nil {
		return fmt.Errorf("set continuation mode: %w", err)
	}
	s.logger.Info("project set to continuation mode",
		zap.String("project_id", id),
		zap.String("ref_id", refID),
		zap.Int("start_chapter", startChapter))
	return nil
}
