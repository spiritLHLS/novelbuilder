import sys

content = '''package handlers

import (
\t"encoding/json"
\t"fmt"

\t"github.com/gin-gonic/gin"
\t"github.com/novelbuilder/backend/internal/models"
)

// ── Project CRUD ──────────────────────────────────────────────────────────────

func (h *Handler) ListProjects(c *gin.Context) {
\tprojects, err := h.projects.List(c.Request.Context())
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(200, gin.H{"data": projects})
}

func (h *Handler) CreateProject(c *gin.Context) {
\tvar req models.CreateProjectRequest
\tif err := c.ShouldBindJSON(&req); err != nil {
\t\tc.JSON(400, gin.H{"error": err.Error()})
\t\treturn
\t}
\tproject, err := h.projects.Create(c.Request.Context(), req)
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(201, gin.H{"data": project})
}

func (h *Handler) GetProject(c *gin.Context) {
\tproject, err := h.projects.Get(c.Request.Context(), c.Param("id"))
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tif project == nil {
\t\tc.JSON(404, gin.H{"error": "project not found"})
\t\treturn
\t}
\tc.JSON(200, gin.H{"data": project})
}

func (h *Handler) UpdateProject(c *gin.Context) {
\tvar req models.CreateProjectRequest
\tif err := c.ShouldBindJSON(&req); err != nil {
\t\tc.JSON(400, gin.H{"error": err.Error()})
\t\treturn
\t}
\tproject, err := h.projects.Update(c.Request.Context(), c.Param("id"), req)
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(200, gin.H{"data": project})
}

func (h *Handler) DeleteProject(c *gin.Context) {
\tif err := h.projects.Delete(c.Request.Context(), c.Param("id")); err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(204, nil)
}

// ── Blueprint Workflow ────────────────────────────────────────────────────────

func (h *Handler) GenerateBlueprint(c *gin.Context) {
\tvar req models.GenerateBlueprintRequest
\tif err := c.ShouldBindJSON(&req); err != nil {
\t\tc.JSON(400, gin.H{"error": err.Error()})
\t\treturn
\t}
\tbp, err := h.blueprints.Generate(c.Request.Context(), c.Param("id"), req)
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(201, gin.H{"data": bp})
}

func (h *Handler) GetBlueprint(c *gin.Context) {
\tbp, err := h.blueprints.Get(c.Request.Context(), c.Param("id"))
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tif bp == nil {
\t\tc.JSON(404, gin.H{"error": "blueprint not found"})
\t\treturn
\t}
\tc.JSON(200, gin.H{"data": bp})
}

func (h *Handler) SubmitBlueprintReview(c *gin.Context) {
\tif err := h.blueprints.SubmitReview(c.Request.Context(), c.Param("id")); err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(200, gin.H{"status": "pending_review"})
}

func (h *Handler) ApproveBlueprint(c *gin.Context) {
\tvar req models.ReviewRequest
\tc.ShouldBindJSON(&req)
\tif err := h.blueprints.Approve(c.Request.Context(), c.Param("id"), req.ReviewComment); err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(200, gin.H{"status": "approved"})
}

func (h *Handler) RejectBlueprint(c *gin.Context) {
\tvar req models.ReviewRequest
\tc.ShouldBindJSON(&req)
\tif err := h.blueprints.Reject(c.Request.Context(), c.Param("id"), req.ReviewComment); err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(200, gin.H{"status": "rejected"})
}

// ── World Bible ───────────────────────────────────────────────────────────────

func (h *Handler) GetWorldBible(c *gin.Context) {
\twb, err := h.worldBibles.Get(c.Request.Context(), c.Param("id"))
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tif wb == nil {
\t\tc.JSON(404, gin.H{"error": "world bible not found"})
\t\treturn
\t}
\tc.JSON(200, gin.H{"data": wb})
}

func (h *Handler) UpdateWorldBible(c *gin.Context) {
\tvar body json.RawMessage
\tif err := c.ShouldBindJSON(&body); err != nil {
\t\tc.JSON(400, gin.H{"error": err.Error()})
\t\treturn
\t}
\twb, err := h.worldBibles.Update(c.Request.Context(), c.Param("id"), body)
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(200, gin.H{"data": wb})
}

func (h *Handler) GetConstitution(c *gin.Context) {
\twbc, err := h.worldBibles.GetConstitution(c.Request.Context(), c.Param("id"))
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tif wbc == nil {
\t\tc.JSON(404, gin.H{"error": "constitution not found"})
\t\treturn
\t}
\tc.JSON(200, gin.H{"data": wbc})
}

func (h *Handler) UpdateConstitution(c *gin.Context) {
\tvar body struct {
\t\tImmutableRules   json.RawMessage `json:"immutable_rules"`
\t\tMutableRules     json.RawMessage `json:"mutable_rules"`
\t\tForbiddenAnchors json.RawMessage `json:"forbidden_anchors"`
\t}
\tif err := c.ShouldBindJSON(&body); err != nil {
\t\tc.JSON(400, gin.H{"error": err.Error()})
\t\treturn
\t}
\twbc, err := h.worldBibles.UpdateConstitution(c.Request.Context(), c.Param("id"),
\t\tbody.ImmutableRules, body.MutableRules, body.ForbiddenAnchors)
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(200, gin.H{"data": wbc})
}

// ── Fan Fiction Settings ──────────────────────────────────────────────────────

func (h *Handler) UpdateProjectFanfic(c *gin.Context) {
\tprojectID := c.Param("id")
\tvar req models.UpdateProjectFanficRequest
\tif err := c.ShouldBindJSON(&req); err != nil {
\t\tc.JSON(400, gin.H{"error": err.Error()})
\t\treturn
\t}
\tif req.FanficMode != nil {
\t\tallowed := map[string]bool{"canon": true, "au": true, "ooc": true, "cp": true, "": true}
\t\tif !allowed[*req.FanficMode] {
\t\t\tc.JSON(400, gin.H{"error": "fanfic_mode must be one of: canon, au, ooc, cp"})
\t\t\treturn
\t\t}
\t}
\tif err := h.projects.UpdateFanfic(c.Request.Context(), projectID, req.FanficMode, req.FanficSourceText); err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(200, gin.H{"ok": true})
}

// ── Auto-Write Daemon ─────────────────────────────────────────────────────────

func (h *Handler) SetAutoWrite(c *gin.Context) {
\tprojectID := c.Param("id")
\tvar req models.AutoWriteRequest
\tif err := c.ShouldBindJSON(&req); err != nil {
\t\tc.JSON(400, gin.H{"error": err.Error()})
\t\treturn
\t}
\tenabled := req.IntervalMinutes > 0
\tinterval := req.IntervalMinutes
\tif interval <= 0 {
\t\tinterval = 60
\t}
\tif err := h.projects.SetAutoWrite(c.Request.Context(), projectID, enabled, interval); err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(200, gin.H{"auto_write_enabled": enabled, "auto_write_interval": interval})
}

// ── Analytics ─────────────────────────────────────────────────────────────────

func (h *Handler) GetProjectAnalytics(c *gin.Context) {
\tdata, err := h.analytics.GetProjectAnalytics(c.Request.Context(), c.Param("id"))
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.JSON(200, gin.H{"data": data})
}

// ── Export ────────────────────────────────────────────────────────────────────

func (h *Handler) ExportTXT(c *gin.Context) {
\tprojectID := c.Param("id")
\tdata, err := h.export.ExportTXT(c.Request.Context(), projectID)
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\\"novel_%s.txt\\"", projectID))
\tc.Data(200, "text/plain; charset=utf-8", data)
}

func (h *Handler) ExportMarkdown(c *gin.Context) {
\tprojectID := c.Param("id")
\tdata, err := h.export.ExportMarkdown(c.Request.Context(), projectID)
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\\"novel_%s.md\\"", projectID))
\tc.Data(200, "text/markdown; charset=utf-8", data)
}

func (h *Handler) ExportEPUB(c *gin.Context) {
\tprojectID := c.Param("id")
\tdata, err := h.export.ExportEPUB(c.Request.Context(), projectID)
\tif err != nil {
\t\tc.JSON(500, gin.H{"error": err.Error()})
\t\treturn
\t}
\tc.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="novel_%s.epub"`, projectID))
\tc.Data(200, "application/epub+zip", data)
}
'''

with open('/Volumes/Additional/\u4e2a\u4eba\u6570\u636e/GitHub/novelbuilder/backend/internal/handlers/handler_projects.go', 'w') as f:
    f.write(content)

print("Done")
