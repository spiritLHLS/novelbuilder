package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTaskListParamsFromRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/api/projects/project-123/tasks?page=2&page_size=50&status=running&type=rag_rebuild", nil)
	c.Params = gin.Params{{Key: "id", Value: "project-123"}}

	params := taskListParamsFromRequest(c)

	if params.ProjectID != "project-123" {
		t.Fatalf("ProjectID = %q, want project-123", params.ProjectID)
	}
	if params.Page != 2 || params.PageSize != 50 {
		t.Fatalf("pagination = page %d size %d, want page 2 size 50", params.Page, params.PageSize)
	}
	if params.Status != "running" || params.TaskType != "rag_rebuild" {
		t.Fatalf("filters = status %q type %q, want running/rag_rebuild", params.Status, params.TaskType)
	}
}

func TestTaskListParamsRejectInvalidPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/api/tasks?page=-4&page_size=500", nil)

	params := taskListParamsFromRequest(c)

	if params.Page != 1 || params.PageSize != 10 {
		t.Fatalf("pagination = page %d size %d, want defaults page 1 size 10", params.Page, params.PageSize)
	}
}

func TestTaskPagination(t *testing.T) {
	pagination := taskPagination(95, 20, 2)

	if pagination["page"] != 2 {
		t.Fatalf("page = %v, want 2", pagination["page"])
	}
	if pagination["page_size"] != 20 {
		t.Fatalf("page_size = %v, want 20", pagination["page_size"])
	}
	if pagination["total"] != 95 {
		t.Fatalf("total = %v, want 95", pagination["total"])
	}
	if pagination["total_pages"] != 5 {
		t.Fatalf("total_pages = %v, want 5", pagination["total_pages"])
	}
}
