package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// TaskHandler is called by the worker pool to handle a task.
// Returning an error marks the task as failed (retried if attempts < max_attempts).
type TaskHandler func(ctx context.Context, payload json.RawMessage) error

type TaskQueueService struct {
	db         *pgxpool.Pool
	maxRetries int
	workers    int
	logger     *zap.Logger

	handlers map[string]TaskHandler
	mu       sync.RWMutex
	stop     chan struct{}
	wg       sync.WaitGroup
}

func NewTaskQueueService(db *pgxpool.Pool, workers, maxRetries int, logger *zap.Logger) *TaskQueueService {
	return &TaskQueueService{
		db:         db,
		workers:    workers,
		maxRetries: maxRetries,
		logger:     logger,
		handlers:   make(map[string]TaskHandler),
		stop:       make(chan struct{}),
	}
}

// RegisterHandler registers a TaskHandler for the given task type.
func (s *TaskQueueService) RegisterHandler(taskType string, h TaskHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[taskType] = h
}

// Start launches the worker goroutines. Call once at application startup.
func (s *TaskQueueService) Start() {
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	s.logger.Info("task queue started", zap.Int("workers", s.workers))
}

// Stop signals workers to exit and waits for them.
func (s *TaskQueueService) Stop() {
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
			if err := s.processOne(context.Background()); err != nil {
				s.logger.Debug("task worker: no task or error", zap.Error(err))
			}
		}
	}
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
		 FROM task_queue
		 WHERE status = 'pending' AND scheduled_at <= NOW()
		 ORDER BY priority DESC, created_at ASC
		 FOR UPDATE SKIP LOCKED
		 LIMIT 1`).Scan(
		&task.ID, &task.ProjectID, &task.TaskType, &task.Payload,
		&task.Status, &task.Priority, &task.Attempts, &task.MaxAttempts,
		&task.ErrorMessage, &task.ScheduledAt, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return err // pgx.ErrNoRows → nothing to do
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

	// Run handler OUTSIDE the transaction
	s.mu.RLock()
	h, ok := s.handlers[task.TaskType]
	s.mu.RUnlock()

	var handlerErr error
	if !ok {
		handlerErr = fmt.Errorf("no handler registered for task type %q", task.TaskType)
	} else {
		handlerErr = h(ctx, task.Payload)
	}

	if handlerErr == nil {
		_, _ = s.db.Exec(ctx,
			`UPDATE task_queue SET status='done', completed_at=$1, updated_at=$1 WHERE id=$2`,
			time.Now(), task.ID)
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
		_, _ = s.db.Exec(ctx,
			`UPDATE task_queue SET status='pending', error_message=$1, scheduled_at=$2, updated_at=$3 WHERE id=$4`,
			handlerErr.Error(), time.Now().Add(backoff), time.Now(), task.ID)
	} else {
		_, _ = s.db.Exec(ctx,
			`UPDATE task_queue SET status='failed', error_message=$1, updated_at=$2 WHERE id=$3`,
			handlerErr.Error(), time.Now(), task.ID)
	}

	return handlerErr
}

// Enqueue inserts a new task into the queue.
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

// Cancel marks a pending task as cancelled.
func (s *TaskQueueService) Cancel(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE task_queue SET status='cancelled', updated_at=NOW() WHERE id=$1 AND status='pending'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s not found or not in pending state", id)
	}
	return nil
}

// Retry resets a failed/cancelled task back to pending.
func (s *TaskQueueService) Retry(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE task_queue SET status='pending', attempts=0, error_message=NULL, scheduled_at=NOW(), updated_at=NOW()
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
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	return &t, nil
}

func (s *TaskQueueService) List(ctx context.Context, projectID string) ([]models.TaskQueueItem, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, task_type, payload, status, priority, attempts, max_attempts,
		        error_message, scheduled_at, started_at, completed_at, created_at, updated_at
		 FROM task_queue WHERE project_id = $1
		 ORDER BY created_at DESC LIMIT 200`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []models.TaskQueueItem
	for rows.Next() {
		var t models.TaskQueueItem
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.TaskType, &t.Payload, &t.Status, &t.Priority,
			&t.Attempts, &t.MaxAttempts, &t.ErrorMessage, &t.ScheduledAt,
			&t.StartedAt, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
