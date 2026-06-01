package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/database"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// TaskHandler is called by the worker pool to handle a task.
// Returning an error marks the task as failed (retried if attempts < max_attempts).
type TaskHandler func(ctx context.Context, task models.TaskQueueItem) error

type TaskQueueService struct {
	db         *database.DB
	maxRetries int
	workers    int
	logger     *zap.Logger

	handlers   map[string]TaskHandler
	mu         sync.RWMutex
	runningMu  sync.RWMutex
	running    map[string]context.CancelFunc
	stop       chan struct{}
	stopCtx    context.Context
	stopCancel context.CancelFunc
	wg         sync.WaitGroup
}

func NewTaskQueueService(db *database.DB, workers, maxRetries int, logger *zap.Logger) *TaskQueueService {
	ctx, cancel := context.WithCancel(context.Background())
	return &TaskQueueService{
		db:         db,
		workers:    workers,
		maxRetries: maxRetries,
		logger:     logger,
		handlers:   make(map[string]TaskHandler),
		running:    make(map[string]context.CancelFunc),
		stop:       make(chan struct{}),
		stopCtx:    ctx,
		stopCancel: cancel,
	}
}

// RegisterHandler registers a TaskHandler for the given task type.
func (s *TaskQueueService) RegisterHandler(taskType string, h TaskHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[taskType] = h
}

// Start launches the worker goroutines. Call once at application startup.
// It also recovers any tasks that were left in 'running' state by a previous
// server crash so they can be retried.
func (s *TaskQueueService) Start() {
	s.recoverStaleRunning(context.Background())
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	// Periodically recover tasks that got stuck in 'running' after a crash/restart.
	s.wg.Add(1)
	go s.recoveryWorker()
	s.logger.Info("task queue started", zap.Int("workers", s.workers))
}

// recoverStaleRunning resets tasks that have been stuck in 'running' state for more
// than 5 minutes back to 'pending' so they can be retried. This handles the case
// where the server crashed while a task was executing.
func (s *TaskQueueService) recoverStaleRunning(ctx context.Context) {
	tag, err := s.db.Exec(ctx,
		`UPDATE task_queue
		 SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
		     error_message = COALESCE(error_message, '') || ' [recovered from stale running state]',
		     scheduled_at = NOW(),
		     updated_at = NOW()
		 WHERE status = 'running'
		   AND updated_at < NOW() - INTERVAL '5 minutes'`)
	if err != nil {
		s.logger.Warn("failed to recover stale running tasks", zap.Error(err))
		return
	}
	if tag.RowsAffected() > 0 {
		s.logger.Info("recovered stale running tasks", zap.Int64("count", tag.RowsAffected()))
	}
}

// recoveryWorker runs recoverStaleRunning every 5 minutes so tasks that become
// stale after startup are also cleaned up.
func (s *TaskQueueService) recoveryWorker() {
	defer s.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.recoverStaleRunning(s.stopCtx)
		}
	}
}

// Stop signals workers to exit and waits for them.
func (s *TaskQueueService) Stop() {
	s.stopCancel()
	close(s.stop)
	s.wg.Wait()
	s.logger.Info("task queue stopped")
}

func (s *TaskQueueService) worker() {
	defer s.wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			if err := s.processOne(s.stopCtx); err != nil {
				s.logger.Debug("task worker: no task or error", zap.Error(err))
			}
		}
	}
}

func (s *TaskQueueService) registerRunningTask(id string, cancel context.CancelFunc) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	s.running[id] = cancel
}

func (s *TaskQueueService) unregisterRunningTask(id string) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	delete(s.running, id)
}

func (s *TaskQueueService) runningCancel(id string) (context.CancelFunc, bool) {
	s.runningMu.RLock()
	defer s.runningMu.RUnlock()
	cancel, ok := s.running[id]
	return cancel, ok
}

func queueFinishContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil || parent.Err() != nil {
		return context.WithTimeout(context.Background(), 10*time.Second)
	}
	return parent, func() {}
}

func (s *TaskQueueService) taskStatus(ctx context.Context, id string) (string, error) {
	var status string
	err := s.db.QueryRow(ctx, `SELECT status FROM task_queue WHERE id=$1`, id).Scan(&status)
	if errors.Is(err, database.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return status, nil
}

func (s *TaskQueueService) isTaskCancelled(ctx context.Context, id string) (bool, error) {
	status, err := s.taskStatus(ctx, id)
	return status == "cancelled", err
}

// processOne claims and runs a single pending task. Returns an error when no task is available.
func (s *TaskQueueService) processOne(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var task models.TaskQueueItem
	err = tx.QueryRow(ctx,
		`SELECT id, project_id, task_type, payload, status, priority, attempts, max_attempts, error_message, scheduled_at, created_at, updated_at
		 FROM task_queue t1
		 WHERE status = 'pending' AND scheduled_at <= NOW()
		   AND (
		     t1.project_id IS NULL
		     OR NOT EXISTS (
		       SELECT 1 FROM projects p
		       WHERE p.id = t1.project_id
		         AND COALESCE(p.status, '') IN ('paused', 'cancelled', 'terminated', 'archived')
		     )
		   )
		   AND (
		     t1.project_id IS NULL
		     OR NOT EXISTS (
		       SELECT 1 FROM task_queue t2
		       WHERE t2.status = 'running'
		         AND t2.project_id = t1.project_id
		         AND (
		           t2.task_type = t1.task_type
		           OR (
		             t1.task_type IN ('chapter_generate', 'generate_next_chapter', 'chapter_regenerate', 'chapter_import_process')
		             AND t2.task_type IN ('chapter_generate', 'generate_next_chapter', 'chapter_regenerate', 'chapter_import_process')
		           )
		         )
		     )
		   )
		 ORDER BY priority DESC, created_at ASC
		 FOR UPDATE SKIP LOCKED
		 LIMIT 1`).Scan(
		&task.ID, &task.ProjectID, &task.TaskType, &task.Payload,
		&task.Status, &task.Priority, &task.Attempts, &task.MaxAttempts,
		&task.ErrorMessage, &task.ScheduledAt, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return err // database.ErrNoRows → nothing to do
	}

	now := time.Now()
	if _, err := tx.Exec(ctx,
		`UPDATE task_queue SET status='running', started_at=$1, attempts=attempts+1, updated_at=$1 WHERE id=$2`,
		now, task.ID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	started := time.Now()
	logFields := []zap.Field{
		zap.String("task_id", task.ID),
		zap.String("task_type", task.TaskType),
		zap.Int("attempt", task.Attempts+1),
		zap.Int("max_attempts", task.MaxAttempts),
	}
	if task.ProjectID != nil {
		logFields = append(logFields, zap.String("project_id", *task.ProjectID))
	}
	s.logger.Info("task started", logFields...)

	taskCtx, taskCancel := context.WithCancel(ctx)
	s.registerRunningTask(task.ID, taskCancel)
	defer func() {
		s.unregisterRunningTask(task.ID)
		taskCancel()
	}()

	if cancelled, err := s.isTaskCancelled(ctx, task.ID); err != nil {
		s.logger.Warn("task cancellation state check failed", append(logFields, zap.Error(err))...)
	} else if cancelled {
		s.logger.Info("task skipped because it was cancelled before handler start", logFields...)
		return nil
	}

	// Run handler OUTSIDE the transaction
	s.mu.RLock()
	h, ok := s.handlers[task.TaskType]
	s.mu.RUnlock()

	var handlerErr error
	if !ok {
		handlerErr = fmt.Errorf("no handler registered for task type %q", task.TaskType)
	} else {
		func() {
			defer func() {
				if r := recover(); r != nil {
					handlerErr = fmt.Errorf("task handler panic: %v", r)
					s.logger.Error("task handler panic",
						append(logFields,
							zap.Any("panic", r),
							zap.ByteString("stack", debug.Stack()),
						)...)
				}
			}()
			handlerErr = h(taskCtx, task)
		}()
	}

	finishCtx, finishCancel := queueFinishContext(ctx)
	defer finishCancel()

	if status, err := s.taskStatus(finishCtx, task.ID); err != nil {
		s.logger.Warn("task cancellation state check failed", append(logFields, zap.Error(err))...)
	} else if status == "cancelled" || status == "paused" {
		s.logger.Info("task external state acknowledged",
			append(logFields, zap.String("status", status), zap.Duration("duration", time.Since(started)))...)
		return nil
	}

	if handlerErr == nil {
		_, _ = s.db.Exec(finishCtx,
			`UPDATE task_queue SET status='done', completed_at=$1, updated_at=$1 WHERE id=$2`,
			time.Now(), task.ID)
		s.logger.Info("task completed", append(logFields, zap.Duration("duration", time.Since(started)))...)
		return nil
	}

	if errors.Is(handlerErr, context.Canceled) || errors.Is(taskCtx.Err(), context.Canceled) {
		if ctx.Err() != nil {
			_, _ = s.db.Exec(finishCtx,
				`UPDATE task_queue
				 SET status='pending', error_message=$1, scheduled_at=NOW(), updated_at=NOW()
				 WHERE id=$2 AND status='running'`,
				"server stopped while task was running; queued for retry", task.ID)
			s.logger.Info("task returned to queue during shutdown",
				append(logFields, zap.Duration("duration", time.Since(started)))...)
			return nil
		}
		if status, statusErr := s.taskStatus(finishCtx, task.ID); statusErr == nil {
			if status == "paused" || status == "cancelled" {
				s.logger.Info("task context stopped by external state",
					append(logFields, zap.String("status", status), zap.Duration("duration", time.Since(started)))...)
				return nil
			}
		}
		_, _ = s.db.Exec(finishCtx,
			`UPDATE task_queue
			 SET status='cancelled', error_message=$1, completed_at=NOW(), updated_at=NOW()
			 WHERE id=$2 AND status IN ('pending','running')`,
			"cancel requested", task.ID)
		s.logger.Info("task cancelled",
			append(logFields, zap.Duration("duration", time.Since(started)))...)
		return nil
	}

	// Handle failure: retry or mark as failed
	attempt := task.Attempts + 1
	maxAtt := task.MaxAttempts
	if maxAtt == 0 {
		maxAtt = s.maxRetries
	}

	if attempt < maxAtt {
		backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
		_, _ = s.db.Exec(finishCtx,
			`UPDATE task_queue SET status='pending', error_message=$1, scheduled_at=$2, updated_at=$3 WHERE id=$4`,
			handlerErr.Error(), time.Now().Add(backoff), time.Now(), task.ID)
		s.logger.Warn("task failed; scheduled retry",
			append(logFields,
				zap.Duration("duration", time.Since(started)),
				zap.Duration("backoff", backoff),
				zap.Error(handlerErr),
			)...)
	} else {
		_, _ = s.db.Exec(finishCtx,
			`UPDATE task_queue SET status='failed', error_message=$1, updated_at=$2 WHERE id=$3`,
			handlerErr.Error(), time.Now(), task.ID)
		s.logger.Error("task failed permanently",
			append(logFields,
				zap.Duration("duration", time.Since(started)),
				zap.Error(handlerErr),
			)...)
	}

	return handlerErr
}

// Enqueue inserts a new task into the queue.
// EnqueueBatch inserts all tasks in a single database batch.
// Returns the generated task IDs in the same order as reqs.
func (s *TaskQueueService) EnqueueBatch(ctx context.Context, reqs []models.CreateTaskRequest) ([]string, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	now := time.Now()
	ids := make([]string, len(reqs))
	batch := &database.Batch{}
	for i, req := range reqs {
		id := uuid.New().String()
		ids[i] = id
		payload := req.Payload
		if payload == nil {
			payload = json.RawMessage("{}")
		}
		maxAttempts := req.MaxAttempts
		if maxAttempts == 0 {
			maxAttempts = s.maxRetries
		}
		var projectID *string
		if req.ProjectID != "" {
			pid := req.ProjectID
			projectID = &pid
		}
		batch.Queue(
			`INSERT INTO task_queue
			    (id, project_id, task_type, payload, status, priority, attempts, max_attempts, scheduled_at, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,'pending',$5,0,$6,NOW(),$7,$7)`,
			id, projectID, req.TaskType, payload, req.Priority, maxAttempts, now)
	}
	br := s.db.SendBatch(ctx, batch)
	defer br.Close()
	for i := range reqs {
		if _, err := br.Exec(); err != nil {
			return nil, fmt.Errorf("enqueue batch task %d: %w", i+1, err)
		}
	}
	return ids, nil
}

// Enqueue inserts a single task. For bulk insertion use EnqueueBatch.
func (s *TaskQueueService) Enqueue(ctx context.Context, req models.CreateTaskRequest) (*models.TaskQueueItem, error) {
	id := uuid.New().String()
	now := time.Now()

	payload := req.Payload
	if payload == nil {
		payload = json.RawMessage("{}")
	}

	maxAttempts := req.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = s.maxRetries
	}

	// ProjectID: convert empty string to nil for nullable FK
	var projectID *string
	if req.ProjectID != "" {
		projectID = &req.ProjectID
	}

	_, err := s.db.Exec(ctx,
		`INSERT INTO task_queue (id, project_id, task_type, payload, status, priority, attempts, max_attempts, scheduled_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'pending', $5, 0, $6, NOW(), $7, $7)`,
		id, projectID, req.TaskType, payload, req.Priority, maxAttempts, now)
	if err != nil {
		return nil, fmt.Errorf("enqueue task: %w", err)
	}

	return &models.TaskQueueItem{
		ID: id, ProjectID: projectID, TaskType: req.TaskType,
		Payload: payload, Status: "pending", Priority: req.Priority,
		Attempts: 0, MaxAttempts: maxAttempts, ScheduledAt: now,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// Cancel marks a pending/running/paused task as cancelled. Running handlers receive a
// cancelled context; completion code re-checks the DB status before writing done.
func (s *TaskQueueService) Cancel(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE task_queue
		 SET status='cancelled', error_message=$1, completed_at=NOW(), updated_at=NOW()
		 WHERE id=$2 AND status IN ('pending','running','paused')`,
		"cancel requested", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var status string
		statusErr := s.db.QueryRow(ctx, `SELECT status FROM task_queue WHERE id=$1`, id).Scan(&status)
		if errors.Is(statusErr, database.ErrNoRows) {
			return fmt.Errorf("task %s not found", id)
		}
		if statusErr != nil {
			return statusErr
		}
		if status == "cancelled" {
			return nil
		}
		return fmt.Errorf("task %s is already %s and cannot be cancelled", id, status)
	}
	if cancel, ok := s.runningCancel(id); ok {
		cancel()
	}
	return nil
}

// Pause marks a pending/running task as paused. Running handlers receive a
// cancelled context; completion code sees the DB status and leaves it paused.
func (s *TaskQueueService) Pause(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE task_queue
		 SET status='paused', error_message=$1, updated_at=NOW()
		 WHERE id=$2 AND status IN ('pending','running')`,
		"pause requested", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var status string
		statusErr := s.db.QueryRow(ctx, `SELECT status FROM task_queue WHERE id=$1`, id).Scan(&status)
		if errors.Is(statusErr, database.ErrNoRows) {
			return fmt.Errorf("task %s not found", id)
		}
		if statusErr != nil {
			return statusErr
		}
		if status == "paused" {
			return nil
		}
		return fmt.Errorf("task %s is already %s and cannot be paused", id, status)
	}
	if cancel, ok := s.runningCancel(id); ok {
		cancel()
	}
	return nil
}

func (s *TaskQueueService) Resume(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE task_queue
		 SET status='pending', error_message='', scheduled_at=NOW(), updated_at=NOW()
		 WHERE id=$1 AND status='paused'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s not found or not paused", id)
	}
	return nil
}

func (s *TaskQueueService) PauseProject(ctx context.Context, projectID string) (int64, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id FROM task_queue WHERE project_id=$1 AND status='running'`, projectID)
	if err != nil {
		return 0, err
	}
	var runningIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			runningIDs = append(runningIDs, id)
		}
	}
	rows.Close()
	tag, err := s.db.Exec(ctx,
		`UPDATE task_queue
		 SET status='paused', error_message=$1, updated_at=NOW()
		 WHERE project_id=$2 AND status IN ('pending','running')`,
		"project pause requested", projectID)
	if err != nil {
		return 0, err
	}
	for _, id := range runningIDs {
		if cancel, ok := s.runningCancel(id); ok {
			cancel()
		}
	}
	return tag.RowsAffected(), nil
}

func (s *TaskQueueService) ResumeProject(ctx context.Context, projectID string) (int64, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE task_queue
		 SET status='pending', error_message='', scheduled_at=NOW(), updated_at=NOW()
		 WHERE project_id=$1 AND status='paused'`, projectID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *TaskQueueService) CancelProject(ctx context.Context, projectID string) (int64, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id FROM task_queue WHERE project_id=$1 AND status='running'`, projectID)
	if err != nil {
		return 0, err
	}
	var runningIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			runningIDs = append(runningIDs, id)
		}
	}
	rows.Close()
	tag, err := s.db.Exec(ctx,
		`UPDATE task_queue
		 SET status='cancelled', error_message=$1, completed_at=NOW(), updated_at=NOW()
		 WHERE project_id=$2 AND status IN ('pending','running','paused')`,
		"project cancel requested", projectID)
	if err != nil {
		return 0, err
	}
	for _, id := range runningIDs {
		if cancel, ok := s.runningCancel(id); ok {
			cancel()
		}
	}
	return tag.RowsAffected(), nil
}

func (s *TaskQueueService) ResetProjectTasks(ctx context.Context, projectID string) (int64, error) {
	cancelled, err := s.CancelProject(ctx, projectID)
	if err != nil {
		return 0, err
	}
	tag, err := s.db.Exec(ctx,
		`UPDATE task_queue
		 SET attempts=0, error_message='reset requested', updated_at=NOW()
		 WHERE project_id=$1 AND status IN ('failed','cancelled')`,
		projectID)
	if err != nil {
		return cancelled, err
	}
	return cancelled + tag.RowsAffected(), nil
}

// Retry resets a failed/cancelled task back to pending.
func (s *TaskQueueService) Retry(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE task_queue SET status='pending', attempts=0, error_message='', scheduled_at=NOW(), updated_at=NOW()
		 WHERE id=$1 AND status IN ('failed','cancelled')`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s not found or not retryable", id)
	}
	return nil
}

func (s *TaskQueueService) Get(ctx context.Context, id string) (*models.TaskQueueItem, error) {
	var t models.TaskQueueItem
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, task_type, payload, status, priority, attempts, max_attempts,
		        error_message, scheduled_at, started_at, completed_at, created_at, updated_at
		 FROM task_queue WHERE id = $1`, id).Scan(
		&t.ID, &t.ProjectID, &t.TaskType, &t.Payload, &t.Status, &t.Priority,
		&t.Attempts, &t.MaxAttempts, &t.ErrorMessage, &t.ScheduledAt,
		&t.StartedAt, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, database.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	return &t, nil
}

// ListParams contains pagination and filter params for List operation.
type TaskListParams struct {
	ProjectID string
	Status    string // filter by status (optional)
	TaskType  string // filter by task_type (optional)
	Page      int    // 1-based page number
	PageSize  int    // items per page
}

func (s *TaskQueueService) List(ctx context.Context, params TaskListParams) ([]models.TaskQueueItem, int, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > 100 {
		params.PageSize = 10
	}
	offset := (params.Page - 1) * params.PageSize

	// Build WHERE clause. Empty ProjectID means global task view.
	whereClauses := []string{"1 = 1"}
	args := []interface{}{}
	argIdx := 1

	if params.ProjectID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, params.ProjectID)
		argIdx++
	}

	if params.Status != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, params.Status)
		argIdx++
	}
	if params.TaskType != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("task_type = $%d", argIdx))
		args = append(args, params.TaskType)
		argIdx++
	}

	whereClause := strings.Join(whereClauses, " AND ")

	// Get total count
	var total int
	err := s.db.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM task_queue WHERE %s", whereClause), args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}

	// Get paginated results
	args = append(args, params.PageSize, offset)
	query := fmt.Sprintf(
		`SELECT id, project_id, task_type, payload, status, priority, attempts, max_attempts,
		        error_message, scheduled_at, started_at, completed_at, created_at, updated_at
		 FROM task_queue WHERE %s
		 ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, argIdx, argIdx+1)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []models.TaskQueueItem
	for rows.Next() {
		var t models.TaskQueueItem
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.TaskType, &t.Payload, &t.Status, &t.Priority,
			&t.Attempts, &t.MaxAttempts, &t.ErrorMessage, &t.ScheduledAt,
			&t.StartedAt, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, t)
	}
	return tasks, total, rows.Err()
}
