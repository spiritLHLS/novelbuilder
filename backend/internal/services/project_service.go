package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/database"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ============================================================
// Project Service
// ============================================================

type ProjectService struct {
	db     *database.DB
	orm    *gorm.DB
	logger *zap.Logger
}

func NewProjectService(db *database.DB, orm *gorm.DB, logger *zap.Logger) *ProjectService {
	return &ProjectService{db: db, orm: orm, logger: logger}
}

func (s *ProjectService) Ping(ctx context.Context) error {
	if s.orm != nil {
		sqlDB, err := s.orm.DB()
		if err != nil {
			return err
		}
		return sqlDB.PingContext(ctx)
	}
	return s.db.Ping(ctx)
}

// DB exposes the underlying connection pool for ad-hoc queries by handler layers.
func (s *ProjectService) DB() *database.DB {
	return s.db
}

func (s *ProjectService) List(ctx context.Context) ([]models.Project, error) {
	if s.orm != nil {
		var rows []database.ProjectSchema
		if err := s.orm.WithContext(ctx).Order("created_at DESC").Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("list projects: %w", err)
		}
		projects := make([]models.Project, 0, len(rows))
		for _, row := range rows {
			projects = append(projects, projectSchemaToModel(row))
		}
		return projects, nil
	}

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
	id := uuid.New().String()
	if s.orm != nil {
		now := time.Now()
		row := database.ProjectSchema{
			ID:                       id,
			Title:                    req.Title,
			Genre:                    req.Genre,
			Description:              req.Description,
			StyleDescription:         req.StyleDescription,
			TargetWords:              req.TargetWords,
			ChapterWords:             req.ChapterWords,
			Status:                   "draft",
			ProjectType:              req.ProjectType,
			ContinuationRefID:        req.ContinuationRefID,
			ContinuationStartChapter: req.ContinuationStartChapter,
			CreatedAt:                &now,
			UpdatedAt:                &now,
		}
		if err := s.orm.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, fmt.Errorf("create project: %w", err)
		}
		project := projectSchemaToModel(row)
		return &project, nil
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
	if s.orm != nil {
		var row database.ProjectSchema
		err := s.orm.WithContext(ctx).First(&row, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get project: %w", err)
		}
		project := projectSchemaToModel(row)
		return &project, nil
	}

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
	if errors.Is(err, database.ErrNoRows) {
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
	if s.orm != nil {
		updates := map[string]interface{}{
			"title":             req.Title,
			"genre":             req.Genre,
			"description":       req.Description,
			"style_description": req.StyleDescription,
			"target_words":      req.TargetWords,
			"chapter_words":     req.ChapterWords,
			"updated_at":        time.Now(),
		}
		tx := s.orm.WithContext(ctx).Model(&database.ProjectSchema{}).Where("id = ?", id).Updates(updates)
		if tx.Error != nil {
			return nil, fmt.Errorf("update project: %w", tx.Error)
		}
		if tx.RowsAffected == 0 {
			return nil, fmt.Errorf("update project: not found")
		}
		return s.Get(ctx, id)
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
	if s.orm != nil {
		return s.orm.WithContext(ctx).Delete(&database.ProjectSchema{}, "id = ?", id).Error
	}
	_, err := s.db.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return err
}

func (s *ProjectService) UpdateFanfic(ctx context.Context, id string, fanficMode *string, sourceText string) error {
	if s.orm != nil {
		return s.orm.WithContext(ctx).Model(&database.ProjectSchema{}).Where("id = ?", id).Updates(map[string]interface{}{
			"fanfic_mode":        fanficMode,
			"fanfic_source_text": sourceText,
			"updated_at":         time.Now(),
		}).Error
	}
	_, err := s.db.Exec(ctx,
		`UPDATE projects SET fanfic_mode = $2, fanfic_source_text = $3, updated_at = NOW() WHERE id = $1`,
		id, fanficMode, sourceText)
	return err
}

func (s *ProjectService) SetAutoWrite(ctx context.Context, id string, enabled bool, intervalMinutes int) error {
	if s.orm != nil {
		return s.orm.WithContext(ctx).Model(&database.ProjectSchema{}).Where("id = ?", id).Updates(map[string]interface{}{
			"auto_write_enabled":  enabled,
			"auto_write_interval": intervalMinutes,
			"updated_at":          time.Now(),
		}).Error
	}
	_, err := s.db.Exec(ctx,
		`UPDATE projects SET auto_write_enabled = $2, auto_write_interval = $3, updated_at = NOW() WHERE id = $1`,
		id, enabled, intervalMinutes)
	return err
}

// SetContinuationMode updates a project to continuation mode with the given reference ID and start chapter.
func (s *ProjectService) SetContinuationMode(ctx context.Context, id string, refID string, startChapter int) error {
	if startChapter <= 0 {
		var lastReferenceChapter int
		if s.orm != nil {
			_ = s.orm.WithContext(ctx).
				Model(&database.ReferenceBookChapterSchema{}).
				Where("ref_id = ? AND is_deleted = ?", refID, false).
				Select("COALESCE(MAX(chapter_no), 0)").
				Scan(&lastReferenceChapter).Error
		} else {
			_ = s.db.QueryRow(ctx,
				`SELECT COALESCE(MAX(chapter_no), 0)
				 FROM reference_book_chapters
				 WHERE ref_id = $1 AND is_deleted = FALSE`,
				refID).Scan(&lastReferenceChapter)
		}
		startChapter = lastReferenceChapter + 1
		if startChapter <= 1 {
			startChapter = 1
		}
	}
	if s.orm != nil {
		err := s.orm.WithContext(ctx).Model(&database.ProjectSchema{}).Where("id = ?", id).Updates(map[string]interface{}{
			"project_type":               "continuation",
			"continuation_ref_id":        refID,
			"continuation_start_chapter": startChapter,
			"updated_at":                 time.Now(),
		}).Error
		if err != nil {
			return fmt.Errorf("set continuation mode: %w", err)
		}
		s.logger.Info("project set to continuation mode",
			zap.String("project_id", id),
			zap.String("ref_id", refID),
			zap.Int("start_chapter", startChapter))
		return nil
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

func projectSchemaToModel(row database.ProjectSchema) models.Project {
	createdAt := time.Time{}
	if row.CreatedAt != nil {
		createdAt = *row.CreatedAt
	}
	updatedAt := time.Time{}
	if row.UpdatedAt != nil {
		updatedAt = *row.UpdatedAt
	}
	projectType := row.ProjectType
	if projectType == "" {
		projectType = "original"
	}
	startChapter := row.ContinuationStartChapter
	if startChapter <= 0 {
		startChapter = 1
	}
	return models.Project{
		ID:                       row.ID,
		Title:                    row.Title,
		Genre:                    row.Genre,
		Description:              row.Description,
		StyleDescription:         row.StyleDescription,
		TargetWords:              row.TargetWords,
		ChapterWords:             row.ChapterWords,
		Status:                   row.Status,
		ProjectType:              projectType,
		ContinuationRefID:        row.ContinuationRefID,
		ContinuationStartChapter: startChapter,
		CreatedAt:                createdAt,
		UpdatedAt:                updatedAt,
	}
}
