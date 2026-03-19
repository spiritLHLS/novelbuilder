package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// SubplotService manages subplot (A/B/C plot-line) tracking.
type SubplotService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewSubplotService(db *pgxpool.Pool, logger *zap.Logger) *SubplotService {
	return &SubplotService{db: db, logger: logger}
}

type Subplot struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Title          string    `json:"title"`
	LineLabel      string    `json:"line_label"`
	Description    string    `json:"description"`
	Status         string    `json:"status"`
	Priority       int       `json:"priority"`
	StartChapter   *int      `json:"start_chapter"`
	ResolveChapter *int      `json:"resolve_chapter"`
	Tags           []string  `json:"tags"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SubplotCheckpoint struct {
	ID         string    `json:"id"`
	SubplotID  string    `json:"subplot_id"`
	ChapterID  *string   `json:"chapter_id"`
	ChapterNum *int      `json:"chapter_num"`
	Note       string    `json:"note"`
	Progress   int       `json:"progress"`
	CreatedAt  time.Time `json:"created_at"`
}

type CreateSubplotRequest struct {
	Title        string   `json:"title" binding:"required"`
	LineLabel    string   `json:"line_label"`
	Description  string   `json:"description"`
	Priority     int      `json:"priority"`
	StartChapter *int     `json:"start_chapter"`
	Tags         []string `json:"tags"`
}

type UpdateSubplotRequest struct {
	Title          string   `json:"title"`
	LineLabel      string   `json:"line_label"`
	Description    string   `json:"description"`
	Status         string   `json:"status"`
	Priority       int      `json:"priority"`
	ResolveChapter *int     `json:"resolve_chapter"`
	Tags           []string `json:"tags"`
}

type CreateCheckpointRequest struct {
	ChapterID  *string `json:"chapter_id"`
	ChapterNum *int    `json:"chapter_num"`
	Note       string  `json:"note"`
	Progress   int     `json:"progress"`
}

func (s *SubplotService) List(ctx context.Context, projectID string) ([]Subplot, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, title, line_label, description, status, priority,
		        start_chapter, resolve_chapter, COALESCE(tags, '{}'), created_at, updated_at
		 FROM subplots WHERE project_id = $1 ORDER BY line_label, priority`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list subplots: %w", err)
	}
	defer rows.Close()
	var out []Subplot
	for rows.Next() {
		var sp Subplot
		if err := rows.Scan(&sp.ID, &sp.ProjectID, &sp.Title, &sp.LineLabel,
			&sp.Description, &sp.Status, &sp.Priority,
			&sp.StartChapter, &sp.ResolveChapter, &sp.Tags,
			&sp.CreatedAt, &sp.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *SubplotService) Create(ctx context.Context, projectID string, req CreateSubplotRequest) (*Subplot, error) {
	id := uuid.New().String()
	label := req.LineLabel
	if label == "" {
		label = "A"
	}
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	prio := req.Priority
	if prio <= 0 {
		prio = 3
	}
	var sp Subplot
	err := s.db.QueryRow(ctx,
		`INSERT INTO subplots (id, project_id, title, line_label, description, priority, start_chapter, tags)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id, project_id, title, line_label, description, status, priority,
		           start_chapter, resolve_chapter, COALESCE(tags,'{}'), created_at, updated_at`,
		id, projectID, req.Title, label, req.Description, prio, req.StartChapter, tags).
		Scan(&sp.ID, &sp.ProjectID, &sp.Title, &sp.LineLabel, &sp.Description,
			&sp.Status, &sp.Priority, &sp.StartChapter, &sp.ResolveChapter,
			&sp.Tags, &sp.CreatedAt, &sp.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create subplot: %w", err)
	}
	return &sp, nil
}

func (s *SubplotService) Update(ctx context.Context, id string, req UpdateSubplotRequest) (*Subplot, error) {
	var sp Subplot
	err := s.db.QueryRow(ctx,
		`UPDATE subplots SET
		   title           = COALESCE(NULLIF($1,''), title),
		   line_label      = COALESCE(NULLIF($2,''), line_label),
		   description     = COALESCE(NULLIF($3,''), description),
		   status          = COALESCE(NULLIF($4,''), status),
		   priority        = CASE WHEN $5 > 0 THEN $5 ELSE priority END,
		   resolve_chapter = COALESCE($6, resolve_chapter),
		   tags            = COALESCE($7, tags)
		 WHERE id = $8
		 RETURNING id, project_id, title, line_label, description, status, priority,
		           start_chapter, resolve_chapter, COALESCE(tags,'{}'), created_at, updated_at`,
		req.Title, req.LineLabel, req.Description, req.Status, req.Priority,
		req.ResolveChapter, req.Tags, id).
		Scan(&sp.ID, &sp.ProjectID, &sp.Title, &sp.LineLabel, &sp.Description,
			&sp.Status, &sp.Priority, &sp.StartChapter, &sp.ResolveChapter,
			&sp.Tags, &sp.CreatedAt, &sp.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update subplot: %w", err)
	}
	return &sp, nil
}

func (s *SubplotService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM subplots WHERE id = $1`, id)
	return err
}

func (s *SubplotService) ListCheckpoints(ctx context.Context, subplotID string) ([]SubplotCheckpoint, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, subplot_id, chapter_id, chapter_num, note, progress, created_at
		 FROM subplot_checkpoints WHERE subplot_id = $1 ORDER BY chapter_num`, subplotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubplotCheckpoint
	for rows.Next() {
		var cp SubplotCheckpoint
		if err := rows.Scan(&cp.ID, &cp.SubplotID, &cp.ChapterID, &cp.ChapterNum,
			&cp.Note, &cp.Progress, &cp.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	return out, rows.Err()
}

func (s *SubplotService) AddCheckpoint(ctx context.Context, subplotID string, req CreateCheckpointRequest) (*SubplotCheckpoint, error) {
	id := uuid.New().String()
	var cp SubplotCheckpoint
	err := s.db.QueryRow(ctx,
		`INSERT INTO subplot_checkpoints (id, subplot_id, chapter_id, chapter_num, note, progress)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING id, subplot_id, chapter_id, chapter_num, note, progress, created_at`,
		id, subplotID, req.ChapterID, req.ChapterNum, req.Note, req.Progress).
		Scan(&cp.ID, &cp.SubplotID, &cp.ChapterID, &cp.ChapterNum, &cp.Note, &cp.Progress, &cp.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("add checkpoint: %w", err)
	}
	return &cp, nil
}
