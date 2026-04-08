package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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
		`INSERT INTO world_bibles (id, project_id, content, version, created_at, updated_at)
		 VALUES ($1, $2, $3, 1, NOW(), NOW())
		 ON CONFLICT (project_id) DO UPDATE
		     SET content = EXCLUDED.content,
		         version = world_bibles.version + 1,
		         updated_at = NOW()
		 RETURNING id, project_id, content, migration_source, version, created_at, updated_at`,
		uuid.New().String(), projectID, content).Scan(&wb.ID, &wb.ProjectID, &wb.Content, &wb.MigrationSource,
		&wb.Version, &wb.CreatedAt, &wb.UpdatedAt)
	return &wb, err
}

// WorldBibleBundle is the JSON structure used for import/export.
type WorldBibleBundle struct {
	Version      string              `json:"version"`
	Type         string              `json:"type"`
	ExportedAt   time.Time           `json:"exported_at"`
	WorldBible   json.RawMessage     `json:"world_bible,omitempty"`
	Constitution *ConstitutionBundle `json:"constitution,omitempty"`
}

type ConstitutionBundle struct {
	ImmutableRules   json.RawMessage `json:"immutable_rules"`
	MutableRules     json.RawMessage `json:"mutable_rules"`
	ForbiddenAnchors json.RawMessage `json:"forbidden_anchors"`
}

// Export returns a JSON bundle containing the world bible and constitution for the project.
func (s *WorldBibleService) Export(ctx context.Context, projectID string) (*WorldBibleBundle, error) {
	wb, err := s.Get(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("export world bible: %w", err)
	}
	wbc, _ := s.GetConstitution(ctx, projectID)

	bundle := &WorldBibleBundle{
		Version:    "1.0",
		Type:       "world_bible_bundle",
		ExportedAt: time.Now(),
	}
	if wb != nil {
		bundle.WorldBible = wb.Content
	}
	if wbc != nil {
		bundle.Constitution = &ConstitutionBundle{
			ImmutableRules:   wbc.ImmutableRules,
			MutableRules:     wbc.MutableRules,
			ForbiddenAnchors: wbc.ForbiddenAnchors,
		}
	}
	return bundle, nil
}

// Import overwrites the project's world bible and constitution with the given bundle.
// World bible content: fully replaced by the imported data.
// Constitution: fully replaced if present in the bundle.
func (s *WorldBibleService) Import(ctx context.Context, projectID string, bundle *WorldBibleBundle) error {
	if bundle.Type != "world_bible_bundle" {
		return fmt.Errorf("invalid bundle type: %q (expected world_bible_bundle)", bundle.Type)
	}

	// Overwrite world bible with the imported content directly.
	if len(bundle.WorldBible) > 4 {
		// Validate the imported JSON before writing.
		var check map[string]interface{}
		if err := json.Unmarshal(bundle.WorldBible, &check); err != nil {
			return fmt.Errorf("import: invalid world_bible JSON")
		}
		if _, err := s.Update(ctx, projectID, bundle.WorldBible); err != nil {
			return fmt.Errorf("import world bible update: %w", err)
		}
	}

	// Import constitution — fully overwrite if provided.
	if bundle.Constitution != nil {
		immutable := bundle.Constitution.ImmutableRules
		if len(immutable) == 0 {
			immutable = json.RawMessage(`[]`)
		}
		mutable := bundle.Constitution.MutableRules
		if len(mutable) == 0 {
			mutable = json.RawMessage(`[]`)
		}
		forbidden := bundle.Constitution.ForbiddenAnchors
		if len(forbidden) == 0 {
			forbidden = json.RawMessage(`[]`)
		}
		if _, err := s.UpdateConstitution(ctx, projectID, immutable, mutable, forbidden); err != nil {
			return fmt.Errorf("import constitution: %w", err)
		}
	}
	return nil
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

// ListChapterOutlines returns chapter-level outlines, optionally filtered by volume number.
func (s *OutlineService) ListChapterOutlines(ctx context.Context, projectID string, volumeNum *int) ([]models.Outline, error) {
	var rows pgx.Rows
	var err error

	if volumeNum != nil {
		// Get volume range first
		var chapterStart, chapterEnd int
		err := s.db.QueryRow(ctx,
			`SELECT chapter_start, chapter_end FROM volumes WHERE project_id = $1 AND volume_num = $2`,
			projectID, *volumeNum).Scan(&chapterStart, &chapterEnd)
		if err != nil {
			return nil, fmt.Errorf("get volume range: %w", err)
		}

		rows, err = s.db.Query(ctx,
			`SELECT id, project_id, level, parent_id, order_num, title, content, tension_target, created_at, updated_at
			 FROM outlines 
			 WHERE project_id = $1 AND level = 'chapter' AND order_num >= $2 AND order_num <= $3
			 ORDER BY order_num`,
			projectID, chapterStart, chapterEnd)
	} else {
		rows, err = s.db.Query(ctx,
			`SELECT id, project_id, level, parent_id, order_num, title, content, tension_target, created_at, updated_at
			 FROM outlines 
			 WHERE project_id = $1 AND level = 'chapter'
			 ORDER BY order_num`,
			projectID)
	}

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
		return nil, fmt.Errorf("list chapter outlines rows: %w", err)
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
		        COALESCE(embed_method, ''), COALESCE(resolve_method, ''),
		        COALESCE(planned_embed_chapter, 0), COALESCE(planned_resolve_chapter, 0),
		        priority, status, COALESCE(tags, '{}'),
		        COALESCE(origin, 'manual'), COALESCE(cross_volume, false),
		        created_at, updated_at
		 FROM foreshadowings WHERE project_id = $1 ORDER BY priority DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.Foreshadowing
	for rows.Next() {
		var f models.Foreshadowing
		if err := rows.Scan(&f.ID, &f.ProjectID, &f.Content, &f.EmbedChapterID, &f.ResolveChapterID,
			&f.EmbedMethod, &f.ResolveMethod, &f.PlannedEmbedChapter, &f.PlannedResolveChapter,
			&f.Priority, &f.Status, &f.Tags,
			&f.Origin, &f.CrossVolume,
			&f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list foreshadowings rows: %w", err)
	}
	return list, nil
}

func (s *ForeshadowingService) Create(ctx context.Context, projectID, content, embedMethod string, priority, plannedEmbedChapter, plannedResolveChapter int) (*models.Foreshadowing, error) {
	var f models.Foreshadowing
	err := s.db.QueryRow(ctx,
		`INSERT INTO foreshadowings (project_id, content, embed_method, priority, planned_embed_chapter, planned_resolve_chapter) VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, project_id, content, embed_chapter_id, resolve_chapter_id,
		           COALESCE(embed_method, ''), COALESCE(resolve_method, ''),
		           COALESCE(planned_embed_chapter, 0), COALESCE(planned_resolve_chapter, 0),
		           priority, status, COALESCE(tags, '{}'),
		           COALESCE(origin, 'manual'), COALESCE(cross_volume, false),
		           created_at, updated_at`,
		projectID, content, embedMethod, priority, plannedEmbedChapter, plannedResolveChapter).Scan(
		&f.ID, &f.ProjectID, &f.Content, &f.EmbedChapterID, &f.ResolveChapterID,
		&f.EmbedMethod, &f.ResolveMethod, &f.PlannedEmbedChapter, &f.PlannedResolveChapter,
		&f.Priority, &f.Status, &f.Tags,
		&f.Origin, &f.CrossVolume,
		&f.CreatedAt, &f.UpdatedAt)
	return &f, err
}

func (s *ForeshadowingService) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := s.db.Exec(ctx, `UPDATE foreshadowings SET status = $1 WHERE id = $2`, status, id)
	return err
}

func (s *ForeshadowingService) Update(ctx context.Context, id, content, embedMethod string, tags []string, priority, plannedEmbedChapter, plannedResolveChapter int) (*models.Foreshadowing, error) {
	var f models.Foreshadowing
	err := s.db.QueryRow(ctx,
		`UPDATE foreshadowings SET content = $1, embed_method = $2, tags = $3, priority = $4, planned_embed_chapter = $5, planned_resolve_chapter = $6
		 WHERE id = $7
		 RETURNING id, project_id, content, embed_chapter_id, resolve_chapter_id,
		           COALESCE(embed_method, ''), COALESCE(resolve_method, ''),
		           COALESCE(planned_embed_chapter, 0), COALESCE(planned_resolve_chapter, 0),
		           priority, status, COALESCE(tags, '{}'),
		           COALESCE(origin, 'manual'), COALESCE(cross_volume, false),
		           created_at, updated_at`,
		content, embedMethod, tags, priority, plannedEmbedChapter, plannedResolveChapter, id).Scan(
		&f.ID, &f.ProjectID, &f.Content, &f.EmbedChapterID, &f.ResolveChapterID,
		&f.EmbedMethod, &f.ResolveMethod, &f.PlannedEmbedChapter, &f.PlannedResolveChapter,
		&f.Priority, &f.Status, &f.Tags,
		&f.Origin, &f.CrossVolume,
		&f.CreatedAt, &f.UpdatedAt)
	return &f, err
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

func (s *VolumeService) Get(ctx context.Context, id string) (*models.Volume, error) {
	var v models.Volume
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, volume_num, COALESCE(title, ''), blueprint_id, status,
		        COALESCE(chapter_start, 0), COALESCE(chapter_end, 0), COALESCE(review_comment, ''), created_at, updated_at
		 FROM volumes WHERE id = $1`, id,
	).Scan(&v.ID, &v.ProjectID, &v.VolumeNum, &v.Title, &v.BlueprintID,
		&v.Status, &v.ChapterStart, &v.ChapterEnd, &v.ReviewComment, &v.CreatedAt, &v.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &v, err
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
