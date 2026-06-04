package handlers

import (
	"encoding/json"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/models"
)

// ragRebuildState holds the mutable state of a single RAG rebuild job.
// Access is guarded by mu so goroutine writes are safely visible to HTTP reads.
type ragRebuildState struct {
	mu      sync.Mutex
	status  string // "running" | "completed" | "failed"
	rebuilt int
	errMsg  string
}

func (s *ragRebuildState) markDone(rebuilt int) {
	s.mu.Lock()
	s.status = "completed"
	s.rebuilt = rebuilt
	s.mu.Unlock()
}

func (s *ragRebuildState) markFailed(msg string) {
	s.mu.Lock()
	s.status = "failed"
	s.errMsg = msg
	s.mu.Unlock()
}

func (s *ragRebuildState) snapshot() (status string, rebuilt int, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.rebuilt, s.errMsg
}

// RebuildRAG enqueues a tracked task to re-index all project vectors and
// returns 202 immediately. The frontend should poll GET /projects/:id/rag/rebuild-status.
func (h *Handler) RebuildRAG(c *gin.Context) {
	projectID := c.Param("id")
	task, err := h.taskQueue.Enqueue(c.Request.Context(), models.CreateTaskRequest{
		ProjectID:   projectID,
		TaskType:    "rag_rebuild",
		Payload:     json.RawMessage(`{}`),
		Priority:    4,
		MaxAttempts: 1,
	})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"status": "running", "project_id": projectID, "task_id": task.ID})
}

// GetRebuildRAGStatus returns the current state of the most recent rebuild job.
func (h *Handler) GetRebuildRAGStatus(c *gin.Context) {
	projectID := c.Param("id")
	task, err := h.latestTaskByType(c.Request.Context(), projectID, "rag_rebuild")
	if err == nil && task != nil {
		status := task.Status
		if status == "done" {
			status = "completed"
		}
		c.JSON(200, gin.H{
			"status":          status,
			"rebuilt_sources": 0,
			"error":           task.ErrorMessage,
			"task_id":         task.ID,
		})
		return
	}
	c.JSON(200, gin.H{"status": "idle"})
}

func (h *Handler) GetRAGStatus(c *gin.Context) {
	projectID := c.Param("id")
	if h.rag == nil {
		c.JSON(200, gin.H{"project_id": projectID, "collections": []interface{}{}, "total_chunks": 0})
		return
	}
	stats, err := h.rag.GetProjectStats(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	total := 0
	for _, s := range stats {
		total += s.Count
	}
	c.JSON(200, gin.H{
		"project_id":   projectID,
		"collections":  stats,
		"total_chunks": total,
	})
}

// StartDeepAnalysis enqueues a chunked background analysis job for a reference novel.
// Response is 202 Accepted with job details; poll GetDeepAnalysisJob for progress.
func (h *Handler) StartDeepAnalysis(c *gin.Context) {
	refID := c.Param("id")
	ref, err := h.references.Get(c.Request.Context(), refID)
	if err != nil || ref == nil {
		c.JSON(404, gin.H{"error": "reference not found"})
		return
	}
	if h.deepAnalysis == nil {
		c.JSON(503, gin.H{"error": "deep analysis service not configured"})
		return
	}
	job, err := h.deepAnalysis.StartDeepAnalysis(c.Request.Context(), refID, ref.ProjectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"data": job})
}

// GetDeepAnalysisJob returns the progress/result of a deep analysis job by ref_id.
func (h *Handler) GetDeepAnalysisJob(c *gin.Context) {
	refID := c.Param("id")
	if h.deepAnalysis == nil {
		c.JSON(200, gin.H{"data": nil})
		return
	}
	job, err := h.deepAnalysis.GetJobByRef(c.Request.Context(), refID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": job})
}

// CancelDeepAnalysisJob cancels a pending or running deep analysis job.
func (h *Handler) CancelDeepAnalysisJob(c *gin.Context) {
	refID := c.Param("id")
	if h.deepAnalysis == nil {
		c.JSON(503, gin.H{"error": "deep analysis service not configured"})
		return
	}
	job, err := h.deepAnalysis.GetJobByRef(c.Request.Context(), refID)
	if err != nil || job == nil {
		c.JSON(404, gin.H{"error": "no analysis job found for this reference"})
		return
	}
	if err := h.deepAnalysis.CancelJob(c.Request.Context(), job.ID); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "cancelled", "job_id": job.ID})
}

// ResetDeepAnalysis cancels any running job and deletes all prior analysis records
// for a reference so the next StartDeepAnalysis call begins completely from scratch.
func (h *Handler) ResetDeepAnalysis(c *gin.Context) {
	refID := c.Param("id")
	if h.deepAnalysis == nil {
		c.JSON(503, gin.H{"error": "deep analysis service not configured"})
		return
	}
	if err := h.deepAnalysis.ResetAnalysis(c.Request.Context(), refID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "reset", "ref_id": refID})
}

// ImportDeepAnalysisResult imports the extracted entities from a completed deep analysis job
// into the current project's world_bibles, characters, and outlines tables.
func (h *Handler) ImportDeepAnalysisResult(c *gin.Context) {
	refID := c.Param("id")
	ref, err := h.references.Get(c.Request.Context(), refID)
	if err != nil || ref == nil {
		c.JSON(404, gin.H{"error": "reference not found"})
		return
	}
	if h.deepAnalysis == nil {
		c.JSON(503, gin.H{"error": "deep analysis service not configured"})
		return
	}
	job, err := h.deepAnalysis.GetJobByRef(c.Request.Context(), refID)
	if err != nil || job == nil {
		c.JSON(404, gin.H{"error": "no analysis job found for this reference"})
		return
	}
	if job.Status != "completed" {
		c.JSON(400, gin.H{"error": "analysis is not completed yet", "status": job.Status})
		return
	}
	if err := h.deepAnalysis.ImportResult(c.Request.Context(), job.ID, ref.ProjectID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "imported", "job_id": job.ID, "project_id": ref.ProjectID})
}
