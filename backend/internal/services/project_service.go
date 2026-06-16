package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func nullableString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func normalizeCreationMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "scratch", "own_outline", "prompt_only", "reference_style", "rewrite_original", "continuation", "same_style_new_world":
		return mode
	default:
		return "prompt_only"
	}
}

func normalizeProjectLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "en", "en-us", "en_us", "english":
		return "en-US"
	case "ja", "ja-jp", "ja_jp", "japanese", "日本語", "日语":
		return "ja-JP"
	case "zh", "zh-cn", "zh_cn", "chinese", "中文", "简体中文", "":
		return "zh-CN"
	default:
		return "zh-CN"
	}
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
	return s.ListForUser(ctx, "", true)
}

func (s *ProjectService) ListForUser(ctx context.Context, userID string, includeAll bool) ([]models.Project, error) {
	if s.orm != nil {
		var rows []database.ProjectSchema
		query := s.orm.WithContext(ctx).Order("created_at DESC")
		if !includeAll {
			query = query.Where("owner_id = ?", userID)
		}
		if err := query.Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("list projects: %w", err)
		}
		projects := make([]models.Project, 0, len(rows))
		for _, row := range rows {
			projects = append(projects, projectSchemaToModel(row))
		}
		return projects, nil
	}

	where := ""
	args := []interface{}{}
	if !includeAll {
		where = "WHERE owner_id = $1"
		args = append(args, userID)
	}
	rows, err := s.db.Query(ctx,
		`SELECT id, COALESCE(owner_id::text, ''), title, genre, description, style_description, COALESCE(language, 'zh-CN'), target_words, chapter_words, status,
		        COALESCE(creation_mode, 'prompt_only'), COALESCE(project_type, 'original'), continuation_ref_id, COALESCE(continuation_start_chapter, 1),
		        created_at, updated_at
		 FROM projects `+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(
			&p.ID, &p.OwnerID, &p.Title, &p.Genre, &p.Description, &p.StyleDescription,
			&p.Language,
			&p.TargetWords, &p.ChapterWords, &p.Status,
			&p.CreationMode, &p.ProjectType, &p.ContinuationRefID, &p.ContinuationStartChapter,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		p.Language = normalizeProjectLanguage(p.Language)
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list projects rows: %w", err)
	}
	return projects, nil
}

func (s *ProjectService) Create(ctx context.Context, req models.CreateProjectRequest) (*models.Project, error) {
	return s.CreateForOwner(ctx, req, "")
}

func (s *ProjectService) CreateForOwner(ctx context.Context, req models.CreateProjectRequest, ownerID string) (*models.Project, error) {
	if req.TargetWords <= 0 {
		req.TargetWords = 500000
	}
	if req.ChapterWords <= 0 {
		req.ChapterWords = 3000
	}
	if req.ProjectType == "" {
		req.ProjectType = "original"
	}
	req.CreationMode = normalizeCreationMode(req.CreationMode)
	if req.CreationMode == "continuation" {
		req.ProjectType = "continuation"
	}
	if req.Language == "" {
		req.Language = "zh-CN"
	}
	req.Language = normalizeProjectLanguage(req.Language)
	if req.ProjectType == "continuation" && req.ContinuationStartChapter <= 0 {
		req.ContinuationStartChapter = 1
	}
	id := uuid.New().String()
	if s.orm != nil {
		now := time.Now()
		row := database.ProjectSchema{
			ID:                       id,
			OwnerID:                  nullableString(ownerID),
			Title:                    req.Title,
			Genre:                    req.Genre,
			Description:              req.Description,
			StyleDescription:         req.StyleDescription,
			Language:                 req.Language,
			TargetWords:              req.TargetWords,
			ChapterWords:             req.ChapterWords,
			Status:                   "draft",
			CreationMode:             req.CreationMode,
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
		`INSERT INTO projects (id, owner_id, title, genre, description, style_description, language, target_words, chapter_words, status,
		                       creation_mode, project_type, continuation_ref_id, continuation_start_chapter, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'draft', $10, $11, $12, $13, NOW(), NOW())
		 RETURNING id, COALESCE(owner_id::text, ''), title, genre, description, style_description, COALESCE(language, 'zh-CN'), target_words, chapter_words, status,
		           COALESCE(creation_mode, 'prompt_only'), COALESCE(project_type, 'original'), continuation_ref_id, COALESCE(continuation_start_chapter, 1),
		           created_at, updated_at`,
		id, nullableString(ownerID), req.Title, req.Genre, req.Description, req.StyleDescription, req.Language, req.TargetWords, req.ChapterWords,
		req.CreationMode, req.ProjectType, req.ContinuationRefID, req.ContinuationStartChapter,
	).Scan(
		&p.ID, &p.OwnerID, &p.Title, &p.Genre, &p.Description, &p.StyleDescription,
		&p.Language,
		&p.TargetWords, &p.ChapterWords, &p.Status,
		&p.CreationMode, &p.ProjectType, &p.ContinuationRefID, &p.ContinuationStartChapter,
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
		`SELECT id, COALESCE(owner_id::text, ''), title, genre, description, style_description, COALESCE(language, 'zh-CN'), target_words, chapter_words, status,
		        COALESCE(creation_mode, 'prompt_only'), COALESCE(project_type, 'original'), continuation_ref_id, COALESCE(continuation_start_chapter, 1),
		        created_at, updated_at
		 FROM projects WHERE id = $1`, id,
	).Scan(
		&p.ID, &p.OwnerID, &p.Title, &p.Genre, &p.Description, &p.StyleDescription,
		&p.Language,
		&p.TargetWords, &p.ChapterWords, &p.Status,
		&p.CreationMode, &p.ProjectType, &p.ContinuationRefID, &p.ContinuationStartChapter,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, database.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	p.Language = normalizeProjectLanguage(p.Language)
	return &p, nil
}

func (s *ProjectService) Update(ctx context.Context, id string, req models.CreateProjectRequest) (*models.Project, error) {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("update project: not found")
	}
	if req.TargetWords <= 0 {
		req.TargetWords = existing.TargetWords
	}
	if req.ChapterWords <= 0 {
		req.ChapterWords = existing.ChapterWords
	}
	if req.Language == "" {
		req.Language = existing.Language
	}
	if req.Language == "" {
		req.Language = "zh-CN"
	}
	req.Language = normalizeProjectLanguage(req.Language)
	if req.CreationMode == "" {
		req.CreationMode = existing.CreationMode
	}
	req.CreationMode = normalizeCreationMode(req.CreationMode)
	if s.orm != nil {
		updates := map[string]interface{}{
			"title":             req.Title,
			"genre":             req.Genre,
			"description":       req.Description,
			"style_description": req.StyleDescription,
			"language":          req.Language,
			"target_words":      req.TargetWords,
			"chapter_words":     req.ChapterWords,
			"creation_mode":     req.CreationMode,
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
	err = s.db.QueryRow(ctx,
		`UPDATE projects
		 SET title = $1, genre = $2, description = $3, style_description = $4,
		     language = $5, target_words = $6, chapter_words = $7, creation_mode = $8, updated_at = NOW()
		 WHERE id = $9
		 RETURNING id, COALESCE(owner_id::text, ''), title, genre, description, style_description, COALESCE(language, 'zh-CN'), target_words, chapter_words, status,
		           COALESCE(creation_mode, 'prompt_only'), COALESCE(project_type, 'original'), continuation_ref_id, COALESCE(continuation_start_chapter, 1),
		           created_at, updated_at`,
		req.Title, req.Genre, req.Description, req.StyleDescription,
		req.Language, req.TargetWords, req.ChapterWords, req.CreationMode, id,
	).Scan(
		&p.ID, &p.OwnerID, &p.Title, &p.Genre, &p.Description, &p.StyleDescription,
		&p.Language,
		&p.TargetWords, &p.ChapterWords, &p.Status,
		&p.CreationMode, &p.ProjectType, &p.ContinuationRefID, &p.ContinuationStartChapter,
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

func (s *ProjectService) SetStatus(ctx context.Context, id string, status string) error {
	if s.orm != nil {
		tx := s.orm.WithContext(ctx).Model(&database.ProjectSchema{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now(),
		})
		if tx.Error != nil {
			return fmt.Errorf("set project status: %w", tx.Error)
		}
		if tx.RowsAffected == 0 {
			return fmt.Errorf("project not found")
		}
		return nil
	}
	tag, err := s.db.Exec(ctx, `UPDATE projects SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("set project status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("project not found")
	}
	return nil
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
			"creation_mode":              "continuation",
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
		 SET creation_mode = 'continuation', project_type = 'continuation', continuation_ref_id = $2, continuation_start_chapter = $3, updated_at = NOW()
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
	creationMode := row.CreationMode
	if creationMode == "" {
		creationMode = "prompt_only"
	}
	startChapter := row.ContinuationStartChapter
	if startChapter <= 0 {
		startChapter = 1
	}
	language := normalizeProjectLanguage(row.Language)
	return models.Project{
		ID:                       row.ID,
		OwnerID:                  stringFromPtr(row.OwnerID),
		Title:                    row.Title,
		Genre:                    row.Genre,
		Description:              row.Description,
		StyleDescription:         row.StyleDescription,
		Language:                 language,
		TargetWords:              row.TargetWords,
		ChapterWords:             row.ChapterWords,
		Status:                   row.Status,
		CreationMode:             creationMode,
		ProjectType:              projectType,
		ContinuationRefID:        row.ContinuationRefID,
		ContinuationStartChapter: startChapter,
		CreatedAt:                createdAt,
		UpdatedAt:                updatedAt,
	}
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
