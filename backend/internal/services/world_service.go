package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
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
