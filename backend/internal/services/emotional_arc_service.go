package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// EmotionalArcService tracks per-character emotion states across chapters.
type EmotionalArcService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewEmotionalArcService(db *pgxpool.Pool, logger *zap.Logger) *EmotionalArcService {
	return &EmotionalArcService{db: db, logger: logger}
}

type EmotionalArcEntry struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	CharacterID string    `json:"character_id"`
	ChapterID   *string   `json:"chapter_id"`
	ChapterNum  int       `json:"chapter_num"`
	Emotion     string    `json:"emotion"`
	Intensity   float64   `json:"intensity"`
	Note        string    `json:"note"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Populated on list
	CharacterName string `json:"character_name,omitempty"`
}

type UpsertEmotionalArcRequest struct {
	CharacterID string  `json:"character_id" binding:"required"`
	ChapterID   *string `json:"chapter_id"`
	ChapterNum  int     `json:"chapter_num" binding:"required"`
	Emotion     string  `json:"emotion" binding:"required"`
	Intensity   float64 `json:"intensity"`
	Note        string  `json:"note"`
}

// ListForProject returns all emotional arc entries ordered by character, then chapter.
func (s *EmotionalArcService) ListForProject(ctx context.Context, projectID string) ([]EmotionalArcEntry, error) {
	rows, err := s.db.Query(ctx,
		`SELECT e.id, e.project_id, e.character_id, e.chapter_id, e.chapter_num,
		        e.emotion, e.intensity, e.note, e.created_at, e.updated_at,
		        COALESCE(c.name,'')
		 FROM emotional_arc_entries e
		 LEFT JOIN characters c ON c.id = e.character_id
		 WHERE e.project_id = $1
		 ORDER BY c.name, e.chapter_num`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list emotional arcs: %w", err)
	}
	defer rows.Close()
	var out []EmotionalArcEntry
	for rows.Next() {
		var e EmotionalArcEntry
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.CharacterID, &e.ChapterID,
			&e.ChapterNum, &e.Emotion, &e.Intensity, &e.Note,
			&e.CreatedAt, &e.UpdatedAt, &e.CharacterName); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListForCharacter returns arc entries for a single character.
func (s *EmotionalArcService) ListForCharacter(ctx context.Context, characterID string) ([]EmotionalArcEntry, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, character_id, chapter_id, chapter_num,
		        emotion, intensity, note, created_at, updated_at
		 FROM emotional_arc_entries
		 WHERE character_id = $1
		 ORDER BY chapter_num`, characterID)
	if err != nil {
		return nil, fmt.Errorf("list arc for character: %w", err)
	}
	defer rows.Close()
	var out []EmotionalArcEntry
	for rows.Next() {
		var e EmotionalArcEntry
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.CharacterID, &e.ChapterID,
			&e.ChapterNum, &e.Emotion, &e.Intensity, &e.Note,
			&e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Upsert creates or updates an emotional arc entry for a given character+chapter.
func (s *EmotionalArcService) Upsert(ctx context.Context, projectID string, req UpsertEmotionalArcRequest) (*EmotionalArcEntry, error) {
	id := uuid.New().String()
	intensity := req.Intensity
	if intensity < 0 {
		intensity = 0
	}
	var e EmotionalArcEntry
	err := s.db.QueryRow(ctx,
		`INSERT INTO emotional_arc_entries
		    (id, project_id, character_id, chapter_id, chapter_num, emotion, intensity, note)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (character_id, chapter_num)
		   DO UPDATE SET emotion=$6, intensity=$7, note=$8, chapter_id=$4, updated_at=NOW()
		 RETURNING id, project_id, character_id, chapter_id, chapter_num,
		           emotion, intensity, note, created_at, updated_at`,
		id, projectID, req.CharacterID, req.ChapterID, req.ChapterNum,
		req.Emotion, intensity, req.Note).
		Scan(&e.ID, &e.ProjectID, &e.CharacterID, &e.ChapterID, &e.ChapterNum,
			&e.Emotion, &e.Intensity, &e.Note, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert emotional arc: %w", err)
	}
	return &e, nil
}

func (s *EmotionalArcService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM emotional_arc_entries WHERE id = $1`, id)
	return err
}
