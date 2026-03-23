package handlers

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/workflow"
)

func (h *Handler) ListVolumes(c *gin.Context) {
	vols, err := h.volumes.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": vols})
}

func (h *Handler) SubmitVolumeReview(c *gin.Context) {
	if err := h.volumes.SubmitReview(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "pending_review"})
}

func (h *Handler) ApproveVolume(c *gin.Context) {
	var req models.ReviewRequest
	c.ShouldBindJSON(&req)
	if err := h.volumes.Approve(c.Request.Context(), c.Param("id"), req.ReviewComment); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "approved"})
}

func (h *Handler) RejectVolume(c *gin.Context) {
	var req models.ReviewRequest
	c.ShouldBindJSON(&req)
	if err := h.volumes.Reject(c.Request.Context(), c.Param("id"), req.ReviewComment); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "rejected"})
}

func (h *Handler) ListChapters(c *gin.Context) {
	chapters, err := h.chapters.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": chapters})
}

func (h *Handler) GenerateChapter(c *gin.Context) {
	var req models.GenerateChapterRequest
	c.ShouldBindJSON(&req)

	projectID := c.Param("id")

	// Check workflow gates synchronously for immediate user feedback.
	if err := h.workflow.CanGenerateNextChapter(c.Request.Context(), projectID); err != nil {
		code := "WF_000"
		msg := err.Error()
		switch err {
		case workflow.ErrBlueprintNotApproved:
			code, msg = "WF_001", "请先通过整书资产包审核后再生成章节。"
		case workflow.ErrPrevChapterNotApproved:
			code, msg = "WF_002", "上一章尚未审核通过，暂不能继续。"
		case workflow.ErrVolumeGateClosed:
			code, msg = "WF_003", "当前卷尚未通过卷级审核。"
		}
		c.JSON(409, gin.H{"error": err.Error(), "code": code, "message": msg})
		return
	}

	// chapter_num: prefer JSON body field, fall back to query param.
	chapterNum := req.ChapterNum
	if chapterNum == 0 {
		if n, err := strconv.Atoi(c.Query("chapter_num")); err == nil {
			chapterNum = n
		}
	}
	if chapterNum == 0 {
		chapterNum = 1
	}

	// Store only safe, serialisable fields - never LLM credentials.
	payloadBytes, _ := json.Marshal(map[string]any{
		"chapter_num":       chapterNum,
		"chapter_words_min": req.ChapterWordsMin,
		"chapter_words_max": req.ChapterWordsMax,
		"context_hint":      req.ContextHint,
	})

	task, err := h.taskQueue.Enqueue(c.Request.Context(), models.CreateTaskRequest{
		ProjectID: projectID,
		TaskType:  "chapter_generate",
		Payload:   payloadBytes,
		Priority:  5,
	})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"task_id": task.ID, "message": "章节生成任务已加入队列"})
}

func (h *Handler) ContinueGenerate(c *gin.Context) {
	projectID := c.Param("id")

	idempotencyKey := c.GetHeader("Idempotency-Key")
	if idempotencyKey == "" {
		idempotencyKey = uuid.New().String()
	}

	exists, body, err := h.workflow.CheckIdempotency(c.Request.Context(), idempotencyKey, "chapters/continue")
	if err == nil && exists {
		c.Data(200, "application/json", body)
		return
	}

	if err := h.workflow.CanGenerateNextChapter(c.Request.Context(), projectID); err != nil {
		code := "WF_000"
		msg := err.Error()
		switch err {
		case workflow.ErrBlueprintNotApproved:
			code = "WF_001"
			msg = "请先通过整书资产包审核后再生成章节。"
		case workflow.ErrPrevChapterNotApproved:
			code = "WF_002"
			msg = "上一章尚未审核通过，暂不能继续。"
		case workflow.ErrVolumeGateClosed:
			code = "WF_003"
			msg = "当前卷尚未通过卷级审核。"
		}
		c.JSON(409, gin.H{"error": err.Error(), "code": code, "message": msg})
		return
	}

	lastNum, err := h.chapters.MaxChapterNum(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to determine next chapter number"})
		return
	}
	nextNum := lastNum + 1

	var req models.GenerateChapterRequest
	c.ShouldBindJSON(&req)

	if writerCfg, wErr := h.resolveAgentLLMConfig(c.Request.Context(), "writer", projectID); wErr == nil {
		req.LLMConfig = writerCfg
	}

	ch, genErr := h.chapters.Generate(c.Request.Context(), projectID, nextNum, req)
	if genErr != nil {
		c.JSON(500, gin.H{"error": genErr.Error()})
		return
	}

	respBody, _ := json.Marshal(gin.H{"data": ch, "next_action": "chapter_review"})
	h.workflow.SaveIdempotency(c.Request.Context(), idempotencyKey, "chapters/continue", "", 200, respBody)

	c.JSON(201, gin.H{"data": ch, "next_action": "chapter_review"})
}

func (h *Handler) GetChapter(c *gin.Context) {
	ch, err := h.chapters.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if ch == nil {
		c.JSON(404, gin.H{"error": "chapter not found"})
		return
	}
	c.JSON(200, gin.H{"data": ch})
}

func (h *Handler) DeleteChapter(c *gin.Context) {
	err := h.chapters.Delete(c.Request.Context(), c.Param("id"))
	if err != nil && strings.Contains(err.Error(), "only the latest chapter can be deleted") {
		c.JSON(409, gin.H{"error": err.Error(), "code": "CH_001", "message": "为了避免打乱后续章节与依赖关系，目前只允许删除最后一章。"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "deleted"})
}

func (h *Handler) SubmitChapterReview(c *gin.Context) {
	if err := h.chapters.SubmitReview(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "pending_review"})
}

func (h *Handler) ApproveChapter(c *gin.Context) {
	var req struct {
		ReviewComment string `json:"review_comment"`
		Version       int    `json:"version"`
	}
	c.ShouldBindJSON(&req)
	if err := h.chapters.Approve(c.Request.Context(), c.Param("id"), req.ReviewComment, req.Version); err != nil {
		if errors.Is(err, workflow.ErrOptimisticLock) {
			c.JSON(409, gin.H{"error": err.Error(), "code": "WF_006", "message": "当前页面版本已过期。"})
		} else {
			c.JSON(500, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(200, gin.H{"status": "approved", "next_action": "chapter_continue_available"})
}

func (h *Handler) RejectChapter(c *gin.Context) {
	var req struct {
		ReviewComment string `json:"review_comment"`
		Version       int    `json:"version"`
	}
	c.ShouldBindJSON(&req)
	if err := h.chapters.Reject(c.Request.Context(), c.Param("id"), req.ReviewComment, req.Version); err != nil {
		if errors.Is(err, workflow.ErrOptimisticLock) {
			c.JSON(409, gin.H{"error": err.Error(), "code": "WF_006", "message": "当前页面版本已过期。"})
		} else {
			c.JSON(500, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(200, gin.H{"status": "rejected"})
}

func (h *Handler) RegenerateChapter(c *gin.Context) {
	chapterID := c.Param("id")

	// Fetch the chapter to get its project_id for task routing.
	ch, err := h.chapters.Get(c.Request.Context(), chapterID)
	if err != nil || ch == nil {
		c.JSON(404, gin.H{"error": "chapter not found"})
		return
	}

	payloadBytes, _ := json.Marshal(map[string]any{
		"chapter_id": chapterID,
	})

	task, err := h.taskQueue.Enqueue(c.Request.Context(), models.CreateTaskRequest{
		ProjectID: ch.ProjectID,
		TaskType:  "chapter_regenerate",
		Payload:   payloadBytes,
		Priority:  5,
	})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"task_id": task.ID, "message": "重新生成任务已加入队列"})
}

func (h *Handler) QualityCheck(c *gin.Context) {
	report, err := h.quality.RunFullCheck(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": report})
}

// BatchGenerateRequest is the body for POST /projects/:id/chapters/batch-generate.
type BatchGenerateRequest struct {
	Count int `json:"count" binding:"required,min=1,max=50"`
}

func (h *Handler) BatchGenerateChapters(c *gin.Context) {
	projectID := c.Param("id")
	var req BatchGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	reqs := make([]models.CreateTaskRequest, req.Count)
	for i := range reqs {
		reqs[i] = models.CreateTaskRequest{
			ProjectID: projectID,
			TaskType:  "generate_next_chapter",
			Payload:   json.RawMessage(`{}`),
			Priority:  5,
		}
	}
	ids, err := h.taskQueue.EnqueueBatch(c.Request.Context(), reqs)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"count": req.Count, "task_ids": ids}})
}

func (h *Handler) CreateChapterImport(c *gin.Context) {
	projectID := c.Param("id")
	var req models.CreateImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	imp, err := h.imports.Create(c.Request.Context(), projectID, req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": imp})
}

func (h *Handler) ListChapterImports(c *gin.Context) {
	imports, err := h.imports.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": imports})
}

func (h *Handler) GetChapterImport(c *gin.Context) {
	imp, err := h.imports.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(404, gin.H{"error": "import not found"})
		return
	}
	c.JSON(200, gin.H{"data": imp})
}

func (h *Handler) ProcessChapterImport(c *gin.Context) {
	importID := c.Param("id")

	// Look up the import to get the project_id for task association.
	imp, err := h.imports.Get(c.Request.Context(), importID)
	if err != nil || imp == nil {
		c.JSON(404, gin.H{"error": "import not found"})
		return
	}

	// Enqueue as a tracked background task. LLM credentials are resolved at
	// execution time inside the handler — never stored in the task payload.
	payloadBytes, _ := json.Marshal(map[string]any{
		"import_id": importID,
	})
	task, err := h.taskQueue.Enqueue(c.Request.Context(), models.CreateTaskRequest{
		ProjectID: imp.ProjectID,
		TaskType:  "chapter_import_process",
		Payload:   payloadBytes,
		Priority:  5,
	})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"status": "processing", "import_id": importID, "task_id": task.ID})
}
