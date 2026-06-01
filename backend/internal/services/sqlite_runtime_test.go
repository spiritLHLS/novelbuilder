package services

import (
	"context"
	"path/filepath"
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
