package handlers

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/config"
	"github.com/novelbuilder/backend/internal/database"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/services"
	"go.uber.org/zap"
)

func newAccessTestHandler(t *testing.T) (*Handler, *services.ProjectService, *database.DB) {
	t.Helper()
	db, err := database.NewPool(config.DatabaseConfig{
		Driver:       "sqlite",
		SQLitePath:   filepath.Join(t.TempDir(), "novelbuilder.db"),
		MaxOpenConns: 2,
		MaxIdleConns: 1,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPool sqlite: %v", err)
	}
	t.Cleanup(db.Close)
	if err := database.AutoMigrate(context.Background(), db.GORM(), zap.NewNop()); err != nil {
		t.Fatalf("AutoMigrate sqlite: %v", err)
	}
	projects := services.NewProjectService(db, db.GORM(), zap.NewNop())
	return &Handler{projects: projects}, projects, db
}

func TestProjectIDForWorkflowHistoryUsesWorkflowProjectSQLite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, projects, db := newAccessTestHandler(t)
	ctx := context.Background()

	project, err := projects.CreateForOwner(ctx, models.CreateProjectRequest{Title: "Workflow Story"}, "user-1")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	runID := "11111111-1111-1111-1111-111111111111"
	now := time.Now()
	if _, err := db.Exec(ctx,
		`INSERT INTO workflow_runs (id, project_id, strict_review, current_step, status, created_at, updated_at)
		 VALUES ($1, $2, TRUE, 'init', 'running', $3, $3)`,
		runID, project.ID, now,
	); err != nil {
		t.Fatalf("insert workflow run: %v", err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/api/workflows/"+runID+"/history", nil)
	c.Params = gin.Params{{Key: "id", Value: runID}}

	got, scoped, err := h.projectIDForRequest(c, "/api/workflows/:id/history")
	if err != nil {
		t.Fatalf("projectIDForRequest: %v", err)
	}
	if !scoped {
		t.Fatal("expected workflow history to be project scoped")
	}
	if got != project.ID {
		t.Fatalf("project id = %q, want %q", got, project.ID)
	}
}

func TestProjectIDForWorkflowStepUsesRunProjectSQLite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, projects, db := newAccessTestHandler(t)
	ctx := context.Background()

	project, err := projects.CreateForOwner(ctx, models.CreateProjectRequest{Title: "Workflow Step Story"}, "user-1")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	runID := "22222222-2222-2222-2222-222222222222"
	stepID := "33333333-3333-3333-3333-333333333333"
	now := time.Now()
	if _, err := db.Exec(ctx,
		`INSERT INTO workflow_runs (id, project_id, strict_review, current_step, status, created_at, updated_at)
		 VALUES ($1, $2, TRUE, 'init', 'running', $3, $3)`,
		runID, project.ID, now,
	); err != nil {
		t.Fatalf("insert workflow run: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO workflow_steps (id, run_id, step_key, step_order, gate_level, status, created_at)
		 VALUES ($1, $2, 'blueprint', 1, 'manual', 'pending', $3)`,
		stepID, runID, now,
	); err != nil {
		t.Fatalf("insert workflow step: %v", err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/workflow-steps/"+stepID+"/approve", nil)
	c.Params = gin.Params{{Key: "id", Value: stepID}}

	got, scoped, err := h.projectIDForRequest(c, "/api/workflow-steps/:id/approve")
	if err != nil {
		t.Fatalf("projectIDForRequest: %v", err)
	}
	if !scoped {
		t.Fatal("expected workflow step to be project scoped")
	}
	if got != project.ID {
		t.Fatalf("project id = %q, want %q", got, project.ID)
	}
}

func TestProjectIDForReferenceChapterUsesReferenceProjectSQLite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, projects, db := newAccessTestHandler(t)
	ctx := context.Background()

	project, err := projects.CreateForOwner(ctx, models.CreateProjectRequest{Title: "Reference Story"}, "user-1")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	refID := "44444444-4444-4444-4444-444444444444"
	chapterID := "55555555-5555-5555-5555-555555555555"
	now := time.Now()
	if _, err := db.Exec(ctx,
		`INSERT INTO reference_materials (id, project_id, title, status, created_at)
		 VALUES ($1, $2, 'Reference', 'completed', $3)`,
		refID, project.ID, now,
	); err != nil {
		t.Fatalf("insert reference material: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO reference_book_chapters (id, ref_id, chapter_no, title, content, word_count, is_deleted, created_at)
		 VALUES ($1, $2, 1, 'Chapter 1', 'content', 7, FALSE, $3)`,
		chapterID, refID, now,
	); err != nil {
		t.Fatalf("insert reference chapter: %v", err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("DELETE", "/api/reference-chapters/"+chapterID, nil)
	c.Params = gin.Params{{Key: "id", Value: chapterID}}

	got, scoped, err := h.projectIDForRequest(c, "/api/reference-chapters/:id")
	if err != nil {
		t.Fatalf("projectIDForRequest: %v", err)
	}
	if !scoped {
		t.Fatal("expected reference chapter to be project scoped")
	}
	if got != project.ID {
		t.Fatalf("project id = %q, want %q", got, project.ID)
	}
}

func TestProjectIDFromNullableTaskReturnsEmptySQLite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _, db := newAccessTestHandler(t)
	ctx := context.Background()

	taskID := "66666666-6666-6666-6666-666666666666"
	now := time.Now()
	if _, err := db.Exec(ctx,
		`INSERT INTO task_queue (id, project_id, task_type, payload, status, priority, attempts, max_attempts, error_message, created_at, updated_at)
		 VALUES ($1, NULL, 'global', '{}', 'pending', 5, 0, 3, '', $2, $2)`,
		taskID, now,
	); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/api/tasks/"+taskID, nil)
	c.Params = gin.Params{{Key: "id", Value: taskID}}

	got, scoped, err := h.projectIDForRequest(c, "/api/tasks/:id")
	if err != nil {
		t.Fatalf("projectIDForRequest: %v", err)
	}
	if !scoped {
		t.Fatal("expected task to be scoped")
	}
	if got != "" {
		t.Fatalf("project id = %q, want empty for nullable project task", got)
	}
}
