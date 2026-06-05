package handlers

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/database"
	"github.com/novelbuilder/backend/internal/models"
)

func currentUser(c *gin.Context) models.UserSession {
	userID, _ := c.Get("user_id")
	username, _ := c.Get("username")
	role, _ := c.Get("user_role")
	return models.UserSession{
		UserID:   stringValue(userID),
		Username: stringValue(username),
		Role:     stringValue(role),
	}
}

func stringValue(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func isAdmin(session models.UserSession) bool {
	return session.Role == models.UserRoleAdmin
}

func requireAdminPath(method, path string) bool {
	if strings.HasPrefix(path, "/api/users") ||
		strings.HasPrefix(path, "/api/settings") ||
		strings.HasPrefix(path, "/api/logs") ||
		strings.HasPrefix(path, "/api/doctor") ||
		strings.HasPrefix(path, "/api/genre-templates") ||
		strings.HasPrefix(path, "/api/agent-routes") {
		return true
	}
	if path == "/api/tasks" || path == "/api/tasks/stats" || path == "/api/tasks/stream" {
		return true
	}
	if strings.HasPrefix(path, "/api/llm-profiles") {
		return true
	}
	return false
}

func (h *Handler) AuthorizeAPI(c *gin.Context) {
	session := currentUser(c)
	if isAdmin(session) {
		c.Next()
		return
	}
	routePath := c.FullPath()
	if routePath == "" {
		c.Next()
		return
	}
	if requireAdminPath(c.Request.Method, routePath) {
		c.AbortWithStatusJSON(403, gin.H{"error": "admin role required"})
		return
	}
	projectID, scoped, err := h.projectIDForRequest(c, routePath)
	if err != nil {
		c.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
		return
	}
	if !scoped {
		c.Next()
		return
	}
	if projectID == "" {
		c.AbortWithStatusJSON(403, gin.H{"error": "project-scoped resource is not available to this user"})
		return
	}
	allowed, err := h.userCanAccessProject(c.Request.Context(), session.UserID, projectID)
	if err != nil {
		c.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
		return
	}
	if !allowed {
		c.AbortWithStatusJSON(404, gin.H{"error": "project not found"})
		return
	}
	c.Next()
}

func (h *Handler) projectIDForRequest(c *gin.Context, routePath string) (string, bool, error) {
	if strings.HasPrefix(routePath, "/api/projects/:id") {
		return c.Param("id"), true, nil
	}
	id := c.Param("id")
	if id == "" {
		id = c.Param("sid")
	}
	if id == "" {
		id = c.Param("bid")
	}
	if id == "" {
		return "", false, nil
	}
	switch {
	case routePath == "/api/workflows/:id/history":
		return h.projectIDFromTable(c, "workflow_runs", id)
	case strings.HasPrefix(routePath, "/api/blueprints/:id"):
		return h.projectIDFromTable(c, "book_blueprints", id)
	case strings.HasPrefix(routePath, "/api/chapters/:id"):
		return h.projectIDFromTable(c, "chapters", id)
	case strings.HasPrefix(routePath, "/api/characters/:id"):
		return h.projectIDFromTable(c, "characters", id)
	case strings.HasPrefix(routePath, "/api/outlines/:id"):
		return h.projectIDFromTable(c, "outlines", id)
	case strings.HasPrefix(routePath, "/api/foreshadowings/:id"):
		return h.projectIDFromTable(c, "foreshadowings", id)
	case strings.HasPrefix(routePath, "/api/volumes/:id"):
		return h.projectIDFromTable(c, "volumes", id)
	case strings.HasPrefix(routePath, "/api/references/:id"):
		return h.projectIDFromTable(c, "reference_materials", id)
	case strings.HasPrefix(routePath, "/api/reference-chapters/:id"):
		return h.projectIDFromJoin(c, `SELECT r.project_id
			FROM reference_book_chapters rc
			JOIN reference_materials r ON r.id = rc.ref_id
			WHERE rc.id = $1`, id)
	case strings.HasPrefix(routePath, "/api/agent-reviews/:id"):
		return h.projectIDFromTable(c, "agent_review_sessions", id)
	case strings.HasPrefix(routePath, "/api/workflows/:id"):
		return h.projectIDFromTable(c, "workflow_runs", id)
	case strings.HasPrefix(routePath, "/api/workflow-steps/:id"):
		return h.projectIDFromJoin(c, `SELECT wr.project_id
			FROM workflow_steps ws
			JOIN workflow_runs wr ON wr.id = ws.run_id
			WHERE ws.id = $1`, id)
	case strings.HasPrefix(routePath, "/api/resources/:id"):
		return h.projectIDFromTable(c, "story_resources", id)
	case strings.HasPrefix(routePath, "/api/patch-plans/:id"):
		return h.projectIDFromTable(c, "patch_plans", id)
	case strings.HasPrefix(routePath, "/api/patch-items/:id"):
		return h.projectIDFromJoin(c, `SELECT pp.project_id
			FROM patch_items pi
			JOIN patch_plans pp ON pp.id = pi.plan_id
			WHERE pi.id = $1`, id)
	case strings.HasPrefix(routePath, "/api/webhooks/:id"):
		return h.projectIDFromTable(c, "notification_webhooks", id)
	case strings.HasPrefix(routePath, "/api/subplots/:id"):
		return h.projectIDFromTable(c, "subplots", id)
	case strings.HasPrefix(routePath, "/api/emotional-arcs/:id"):
		return h.projectIDFromTable(c, "emotional_arc_entries", id)
	case strings.HasPrefix(routePath, "/api/character-interactions/:id"):
		return h.projectIDFromTable(c, "character_interactions", id)
	case strings.HasPrefix(routePath, "/api/imports/:id"):
		return h.projectIDFromTable(c, "chapter_imports", id)
	case strings.HasPrefix(routePath, "/api/agent/sessions/:sid"):
		return h.projectIDFromTable(c, "agent_sessions", id)
	case strings.HasPrefix(routePath, "/api/agent/batch/:bid"):
		return h.projectIDFromBatchSession(c, id)
	case strings.HasPrefix(routePath, "/api/tasks/:id"):
		return h.projectIDFromTable(c, "task_queue", id)
	case strings.HasPrefix(routePath, "/api/prompt-presets/:id"):
		return h.projectIDFromTable(c, "prompt_presets", id)
	case strings.HasPrefix(routePath, "/api/glossary/:id"):
		return h.projectIDFromTable(c, "glossary_terms", id)
	}
	return "", false, nil
}

func (h *Handler) projectIDFromTable(c *gin.Context, table, id string) (string, bool, error) {
	query := "SELECT project_id FROM " + table + " WHERE id = $1"
	return h.projectIDFromJoin(c, query, id)
}

func (h *Handler) projectIDFromJoin(c *gin.Context, query, id string) (string, bool, error) {
	var projectID sql.NullString
	err := h.projects.DB().QueryRow(c.Request.Context(), query, id).Scan(&projectID)
	if errors.Is(err, database.ErrNoRows) {
		return "", true, nil
	}
	if err != nil {
		return "", true, err
	}
	if !projectID.Valid {
		return "", true, nil
	}
	return projectID.String, true, nil
}

func (h *Handler) projectIDFromBatchSession(c *gin.Context, id string) (string, bool, error) {
	// Batch sessions currently live in memory in the Python sidecar and are not
	// persisted in Go. Non-admin users can only start them from a project route.
	return "", true, nil
}

func (h *Handler) userCanAccessProject(ctx context.Context, userID, projectID string) (bool, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(projectID) == "" {
		return false, nil
	}
	var exists bool
	err := h.projects.DB().QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM projects WHERE id = $1 AND owner_id = $2)`,
		projectID, userID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
