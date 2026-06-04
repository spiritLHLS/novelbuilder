package handlers

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOpenAPIPathConvertsGinParams(t *testing.T) {
	got := openAPIPath("/api/projects/:id/fanqie/upload/:chapter_id")
	want := "/api/projects/{id}/fanqie/upload/{chapter_id}"
	if got != want {
		t.Fatalf("openAPIPath() = %q, want %q", got, want)
	}
}

func TestBuildOpenAPISpecFromGinRoutes(t *testing.T) {
	spec := buildOpenAPISpec(gin.RoutesInfo{
		{Method: http.MethodGet, Path: "/api/projects/:id", Handler: "project"},
		{Method: http.MethodPost, Path: "/api/projects/:id/tasks", Handler: "tasks"},
		{Method: http.MethodOptions, Path: "/api/projects/:id", Handler: "options"},
		{Method: http.MethodGet, Path: "/assets/index.js", Handler: "static"},
	}, "test")

	paths, ok := spec["paths"].(gin.H)
	if !ok {
		t.Fatalf("paths missing from spec: %#v", spec)
	}
	if _, ok := paths["/assets/index.js"]; ok {
		t.Fatalf("non-API route included in spec: %#v", paths)
	}
	project, ok := paths["/api/projects/{id}"].(gin.H)
	if !ok {
		t.Fatalf("project path missing: %#v", paths)
	}
	if _, ok := project["options"]; ok {
		t.Fatalf("OPTIONS route included in spec: %#v", project)
	}
	if _, ok := project["get"]; !ok {
		t.Fatalf("GET operation missing: %#v", project)
	}
	tasks, ok := paths["/api/projects/{id}/tasks"].(gin.H)
	if !ok {
		t.Fatalf("tasks path missing: %#v", paths)
	}
	if _, ok := tasks["post"]; !ok {
		t.Fatalf("POST operation missing: %#v", tasks)
	}
}
