package handlers

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/services"
)

// ── Project CRUD ──────────────────────────────────────────────────────────────

func (h *Handler) ListProjects(c *gin.Context) {
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": projects})
}

func (h *Handler) CreateProject(c *gin.Context) {
	var req models.CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	project, err := h.projects.Create(c.Request.Context(), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": project})
}

func (h *Handler) GetProject(c *gin.Context) {
	project, err := h.projects.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if project == nil {
		c.JSON(404, gin.H{"error": "project not found"})
		return
	}
	c.JSON(200, gin.H{"data": project})
}

func (h *Handler) UpdateProject(c *gin.Context) {
	var req models.CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	project, err := h.projects.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": project})
}

func (h *Handler) DeleteProject(c *gin.Context) {
	if err := h.projects.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

// ── Blueprint Workflow ────────────────────────────────────────────────────────

func (h *Handler) GenerateBlueprint(c *gin.Context) {
	var req models.GenerateBlueprintRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	bp, err := h.blueprints.Generate(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// 202: generation is running in the background; caller should poll GET blueprint.
	c.JSON(202, gin.H{"data": bp})
}

func (h *Handler) GetBlueprint(c *gin.Context) {
	bp, err := h.blueprints.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if bp == nil {
		c.JSON(404, gin.H{"error": "blueprint not found"})
		return
	}
	c.JSON(200, gin.H{"data": bp})
}

func (h *Handler) SubmitBlueprintReview(c *gin.Context) {
	if err := h.blueprints.SubmitReview(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "pending_review"})
}

func (h *Handler) ApproveBlueprint(c *gin.Context) {
	var req models.ReviewRequest
	c.ShouldBindJSON(&req)
	if err := h.blueprints.Approve(c.Request.Context(), c.Param("id"), req.ReviewComment); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "approved"})
}

func (h *Handler) RejectBlueprint(c *gin.Context) {
	var req models.ReviewRequest
	c.ShouldBindJSON(&req)
	if err := h.blueprints.Reject(c.Request.Context(), c.Param("id"), req.ReviewComment); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "rejected"})
}

// ── World Bible ───────────────────────────────────────────────────────────────

func (h *Handler) GetWorldBible(c *gin.Context) {
	wb, err := h.worldBibles.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if wb == nil {
		c.JSON(404, gin.H{"error": "world bible not found"})
		return
	}
	c.JSON(200, gin.H{"data": wb})
}

func (h *Handler) UpdateWorldBible(c *gin.Context) {
	var body json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	wb, err := h.worldBibles.Update(c.Request.Context(), c.Param("id"), body)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": wb})
}

func (h *Handler) GetConstitution(c *gin.Context) {
	wbc, err := h.worldBibles.GetConstitution(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if wbc == nil {
		c.JSON(404, gin.H{"error": "constitution not found"})
		return
	}
	c.JSON(200, gin.H{"data": wbc})
}

func (h *Handler) UpdateConstitution(c *gin.Context) {
	var body struct {
		ImmutableRules   json.RawMessage `json:"immutable_rules"`
		MutableRules     json.RawMessage `json:"mutable_rules"`
		ForbiddenAnchors json.RawMessage `json:"forbidden_anchors"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	wbc, err := h.worldBibles.UpdateConstitution(c.Request.Context(), c.Param("id"),
		body.ImmutableRules, body.MutableRules, body.ForbiddenAnchors)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": wbc})
}

// ── Fan Fiction Settings ──────────────────────────────────────────────────────

// ExportWorldBible streams a JSON bundle containing the world bible + constitution.
func (h *Handler) ExportWorldBible(c *gin.Context) {
	projectID := c.Param("id")
	bundle, err := h.worldBibles.Export(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="world_bible.json"`)
	c.JSON(200, bundle)
}

// ImportWorldBible accepts a JSON bundle and merges it into the project's world bible.
func (h *Handler) ImportWorldBible(c *gin.Context) {
	projectID := c.Param("id")
	var bundle services.WorldBibleBundle
	if err := c.ShouldBindJSON(&bundle); err != nil {
		c.JSON(400, gin.H{"error": "invalid bundle JSON: " + err.Error()})
		return
	}
	if err := h.worldBibles.Import(c.Request.Context(), projectID, &bundle); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "imported"})
}

func (h *Handler) UpdateProjectFanfic(c *gin.Context) {
	projectID := c.Param("id")
	var req models.UpdateProjectFanficRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.FanficMode != nil {
		allowed := map[string]bool{"canon": true, "au": true, "ooc": true, "cp": true, "": true}
		if !allowed[*req.FanficMode] {
			c.JSON(400, gin.H{"error": "fanfic_mode must be one of: canon, au, ooc, cp"})
			return
		}
	}
	if err := h.projects.UpdateFanfic(c.Request.Context(), projectID, req.FanficMode, req.FanficSourceText); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ── Auto-Write Daemon ─────────────────────────────────────────────────────────

func (h *Handler) SetAutoWrite(c *gin.Context) {
	projectID := c.Param("id")
	var req models.AutoWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	enabled := req.IntervalMinutes > 0
	interval := req.IntervalMinutes
	if interval <= 0 {
		interval = 60
	}
	if interval > 1440 {
		interval = 1440
	}
	if err := h.projects.SetAutoWrite(c.Request.Context(), projectID, enabled, interval); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"auto_write_enabled": enabled, "auto_write_interval": interval})
}

// ── Analytics ─────────────────────────────────────────────────────────────────

func (h *Handler) GetProjectAnalytics(c *gin.Context) {
	data, err := h.analytics.GetProjectAnalytics(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": data})
}

// ── Export ────────────────────────────────────────────────────────────────────

func (h *Handler) ExportTXT(c *gin.Context) {
	projectID := c.Param("id")
	data, err := h.export.ExportTXT(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"novel_%s.txt\"", projectID))
	c.Data(200, "text/plain; charset=utf-8", data)
}

func (h *Handler) ExportMarkdown(c *gin.Context) {
	projectID := c.Param("id")
	data, err := h.export.ExportMarkdown(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"novel_%s.md\"", projectID))
	c.Data(200, "text/markdown; charset=utf-8", data)
}

func (h *Handler) ExportEPUB(c *gin.Context) {
	projectID := c.Param("id")
	data, err := h.export.ExportEPUB(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="novel_%s.epub"`, projectID))
	c.Data(200, "application/epub+zip", data)
}
