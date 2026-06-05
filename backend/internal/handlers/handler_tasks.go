package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/services"
)

// ── Task Queue ────────────────────────────────────────────────────────────────

func (h *Handler) ListTasks(c *gin.Context) {
	params := taskListParamsFromRequest(c)

	tasks, total, err := h.taskQueue.List(c.Request.Context(), params)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"data":       tasks,
		"pagination": taskPagination(total, params.PageSize, params.Page),
	})
}

func taskListParamsFromRequest(c *gin.Context) services.TaskListParams {
	page := 1
	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	pageSize := 10
	if ps := c.Query("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}

	return services.TaskListParams{
		ProjectID: c.Param("id"),
		Status:    c.Query("status"),
		TaskType:  c.Query("type"),
		Page:      page,
		PageSize:  pageSize,
	}
}

func taskPagination(total, pageSize, page int) gin.H {
	totalPages := 0
	if pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	return gin.H{
		"page":        page,
		"page_size":   pageSize,
		"total":       total,
		"total_pages": totalPages,
	}
}

func (h *Handler) taskSnapshot(ctx context.Context, params services.TaskListParams) (gin.H, error) {
	tasks, total, err := h.taskQueue.List(ctx, params)
	if err != nil {
		return nil, err
	}
	stats, err := h.taskQueue.Stats(ctx, params.ProjectID)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"data":       tasks,
		"pagination": taskPagination(total, params.PageSize, params.Page),
		"stats":      stats,
		"sent_at":    time.Now().UTC(),
	}, nil
}

func (h *Handler) StreamTasks(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(500, gin.H{"error": "streaming unsupported"})
		return
	}

	params := taskListParamsFromRequest(c)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sendSnapshot := func() bool {
		payload, err := h.taskSnapshot(c.Request.Context(), params)
		eventName := "snapshot"
		if err != nil {
			eventName = "task_error"
			payload = gin.H{"error": err.Error(), "sent_at": time.Now().UTC()}
		}
		b, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			b, _ = json.Marshal(gin.H{"error": marshalErr.Error(), "sent_at": time.Now().UTC()})
			eventName = "task_error"
		}
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", eventName, b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !sendSnapshot() {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if !sendSnapshot() {
				return
			}
		}
	}
}

func (h *Handler) GetTaskStats(c *gin.Context) {
	stats, err := h.taskQueue.Stats(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": stats})
}

func (h *Handler) EnqueueTask(c *gin.Context) {
	var req models.CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	task, err := h.taskQueue.Enqueue(c.Request.Context(), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": task})
}

func (h *Handler) GetTask(c *gin.Context) {
	task, err := h.taskQueue.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(404, gin.H{"error": "task not found"})
		return
	}
	h.populateTaskPromptPreview(c.Request.Context(), task)
	c.JSON(200, gin.H{"data": task})
}

func (h *Handler) populateTaskPromptPreview(ctx context.Context, task *models.TaskQueueItem) {
	if task == nil || task.ProjectID == nil || *task.ProjectID == "" {
		return
	}
	projectID := *task.ProjectID

	switch task.TaskType {
	case "chapter_generate":
		var payload models.ChapterGenerateTaskPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return
		}
		chapterNum := payload.Request.ChapterNum
		if chapterNum <= 0 {
			nextNum, err := h.chapters.NextChapterNum(ctx, projectID)
			if err != nil {
				return
			}
			chapterNum = nextNum
		}
		if preview, err := h.chapters.BuildChapterPromptPreview(ctx, projectID, chapterNum, payload.Request); err == nil {
			task.PromptPreview = preview
		}
	case "generate_next_chapter":
		var payload models.GenerateNextChapterTaskPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return
		}
		chapterNum, err := h.chapters.NextChapterNum(ctx, projectID)
		if err != nil {
			return
		}
		if preview, err := h.chapters.BuildChapterPromptPreview(ctx, projectID, chapterNum, payload.Request); err == nil {
			task.PromptPreview = preview
		}
	case "chapter_regenerate":
		var payload models.ChapterRegenerateTaskPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return
		}
		chapterNum := payload.Request.ChapterNum
		if payload.ChapterID != "" {
			var chapterProjectID string
			var storedChapterNum int
			if err := h.projects.DB().QueryRow(ctx,
				`SELECT project_id, chapter_num FROM chapters WHERE id=$1`, payload.ChapterID).
				Scan(&chapterProjectID, &storedChapterNum); err == nil && chapterProjectID == projectID {
				chapterNum = storedChapterNum
			}
		}
		if chapterNum <= 0 {
			return
		}
		if preview, err := h.chapters.BuildChapterPromptPreview(ctx, projectID, chapterNum, payload.Request); err == nil {
			task.PromptPreview = preview
		}
	}
}

func (h *Handler) CancelTask(c *gin.Context) {
	taskID := c.Param("id")
	if err := h.taskQueue.Cancel(c.Request.Context(), taskID); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	h.syncTaskArtifactState(c.Request.Context(), taskID, "cancelled") //nolint
	c.JSON(200, gin.H{"message": "task cancelled"})
}

func (h *Handler) PauseTask(c *gin.Context) {
	taskID := c.Param("id")
	if err := h.taskQueue.Pause(c.Request.Context(), taskID); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	h.syncTaskArtifactState(c.Request.Context(), taskID, "paused") //nolint
	c.JSON(200, gin.H{"message": "task paused"})
}

func (h *Handler) ResumeTask(c *gin.Context) {
	taskID := c.Param("id")
	if err := h.taskQueue.Resume(c.Request.Context(), taskID); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	h.syncTaskArtifactState(c.Request.Context(), taskID, "running") //nolint
	c.JSON(200, gin.H{"message": "task resumed"})
}

func (h *Handler) RetryTask(c *gin.Context) {
	taskID := c.Param("id")
	if err := h.taskQueue.Retry(c.Request.Context(), taskID); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	h.syncTaskArtifactState(c.Request.Context(), taskID, "running") //nolint
	c.JSON(200, gin.H{"message": "task queued for retry"})
}

func (h *Handler) UpdateTaskPayload(c *gin.Context) {
	var req struct {
		Payload json.RawMessage `json:"payload" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if !json.Valid(req.Payload) {
		c.JSON(400, gin.H{"error": "payload must be valid JSON"})
		return
	}
	task, err := h.taskQueue.UpdatePayload(c.Request.Context(), c.Param("id"), req.Payload)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(404, gin.H{"error": "task not found"})
		return
	}
	h.populateTaskPromptPreview(c.Request.Context(), task)
	c.JSON(200, gin.H{"data": task})
}

func (h *Handler) syncTaskArtifactState(ctx context.Context, taskID, state string) {
	task, err := h.taskQueue.Get(ctx, taskID)
	if err != nil || task == nil {
		return
	}
	switch task.TaskType {
	case "blueprint_generate":
		var payload models.BlueprintGenerateTaskPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.BlueprintID == "" {
			return
		}
		switch state {
		case "paused":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE book_blueprints SET status='paused', error_message='', updated_at=NOW() WHERE id=$1 AND status='generating'`,
				payload.BlueprintID)
		case "running":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE book_blueprints SET status='generating', error_message='', updated_at=NOW() WHERE id=$1 AND status IN ('paused','failed')`,
				payload.BlueprintID)
		case "cancelled":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE book_blueprints SET status='failed', error_message='cancel requested', updated_at=NOW() WHERE id=$1 AND status IN ('generating','paused')`,
				payload.BlueprintID)
		}
	case "reference_fetch_import":
		var payload struct {
			RefID string `json:"ref_id"`
		}
		if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.RefID == "" {
			return
		}
		switch state {
		case "paused":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE reference_materials SET fetch_status='paused', fetch_error='' WHERE id=$1 AND fetch_status='downloading'`,
				payload.RefID)
		case "running":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE reference_materials SET fetch_status='downloading', fetch_error='' WHERE id=$1 AND fetch_status IN ('paused','failed')`,
				payload.RefID)
		case "cancelled":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE reference_materials SET fetch_status='failed', fetch_error='cancel requested' WHERE id=$1 AND fetch_status IN ('downloading','paused')`,
				payload.RefID)
		}
	case "reference_analyze":
		var payload struct {
			RefID string `json:"ref_id"`
		}
		if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.RefID == "" {
			return
		}
		switch state {
		case "paused":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE reference_materials SET status='paused' WHERE id=$1 AND status='analyzing'`,
				payload.RefID)
		case "running":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE reference_materials SET status='analyzing' WHERE id=$1 AND status IN ('paused','failed')`,
				payload.RefID)
		case "cancelled":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE reference_materials SET status='failed' WHERE id=$1 AND status IN ('analyzing','paused')`,
				payload.RefID)
		}
	case "reference_analysis":
		var payload struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.JobID == "" {
			return
		}
		switch state {
		case "paused":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE reference_analysis_jobs SET status='paused', updated_at=NOW() WHERE id=$1 AND status IN ('pending','running')`,
				payload.JobID)
		case "running":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE reference_analysis_jobs SET status='pending', error_message='', updated_at=NOW() WHERE id=$1 AND status IN ('paused','failed','cancelled')`,
				payload.JobID)
		case "cancelled":
			_, _ = h.projects.DB().Exec(ctx,
				`UPDATE reference_analysis_jobs SET status='cancelled', updated_at=NOW() WHERE id=$1 AND status IN ('pending','running','paused')`,
				payload.JobID)
		}
	}
}

func (h *Handler) latestTaskByType(ctx context.Context, projectID, taskType string) (*models.TaskQueueItem, error) {
	var t models.TaskQueueItem
	err := h.projects.DB().QueryRow(ctx,
		`SELECT id, project_id, task_type, payload, status, priority, attempts, max_attempts,
		        error_message, scheduled_at, started_at, completed_at, created_at, updated_at
		 FROM task_queue
		 WHERE project_id = $1 AND task_type = $2
		 ORDER BY created_at DESC
		 LIMIT 1`,
		projectID, taskType).Scan(
		&t.ID, &t.ProjectID, &t.TaskType, &t.Payload, &t.Status, &t.Priority,
		&t.Attempts, &t.MaxAttempts, &t.ErrorMessage, &t.ScheduledAt,
		&t.StartedAt, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
