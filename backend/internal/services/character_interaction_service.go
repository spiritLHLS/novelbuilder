package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// CharacterInteractionService tracks which characters have met and what they know.
type CharacterInteractionService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewCharacterInteractionService(db *pgxpool.Pool, logger *zap.Logger) *CharacterInteractionService {
	return &CharacterInteractionService{db: db, logger: logger}
}

type CharacterInteraction struct {
	ID                  string          `json:"id"`
	ProjectID           string          `json:"project_id"`
	CharAID             string          `json:"char_a_id"`
	CharBID             string          `json:"char_b_id"`
	CharAName           string          `json:"char_a_name,omitempty"`
	CharBName           string          `json:"char_b_name,omitempty"`
	FirstMeetChapter    *int            `json:"first_meet_chapter"`
	LastInteractChapter *int            `json:"last_interact_chapter"`
	Relationship        string          `json:"relationship"`
	InfoKnownByA        json.RawMessage `json:"info_known_by_a"`
	InfoKnownByB        json.RawMessage `json:"info_known_by_b"`
	InteractionCount    int             `json:"interaction_count"`
	Notes               string          `json:"notes"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type UpsertInteractionRequest struct {
	CharAID             string          `json:"char_a_id" binding:"required"`
	CharBID             string          `json:"char_b_id" binding:"required"`
	FirstMeetChapter    *int            `json:"first_meet_chapter"`
	LastInteractChapter *int            `json:"last_interact_chapter"`
	Relationship        string          `json:"relationship"`
	InfoKnownByA        json.RawMessage `json:"info_known_by_a"`
	InfoKnownByB        json.RawMessage `json:"info_known_by_b"`
	Notes               string          `json:"notes"`
	BumpCount           bool            `json:"bump_count"` // increment interaction_count
}

// canonical ordering: char_a_id < char_b_id
func canonical(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

func (s *CharacterInteractionService) List(ctx context.Context, projectID string) ([]CharacterInteraction, error) {
	rows, err := s.db.Query(ctx,
		`SELECT ci.id, ci.project_id, ci.char_a_id, ci.char_b_id,
		        COALESCE(ca.name,''), COALESCE(cb.name,''),
		        ci.first_meet_chapter, ci.last_interact_chapter, ci.relationship,
		        ci.info_known_by_a, ci.info_known_by_b, ci.interaction_count,
		        ci.notes, ci.created_at, ci.updated_at
		 FROM character_interactions ci
		 LEFT JOIN characters ca ON ca.id = ci.char_a_id
		 LEFT JOIN characters cb ON cb.id = ci.char_b_id
		 WHERE ci.project_id = $1
		 ORDER BY ca.name, cb.name`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list character interactions: %w", err)
	}
	defer rows.Close()
	var out []CharacterInteraction
	for rows.Next() {
		var ci CharacterInteraction
		if err := rows.Scan(
			&ci.ID, &ci.ProjectID, &ci.CharAID, &ci.CharBID,
			&ci.CharAName, &ci.CharBName,
			&ci.FirstMeetChapter, &ci.LastInteractChapter, &ci.Relationship,
			&ci.InfoKnownByA, &ci.InfoKnownByB, &ci.InteractionCount,
			&ci.Notes, &ci.CreatedAt, &ci.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ci)
	}
	return out, rows.Err()
}

func (s *CharacterInteractionService) Upsert(ctx context.Context, projectID string, req UpsertInteractionRequest) (*CharacterInteraction, error) {
	charA, charB := canonical(req.CharAID, req.CharBID)
	// Reflect info fields if IDs were swapped
	infoA, infoB := req.InfoKnownByA, req.InfoKnownByB
	if req.CharAID != charA {
		infoA, infoB = req.InfoKnownByB, req.InfoKnownByA
	}
	if infoA == nil {
		infoA = json.RawMessage(`[]`)
	}
	if infoB == nil {
		infoB = json.RawMessage(`[]`)
	}
	rel := req.Relationship
	if rel == "" {
		rel = "acquaintance"
	}

	var ci CharacterInteraction
	err := s.db.QueryRow(ctx,
		`INSERT INTO character_interactions
		    (project_id, char_a_id, char_b_id, first_meet_chapter, last_interact_chapter,
		     relationship, info_known_by_a, info_known_by_b, notes)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT (project_id, char_a_id, char_b_id) DO UPDATE SET
		   first_meet_chapter    = COALESCE($4, character_interactions.first_meet_chapter),
		   last_interact_chapter = COALESCE($5, character_interactions.last_interact_chapter),
		   relationship          = CASE WHEN $6 <> '' THEN $6 ELSE character_interactions.relationship END,
		   info_known_by_a       = CASE WHEN $7::text <> '[]' THEN $7 ELSE character_interactions.info_known_by_a END,
		   info_known_by_b       = CASE WHEN $8::text <> '[]' THEN $8 ELSE character_interactions.info_known_by_b END,
		   notes                 = CASE WHEN $9 <> '' THEN $9 ELSE character_interactions.notes END,
		   interaction_count     = CASE WHEN $10 THEN character_interactions.interaction_count + 1
		                               ELSE character_interactions.interaction_count END,
		   updated_at            = NOW()
		 RETURNING id, project_id, char_a_id, char_b_id,
		           first_meet_chapter, last_interact_chapter, relationship,
		           info_known_by_a, info_known_by_b, interaction_count,
		           notes, created_at, updated_at`,
		projectID, charA, charB, req.FirstMeetChapter, req.LastInteractChapter,
		rel, infoA, infoB, req.Notes, req.BumpCount).
		Scan(&ci.ID, &ci.ProjectID, &ci.CharAID, &ci.CharBID,
			&ci.FirstMeetChapter, &ci.LastInteractChapter, &ci.Relationship,
			&ci.InfoKnownByA, &ci.InfoKnownByB, &ci.InteractionCount,
			&ci.Notes, &ci.CreatedAt, &ci.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert character interaction: %w", err)
	}
	return &ci, nil
}

func (s *CharacterInteractionService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM character_interactions WHERE id = $1`, id)
	return err
}
