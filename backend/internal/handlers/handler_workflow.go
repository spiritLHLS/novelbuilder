package handlers

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/workflow"
	"go.uber.org/zap"
)

func (h *Handler) StartWorkflow(c *gin.Context) {
	projectID := c.Param("id")
	runID, err := h.workflow.CreateRun(c.Request.Context(), projectID, true)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// Create initial workflow steps and sync with current blueprint state.
	if initErr := h.workflow.InitRunSteps(c.Request.Context(), runID, projectID); initErr != nil {
		h.logger.Warn("StartWorkflow: failed to init run steps",
			zap.String("run_id", runID), zap.Error(initErr))
	}
	c.JSON(201, gin.H{"data": gin.H{"run_id": runID}})
}

func (h *Handler) GetWorkflowHistory(c *gin.Context) {
	history, err := h.workflow.GetRunHistory(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": history})
}

func (h *Handler) WorkflowRollback(c *gin.Context) {
	var req models.RollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	affected, err := h.workflow.Rollback(c.Request.Context(), c.Param("id"), req.TargetStepID, req.Reason)
	if err != nil {
		if err == workflow.ErrSnapshotNotFound {
			c.JSON(404, gin.H{"error": err.Error(), "code": "WF_005", "message": "未找到可回退快照。"})
			return
		}
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "rolled_back", "marked_as_needs_recheck": affected})
}

// GetWorkflowDiff returns two workflow snapshots for comparison.
// Query params: fromStep (step_key) and toStep (step_key).
func (h *Handler) GetWorkflowDiff(c *gin.Context) {
	runID := c.Param("id")
	fromStep := c.Query("fromStep")
	toStep := c.Query("toStep")
	if fromStep == "" || toStep == "" {
		c.JSON(400, gin.H{"error": "fromStep and toStep query params are required"})
		return
	}

	from, err := h.workflow.GetSnapshot(c.Request.Context(), runID, fromStep)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if from == nil {
		c.JSON(404, gin.H{"error": fmt.Sprintf("snapshot not found for step '%s'", fromStep)})
		return
	}
	to, err := h.workflow.GetSnapshot(c.Request.Context(), runID, toStep)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if to == nil {
		c.JSON(404, gin.H{"error": fmt.Sprintf("snapshot not found for step '%s'", toStep)})
		return
	}

	c.JSON(200, gin.H{"data": gin.H{"from": from, "to": to}})
}

// ── Change Propagation Handlers ───────────────────────────────────────────────

func (h *Handler) CreateChangeEvent(c *gin.Context) {
	projectID := c.Param("id")
	var req models.CreateChangeEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	plan, err := h.propagation.CreateChangeEventWithAnalysis(c.Request.Context(), projectID, req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": plan})
}

func (h *Handler) ListChangeEvents(c *gin.Context) {
	projectID := c.Param("id")
	events, err := h.propagation.ListChangeEvents(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": events})
}

func (h *Handler) GetPatchPlan(c *gin.Context) {
	planID := c.Param("id")
	if _, err := uuid.Parse(planID); err != nil {
		c.JSON(400, gin.H{"error": "invalid plan id"})
		return
	}
	plan, err := h.propagation.GetPlan(c.Request.Context(), planID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": plan})
}

func (h *Handler) UpdatePatchItemStatus(c *gin.Context) {
	itemID := c.Param("id")
	if _, err := uuid.Parse(itemID); err != nil {
		c.JSON(400, gin.H{"error": "invalid item id"})
		return
	}
	var req models.UpdatePatchItemStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	allowed := map[string]bool{"approved": true, "skipped": true, "pending": true}
	if !allowed[req.Status] {
		c.JSON(400, gin.H{"error": "status must be approved, skipped, or pending"})
		return
	}
	if err := h.propagation.UpdatePatchItemStatus(c.Request.Context(), itemID, req.Status); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) ExecutePatchItem(c *gin.Context) {
	itemID := c.Param("id")
	if _, err := uuid.Parse(itemID); err != nil {
		c.JSON(400, gin.H{"error": "invalid item id"})
		return
	}
	if err := h.propagation.ExecutePatchItem(c.Request.Context(), itemID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
