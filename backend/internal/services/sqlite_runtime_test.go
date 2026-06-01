package services

import (
	"context"
	"path/filepath"
	"testing"

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
	if err := database.EnsureRuntimeSchema(context.Background(), db, zap.NewNop()); err != nil {
		t.Fatalf("EnsureRuntimeSchema sqlite: %v", err)
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
