package handlers

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/services"
)

func (h *Handler) ListCharacters(c *gin.Context) {
	chars, err := h.characters.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": chars})
}

func (h *Handler) CreateCharacter(c *gin.Context) {
	var body struct {
		Name     string          `json:"name" binding:"required"`
		RoleType string          `json:"role_type"`
		Profile  json.RawMessage `json:"profile"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ch, err := h.characters.Create(c.Request.Context(), c.Param("id"), body.Name, body.RoleType, body.Profile)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": ch})
}

func (h *Handler) GetCharacter(c *gin.Context) {
	ch, err := h.characters.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if ch == nil {
		c.JSON(404, gin.H{"error": "character not found"})
		return
	}
	c.JSON(200, gin.H{"data": ch})
}

func (h *Handler) UpdateCharacter(c *gin.Context) {
	var body struct {
		Name     string          `json:"name"`
		RoleType string          `json:"role_type"`
		Profile  json.RawMessage `json:"profile"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ch, err := h.characters.Update(c.Request.Context(), c.Param("id"), body.Name, body.RoleType, body.Profile)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": ch})
}

func (h *Handler) DeleteCharacter(c *gin.Context) {
	if err := h.characters.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

func (h *Handler) ListOutlines(c *gin.Context) {
	outlines, err := h.outlines.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": outlines})
}

func (h *Handler) CreateOutline(c *gin.Context) {
	var body struct {
		Level         string          `json:"level" binding:"required"`
		ParentID      *string         `json:"parent_id"`
		OrderNum      int             `json:"order_num"`
		Title         string          `json:"title"`
		Content       json.RawMessage `json:"content"`
		TensionTarget float64         `json:"tension_target"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	o, err := h.outlines.Create(c.Request.Context(), c.Param("id"), body.Level, body.ParentID,
		body.OrderNum, body.Title, body.Content, body.TensionTarget)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": o})
}

func (h *Handler) UpdateOutline(c *gin.Context) {
	var body struct {
		Title         string          `json:"title"`
		Content       json.RawMessage `json:"content"`
		TensionTarget float64         `json:"tension_target"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	o, err := h.outlines.Update(c.Request.Context(), c.Param("id"), body.Title, body.Content, body.TensionTarget)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": o})
}

func (h *Handler) DeleteOutline(c *gin.Context) {
	if err := h.outlines.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

func (h *Handler) ListForeshadowings(c *gin.Context) {
	list, err := h.foreshadowings.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": list})
}

func (h *Handler) CreateForeshadowing(c *gin.Context) {
	var body struct {
		Content     string `json:"content" binding:"required"`
		EmbedMethod string `json:"embed_method"`
		Priority    int    `json:"priority"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	f, err := h.foreshadowings.Create(c.Request.Context(), c.Param("id"), body.Content, body.EmbedMethod, body.Priority)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": f})
}

func (h *Handler) UpdateForeshadowingStatus(c *gin.Context) {
	var body struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := h.foreshadowings.UpdateStatus(c.Request.Context(), c.Param("id"), body.Status); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": body.Status})
}

func (h *Handler) DeleteForeshadowing(c *gin.Context) {
	if err := h.foreshadowings.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

// ── Subplot Board ─────────────────────────────────────────────────────────────

func (h *Handler) ListSubplots(c *gin.Context) {
	list, err := h.subplots.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": list})
}

func (h *Handler) CreateSubplot(c *gin.Context) {
	var req services.CreateSubplotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	sp, err := h.subplots.Create(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": sp})
}

func (h *Handler) UpdateSubplot(c *gin.Context) {
	var req services.UpdateSubplotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	sp, err := h.subplots.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": sp})
}

func (h *Handler) DeleteSubplot(c *gin.Context) {
	if err := h.subplots.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) ListSubplotCheckpoints(c *gin.Context) {
	list, err := h.subplots.ListCheckpoints(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": list})
}

func (h *Handler) AddSubplotCheckpoint(c *gin.Context) {
	var req services.CreateCheckpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	cp, err := h.subplots.AddCheckpoint(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": cp})
}

// ── Emotional Arcs ────────────────────────────────────────────────────────────

func (h *Handler) ListEmotionalArcs(c *gin.Context) {
	list, err := h.emotionalArcs.ListForProject(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": list})
}

func (h *Handler) UpsertEmotionalArc(c *gin.Context) {
	var req services.UpsertEmotionalArcRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	entry, err := h.emotionalArcs.Upsert(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": entry})
}

func (h *Handler) DeleteEmotionalArc(c *gin.Context) {
	if err := h.emotionalArcs.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ── Character Interaction Matrix ──────────────────────────────────────────────

func (h *Handler) ListCharacterInteractions(c *gin.Context) {
	list, err := h.characterInteractions.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": list})
}

func (h *Handler) UpsertCharacterInteraction(c *gin.Context) {
	var req services.UpsertInteractionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ci, err := h.characterInteractions.Upsert(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": ci})
}

func (h *Handler) DeleteCharacterInteraction(c *gin.Context) {
	if err := h.characterInteractions.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ── Radar Market Scan ─────────────────────────────────────────────────────────

func (h *Handler) RadarScan(c *gin.Context) {
	projectID := c.Param("id")
	var req services.RadarScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	result, err := h.radar.Scan(c.Request.Context(), &projectID, req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (h *Handler) ListRadarHistory(c *gin.Context) {
	projectID := c.Param("id")
	list, err := h.radar.ListRecent(c.Request.Context(), &projectID, 20)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": list})
}
