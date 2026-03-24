package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

var (
	ErrBlueprintNotApproved   = errors.New("WF_001: 整书资产包必须审核通过")
	ErrPrevChapterNotApproved = errors.New("WF_002: 上一章尚未审核通过")
	ErrVolumeGateClosed       = errors.New("WF_003: 当前卷尚未通过卷级审核")
	ErrInvalidTransition      = errors.New("WF_004: 无效状态转换")
	ErrSnapshotNotFound       = errors.New("WF_005: 未找到可回退快照")
	ErrOptimisticLock         = errors.New("WF_006: 并发修改冲突")
	ErrStrictReviewRequired   = errors.New("WF_007: 严格审核模式需要人工审核")
)

type Engine struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewEngine(db *pgxpool.Pool, logger *zap.Logger) *Engine {
	return &Engine{db: db, logger: logger}
}

func (e *Engine) CreateRun(ctx context.Context, projectID string, strictReview bool) (string, error) {
	runID := uuid.New().String()
	_, err := e.db.Exec(ctx,
		`INSERT INTO workflow_runs (id, project_id, strict_review, current_step, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'blueprint', 'running', NOW(), NOW())`,
		runID, projectID, strictReview)
	if err != nil {
		return "", fmt.Errorf("create run: %w", err)
	}
	return runID, nil
}

func (e *Engine) CanGenerateNextChapter(ctx context.Context, projectID string) error {
	// Check blueprint approval: first look for an approved workflow step (tracked path),
	// then fall back to checking book_blueprints directly. This handles the common case
	// where the user approves a blueprint before or without starting the workflow.
	var bpApproved bool
	err := e.db.QueryRow(ctx,
		`SELECT (
			EXISTS(
				SELECT 1 FROM workflow_steps ws
				JOIN workflow_runs wr ON ws.run_id = wr.id
				WHERE wr.project_id = $1 AND ws.step_key = 'blueprint' AND ws.status = 'approved'
			) OR EXISTS(
				SELECT 1 FROM book_blueprints
				WHERE project_id = $1 AND status = 'approved'
			)
		)`, projectID).Scan(&bpApproved)
	if err != nil {
		return fmt.Errorf("check blueprint: %w", err)
	}
	if !bpApproved {
		return ErrBlueprintNotApproved
	}

	var lastChapterNum int
	err = e.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(chapter_num), 0) FROM chapters WHERE project_id = $1`, projectID).Scan(&lastChapterNum)
	if err != nil {
		return fmt.Errorf("check chapters: %w", err)
	}

	if lastChapterNum == 0 {
		return nil
	}

	// Check the strict_review setting on the most recent workflow run.
	// In non-strict (auto) mode, chapters are auto-approved by the task pipeline
	// so we only block on explicitly rejected chapters.
	// In strict mode (manual review), each chapter must be explicitly approved
	// before the next can be generated.
	var strictReview bool
	if qErr := e.db.QueryRow(ctx,
		`SELECT COALESCE(strict_review, true)
		 FROM workflow_runs
		 WHERE project_id = $1
		 ORDER BY created_at DESC LIMIT 1`, projectID).Scan(&strictReview); qErr != nil {
		// If no workflow run exists or query fails, default to strict for safety.
		strictReview = true
	}

	if !strictReview {
		// Auto mode: only block if the last chapter is explicitly rejected.
		var rejected bool
		if err = e.db.QueryRow(ctx,
			`SELECT EXISTS(
				SELECT 1 FROM chapters WHERE project_id = $1 AND chapter_num = $2 AND status = 'rejected'
			)`, projectID, lastChapterNum).Scan(&rejected); err != nil {
			return fmt.Errorf("check prev chapter rejected: %w", err)
		}
		if rejected {
			return ErrPrevChapterNotApproved
		}
		return nil
	}

	// Strict mode: require explicit human approval.
	var prevApproved bool
	err = e.db.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM chapters WHERE project_id = $1 AND chapter_num = $2 AND status = 'approved'
		)`, projectID, lastChapterNum).Scan(&prevApproved)
	if err != nil {
		return fmt.Errorf("check prev chapter: %w", err)
	}
	if !prevApproved {
		return ErrPrevChapterNotApproved
	}

	return nil
}

// IsStrictReview returns true if the latest workflow run for the project has
// strict_review enabled (i.e. chapters require manual human approval).
func (e *Engine) IsStrictReview(ctx context.Context, projectID string) bool {
	var strict bool
	if err := e.db.QueryRow(ctx,
		`SELECT COALESCE(strict_review, true)
		 FROM workflow_runs
		 WHERE project_id = $1
		 ORDER BY created_at DESC LIMIT 1`, projectID).Scan(&strict); err != nil {
		return true // safe default
	}
	return strict
}

func (e *Engine) TransitStep(ctx context.Context, stepID string, toStatus string, version int) error {
	result, err := e.db.Exec(ctx,
		`UPDATE workflow_steps SET status = $1, version = version + 1, reviewed_at = NOW()
		 WHERE id = $2 AND version = $3`,
		toStatus, stepID, version)
	if err != nil {
		return fmt.Errorf("transit step: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrOptimisticLock
	}
	return nil
}

func (e *Engine) CreateStep(ctx context.Context, runID, stepKey, gateLevel string, stepOrder int) (string, error) {
	stepID := uuid.New().String()
	_, err := e.db.Exec(ctx,
		`INSERT INTO workflow_steps (id, run_id, step_key, step_order, gate_level, status, version, created_at)
		 VALUES ($1, $2, $3, $4, $5, 'pending', 0, NOW())`,
		stepID, runID, stepKey, stepOrder, gateLevel)
	if err != nil {
		return "", fmt.Errorf("create step: %w", err)
	}
	return stepID, nil
}

func (e *Engine) MarkStepGenerated(ctx context.Context, stepID, outputRef string) error {
	_, err := e.db.Exec(ctx,
		`UPDATE workflow_steps SET status = 'generated', output_ref = $1, generated_at = NOW() WHERE id = $2`,
		outputRef, stepID)
	return err
}

type RunHistory struct {
	ID           string       `json:"id"`
	ProjectID    string       `json:"project_id"`
	StrictReview bool         `json:"strict_review"`
	CurrentStep  string       `json:"current_step"`
	Status       string       `json:"status"`
	CreatedAt    time.Time    `json:"created_at"`
	Steps        []StepDetail `json:"steps"`
}

type StepDetail struct {
	ID            string     `json:"id"`
	StepKey       string     `json:"step_key"`
	StepOrder     int        `json:"step_order"`
	GateLevel     string     `json:"gate_level"`
	Status        string     `json:"status"`
	OutputRef     *string    `json:"output_ref"`
	ReviewComment string     `json:"review_comment"`
	Version       int        `json:"version"`
	GeneratedAt   *time.Time `json:"generated_at"`
	ReviewedAt    *time.Time `json:"reviewed_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (e *Engine) GetRunHistory(ctx context.Context, projectID string) ([]RunHistory, error) {
	// Single JOIN query eliminates the N+1 pattern.
	rows, err := e.db.Query(ctx,
		`SELECT wr.id, wr.project_id, wr.strict_review, wr.current_step, wr.status, wr.created_at,
		        ws.id, ws.step_key, ws.step_order, ws.gate_level, ws.status,
		        ws.output_ref, ws.review_comment, ws.version, ws.generated_at, ws.reviewed_at, ws.created_at
		 FROM workflow_runs wr
		 LEFT JOIN workflow_steps ws ON ws.run_id = wr.id
		 WHERE wr.project_id = $1
		 ORDER BY wr.created_at DESC, ws.step_order`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query run history: %w", err)
	}
	defer rows.Close()

	runMap := make(map[string]*RunHistory)
	var runOrder []string

	for rows.Next() {
		var (
			runID, runProjectID, currentStep, runStatus string
			strictReview                                bool
			runCreatedAt                                time.Time
			// step columns are nullable due to LEFT JOIN
			stepID        *string
			stepKey       *string
			stepOrder     *int
			gateLevel     *string
			stepStatus    *string
			outputRef     *string
			reviewComment *string
			version       *int
			generatedAt   *time.Time
			reviewedAt    *time.Time
			stepCreatedAt *time.Time
		)
		if err := rows.Scan(
			&runID, &runProjectID, &strictReview, &currentStep, &runStatus, &runCreatedAt,
			&stepID, &stepKey, &stepOrder, &gateLevel, &stepStatus,
			&outputRef, &reviewComment, &version, &generatedAt, &reviewedAt, &stepCreatedAt,
		); err != nil {
			return nil, err
		}

		if _, exists := runMap[runID]; !exists {
			runMap[runID] = &RunHistory{
				ID:           runID,
				ProjectID:    runProjectID,
				StrictReview: strictReview,
				CurrentStep:  currentStep,
				Status:       runStatus,
				CreatedAt:    runCreatedAt,
			}
			runOrder = append(runOrder, runID)
		}

		if stepID != nil {
			rc := ""
			if reviewComment != nil {
				rc = *reviewComment
			}
			runMap[runID].Steps = append(runMap[runID].Steps, StepDetail{
				ID:            *stepID,
				StepKey:       *stepKey,
				StepOrder:     *stepOrder,
				GateLevel:     *gateLevel,
				Status:        *stepStatus,
				OutputRef:     outputRef,
				ReviewComment: rc,
				Version:       *version,
				GeneratedAt:   generatedAt,
				ReviewedAt:    reviewedAt,
				CreatedAt:     *stepCreatedAt,
			})
		}
	}

	result := make([]RunHistory, 0, len(runOrder))
	for _, id := range runOrder {
		result = append(result, *runMap[id])
	}
	return result, nil
}

func (e *Engine) CheckIdempotency(ctx context.Context, key, action string) (bool, []byte, error) {
	var responseBody []byte
	err := e.db.QueryRow(ctx,
		`SELECT response_body FROM idempotency_keys WHERE idempotency_key = $1 AND action = $2`,
		key, action).Scan(&responseBody)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("check idempotency: %w", err)
	}
	return true, responseBody, nil
}

func (e *Engine) SaveIdempotency(ctx context.Context, key, action, requestHash string, statusCode int, responseBody []byte) {
	_, err := e.db.Exec(ctx,
		`INSERT INTO idempotency_keys (idempotency_key, action, request_hash, status_code, response_body, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW()) ON CONFLICT (idempotency_key, action) DO NOTHING`,
		key, action, requestHash, statusCode, responseBody)
	if err != nil {
		e.logger.Warn("failed to save idempotency key", zap.Error(err))
	}
}

func (e *Engine) Rollback(ctx context.Context, runID, targetStepID, reason string) (int, error) {
	var snapshotExists bool
	err := e.db.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM workflow_snapshots WHERE run_id = $1 AND step_key = (
				SELECT step_key FROM workflow_steps WHERE id = $2
			)
		)`, runID, targetStepID).Scan(&snapshotExists)
	if err != nil {
		return 0, fmt.Errorf("check snapshot: %w", err)
	}
	if !snapshotExists {
		return 0, ErrSnapshotNotFound
	}

	tx, err := e.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var targetOrder int
	err = tx.QueryRow(ctx,
		`SELECT step_order FROM workflow_steps WHERE id = $1 AND run_id = $2`,
		targetStepID, runID).Scan(&targetOrder)
	if err != nil {
		return 0, fmt.Errorf("query target step: %w", err)
	}

	result, err := tx.Exec(ctx,
		`UPDATE workflow_steps SET status = 'needs_recheck', reviewed_at = NOW()
		 WHERE run_id = $1 AND step_order > $2`,
		runID, targetOrder)
	if err != nil {
		return 0, fmt.Errorf("rollback steps: %w", err)
	}
	affected := int(result.RowsAffected())

	_, err = tx.Exec(ctx,
		`UPDATE workflow_steps SET status = 'pending', version = version + 1 WHERE id = $1`,
		targetStepID)
	if err != nil {
		return 0, fmt.Errorf("reset target: %w", err)
	}

	reviewID := uuid.New().String()
	_, err = tx.Exec(ctx,
		`INSERT INTO workflow_reviews (id, step_id, action, operator, reason, from_step_order, to_step_order, created_at)
		 VALUES ($1, $2, 'rollback', 'user', $3, $4, $5, NOW())`,
		reviewID, targetStepID, reason, targetOrder, targetOrder)
	if err != nil {
		return 0, fmt.Errorf("record review: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit rollback: %w", err)
	}

	e.logger.Info("workflow rolled back",
		zap.String("run_id", runID),
		zap.String("target_step_id", targetStepID),
		zap.Int("affected", affected))

	return affected, nil
}

func (e *Engine) SaveSnapshot(ctx context.Context, runID, stepKey string, params, contextPayload, outputPayload, qualityPayload json.RawMessage) error {
	snapshotID := uuid.New().String()
	_, err := e.db.Exec(ctx,
		`INSERT INTO workflow_snapshots (id, run_id, step_key, params, context_payload, output_payload, quality_payload, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		snapshotID, runID, stepKey, params, contextPayload, outputPayload, qualityPayload)
	if err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	return nil
}

// InitRunSteps creates the standard initial workflow steps for a new run and syncs
// their status with the current blueprint state of the project.
func (e *Engine) InitRunSteps(ctx context.Context, runID, projectID string) error {
	// Always create the blueprint step as the first tracked step.
	stepID, err := e.CreateStep(ctx, runID, "blueprint", "strict", 1)
	if err != nil {
		return fmt.Errorf("create blueprint step: %w", err)
	}

	// Sync the blueprint step with whatever state the blueprint is already in.
	var bpID, bpStatus string
	if qErr := e.db.QueryRow(ctx,
		`SELECT id, status FROM book_blueprints
		 WHERE project_id = $1
		 ORDER BY created_at DESC LIMIT 1`, projectID).Scan(&bpID, &bpStatus); qErr == nil {
		switch bpStatus {
		case "approved":
			// Blueprint already fully approved: mark step generated then approve it.
			_ = e.MarkStepGenerated(ctx, stepID, bpID)
			_ = e.TransitStep(ctx, stepID, "approved", 0)
		case "pending_review", "generated":
			// Blueprint exists but awaiting review: mark as generated.
			_ = e.MarkStepGenerated(ctx, stepID, bpID)
		}
		// "draft" or other states: step stays in 'pending'.
	}

	return nil
}

func (e *Engine) CompleteRun(ctx context.Context, runID, status string) error {
	_, err := e.db.Exec(ctx,
		`UPDATE workflow_runs SET status = $1, updated_at = NOW() WHERE id = $2`,
		status, runID)
	return err
}

// SnapshotPayload is returned by GetSnapshot.
type SnapshotPayload struct {
	StepKey        string          `json:"step_key"`
	Params         json.RawMessage `json:"params"`
	ContextPayload json.RawMessage `json:"context_payload"`
	OutputPayload  json.RawMessage `json:"output_payload"`
	QualityPayload json.RawMessage `json:"quality_payload"`
}

// GetSnapshot retrieves the most-recent snapshot for a given run + step key.
func (e *Engine) GetSnapshot(ctx context.Context, runID, stepKey string) (*SnapshotPayload, error) {
	var s SnapshotPayload
	err := e.db.QueryRow(ctx,
		`SELECT step_key, params, context_payload, output_payload, quality_payload
		 FROM workflow_snapshots WHERE run_id = $1 AND step_key = $2
		 ORDER BY created_at DESC LIMIT 1`,
		runID, stepKey).Scan(&s.StepKey, &s.Params, &s.ContextPayload, &s.OutputPayload, &s.QualityPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get snapshot: %w", err)
	}
	return &s, nil
}
