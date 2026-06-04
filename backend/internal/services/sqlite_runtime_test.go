package services

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/novelbuilder/backend/internal/config"
	"github.com/novelbuilder/backend/internal/database"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

func newSQLiteRuntime(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.NewPool(config.DatabaseConfig{
		Driver:       "sqlite",
		SQLitePath:   filepath.Join(t.TempDir(), "novelbuilder.db"),
		MaxOpenConns: 4,
		MaxIdleConns: 1,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPool sqlite: %v", err)
	}
	t.Cleanup(db.Close)
	if err := database.AutoMigrate(context.Background(), db.GORM(), zap.NewNop()); err != nil {
		t.Fatalf("AutoMigrate sqlite: %v", err)
	}
	return db
}

func TestSQLiteRuntimeProjectAndTaskQueue(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteRuntime(t)

	projects := NewProjectService(db, db.GORM(), zap.NewNop())
	project, err := projects.Create(ctx, models.CreateProjectRequest{Title: "SQLite Story", Genre: "test"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	list, err := projects.List(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(list) != 1 || list[0].ID != project.ID {
		t.Fatalf("unexpected project list: %#v", list)
	}

	queue := NewTaskQueueService(db, 1, 3, zap.NewNop())
	task, err := queue.Enqueue(ctx, models.CreateTaskRequest{
		ProjectID: project.ID,
		TaskType:  "test_task",
		Priority:  5,
	})
	if err != nil {
		t.Fatalf("enqueue task: %v", err)
	}
	got, err := queue.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got == nil || got.ID != task.ID || got.Status != "pending" {
		t.Fatalf("unexpected task: %#v", got)
	}
}

func TestTaskQueueCancelRunningTask(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteRuntime(t)
	queue := NewTaskQueueService(db, 1, 3, zap.NewNop())
	started := make(chan struct{})
	queue.RegisterHandler("slow_task", func(ctx context.Context, _ models.TaskQueueItem) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})

	task, err := queue.Enqueue(ctx, models.CreateTaskRequest{TaskType: "slow_task", Priority: 5})
	if err != nil {
		t.Fatalf("enqueue task: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- queue.processOne(ctx) }()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	if err := queue.Cancel(ctx, task.ID); err != nil {
		t.Fatalf("cancel task: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("processOne returned error after cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task did not stop after cancellation")
	}

	got, err := queue.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got == nil || got.Status != "cancelled" {
		t.Fatalf("expected cancelled task, got %#v", got)
	}
}

func TestTaskQueueStatsAggregatesSQLite(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteRuntime(t)
	projects := NewProjectService(db, db.GORM(), zap.NewNop())
	project, err := projects.Create(ctx, models.CreateProjectRequest{Title: "Stats Story", Genre: "test"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	queue := NewTaskQueueService(db, 1, 3, zap.NewNop())
	if _, err := queue.Enqueue(ctx, models.CreateTaskRequest{
		ProjectID: project.ID,
		TaskType:  "pending_task",
		Priority:  1,
	}); err != nil {
		t.Fatalf("enqueue pending task: %v", err)
	}

	now := time.Now()
	scheduled := now.Add(-90 * time.Second)
	started := now.Add(-60 * time.Second)
	completed := now.Add(-10 * time.Second)
	if _, err := db.Exec(ctx,
		`INSERT INTO task_queue
		   (id, project_id, task_type, payload, status, priority, attempts, max_attempts, error_message,
		    scheduled_at, started_at, completed_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,'done',5,1,3,'',$5,$6,$7,$5,$7)`,
		"33333333-3333-3333-3333-333333333333",
		project.ID,
		"done_task",
		[]byte(`{}`),
		scheduled,
		started,
		completed,
	); err != nil {
		t.Fatalf("insert done task: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO task_queue
		   (id, project_id, task_type, payload, status, priority, attempts, max_attempts, error_message,
		    scheduled_at, started_at, completed_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,'failed',5,2,3,$5,$6,$7,$8,$6,$8)`,
		"44444444-4444-4444-4444-444444444444",
		project.ID,
		"failed_task",
		[]byte(`{}`),
		"temporary upstream timeout",
		scheduled,
		started,
		completed,
	); err != nil {
		t.Fatalf("insert failed task: %v", err)
	}

	stats, err := queue.Stats(ctx, project.ID)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Total != 3 || stats.Pending != 1 || stats.Done != 1 || stats.Failed != 1 {
		t.Fatalf("unexpected status aggregate: %#v", stats)
	}
	if stats.Retried != 1 || stats.Done24h != 1 {
		t.Fatalf("unexpected retry/throughput aggregate: %#v", stats)
	}
	if stats.AverageQueueMs <= 0 || stats.AverageRuntimeMs <= 0 {
		t.Fatalf("expected positive timing aggregates: %#v", stats)
	}
	if len(stats.FailureReasons) != 1 || stats.FailureReasons[0].Message != "temporary upstream timeout" || stats.FailureReasons[0].Count != 1 {
		t.Fatalf("unexpected failure reasons: %#v", stats.FailureReasons)
	}
	if len(stats.ProjectThroughput) != 1 || stats.ProjectThroughput[0].ProjectID != project.ID || stats.ProjectThroughput[0].Done24h != 1 {
		t.Fatalf("unexpected project throughput: %#v", stats.ProjectThroughput)
	}
}

func TestTaskRetryBackoffBounds(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 2 * time.Second},
		{attempt: 1, want: 2 * time.Second},
		{attempt: 2, want: 4 * time.Second},
		{attempt: 8, want: 256 * time.Second},
		{attempt: 9, want: 5 * time.Minute},
		{attempt: 99, want: 5 * time.Minute},
	}
	for _, tt := range tests {
		if got := taskRetryBackoff(tt.attempt); got != tt.want {
			t.Fatalf("taskRetryBackoff(%d) = %s, want %s", tt.attempt, got, tt.want)
		}
	}
}

func TestTaskQueueFailedTaskSchedulesRetryWithBackoff(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteRuntime(t)
	queue := NewTaskQueueService(db, 1, 3, zap.NewNop())
	handlerErr := errors.New("temporary outage")
	queue.RegisterHandler("retry_task", func(context.Context, models.TaskQueueItem) error {
		return handlerErr
	})

	task, err := queue.Enqueue(ctx, models.CreateTaskRequest{TaskType: "retry_task", Priority: 5, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue task: %v", err)
	}
	before := time.Now()
	if err := queue.processOne(ctx); !errors.Is(err, handlerErr) {
		t.Fatalf("processOne error = %v, want %v", err, handlerErr)
	}

	got, err := queue.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got == nil || got.Status != "pending" || got.Attempts != 1 {
		t.Fatalf("expected retryable pending task with one attempt, got %#v", got)
	}
	if got.ErrorMessage != handlerErr.Error() {
		t.Fatalf("unexpected error message: %q", got.ErrorMessage)
	}
	delay := got.ScheduledAt.Sub(before)
	if delay < 1500*time.Millisecond || delay > 5*time.Second {
		t.Fatalf("expected retry delay around 2s, got %s at %s", delay, got.ScheduledAt)
	}
}

func TestTaskQueuePermanentFailureSetsCompletedAt(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteRuntime(t)
	queue := NewTaskQueueService(db, 1, 3, zap.NewNop())
	handlerErr := errors.New("permanent outage")
	queue.RegisterHandler("fail_task", func(context.Context, models.TaskQueueItem) error {
		return handlerErr
	})

	task, err := queue.Enqueue(ctx, models.CreateTaskRequest{TaskType: "fail_task", Priority: 5, MaxAttempts: 1})
	if err != nil {
		t.Fatalf("enqueue task: %v", err)
	}
	if err := queue.processOne(ctx); !errors.Is(err, handlerErr) {
		t.Fatalf("processOne error = %v, want %v", err, handlerErr)
	}
	got, err := queue.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got == nil || got.Status != "failed" || got.Attempts != 1 {
		t.Fatalf("expected permanently failed task, got %#v", got)
	}
	if got.CompletedAt == nil || got.CompletedAt.IsZero() {
		t.Fatalf("expected completed_at on permanently failed task, got %#v", got)
	}
}

func TestTaskQueueRecoverStaleRunningUsesRetryState(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteRuntime(t)
	queue := NewTaskQueueService(db, 1, 3, zap.NewNop())
	staleAt := time.Now().Add(-10 * time.Minute)
	pendingID := "55555555-5555-5555-5555-555555555555"
	failedID := "66666666-6666-6666-6666-666666666666"

	if _, err := db.Exec(ctx,
		`INSERT INTO task_queue
		   (id, task_type, payload, status, priority, attempts, max_attempts, error_message,
		    scheduled_at, started_at, created_at, updated_at)
		 VALUES ($1,'stale_retry',$2,'running',5,1,3,'upstream reset',$3,$3,$3,$3)`,
		pendingID,
		[]byte(`{}`),
		staleAt,
	); err != nil {
		t.Fatalf("insert retryable stale task: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO task_queue
		   (id, task_type, payload, status, priority, attempts, max_attempts, error_message,
		    scheduled_at, started_at, created_at, updated_at)
		 VALUES ($1,'stale_failed',$2,'running',5,3,3,'already retried',$3,$3,$3,$3)`,
		failedID,
		[]byte(`{}`),
		staleAt,
	); err != nil {
		t.Fatalf("insert exhausted stale task: %v", err)
	}

	before := time.Now()
	queue.recoverStaleRunning(ctx)

	pending, err := queue.Get(ctx, pendingID)
	if err != nil {
		t.Fatalf("get recovered pending task: %v", err)
	}
	if pending == nil || pending.Status != "pending" || pending.Attempts != 1 {
		t.Fatalf("unexpected retryable stale task: %#v", pending)
	}
	if !strings.Contains(pending.ErrorMessage, "recovered from stale running state") {
		t.Fatalf("missing recovery marker: %q", pending.ErrorMessage)
	}
	delay := pending.ScheduledAt.Sub(before)
	if delay < 1500*time.Millisecond || delay > 5*time.Second {
		t.Fatalf("expected recovered task retry delay around 2s, got %s", delay)
	}

	failed, err := queue.Get(ctx, failedID)
	if err != nil {
		t.Fatalf("get recovered failed task: %v", err)
	}
	if failed == nil || failed.Status != "failed" || failed.CompletedAt == nil {
		t.Fatalf("expected exhausted stale task to fail permanently, got %#v", failed)
	}
}

func TestLLMProfileUsageAggregatesWithoutPerProfileQueries(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteRuntime(t)
	service := NewLLMProfileService(db, "test-key", zap.NewNop())
	profile, err := service.Create(ctx, models.CreateLLMProfileRequest{
		Name:        "Usage Model",
		Provider:    "openai_compatible",
		BaseURL:     "http://127.0.0.1:11434/v1",
		APIKey:      "test",
		ModelName:   "usage-model",
		MaxTokens:   1024,
		Temperature: 0.7,
		IsDefault:   true,
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO chapters (id, project_id, chapter_num, title, content, word_count, gen_params, input_tokens, output_tokens, status, version, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'draft',1,NOW(),NOW())`,
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		1,
		"chapter",
		"content",
		7,
		[]byte(`{"llm_config":{"model":"usage-model"}}`),
		123,
		456,
	); err != nil {
		t.Fatalf("insert usage chapter: %v", err)
	}

	usage, err := service.Usage(ctx)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if len(usage) != 1 || usage[0].ProfileID != profile.ID {
		t.Fatalf("unexpected usage rows: %#v", usage)
	}
	if usage[0].ChapterCount != 1 || usage[0].InputTokens != 123 || usage[0].OutputTokens != 456 || usage[0].TotalTokens != 579 {
		t.Fatalf("unexpected usage aggregate: %#v", usage[0])
	}
}
