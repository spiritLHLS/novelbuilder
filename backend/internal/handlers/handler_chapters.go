package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/workflow"
	"go.uber.org/zap"
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

	// Resolve writer-agent LLM config (falls back to default if no route set)
	if writerCfg, err := h.resolveAgentLLMConfig(c.Request.Context(), "writer", c.Param("id")); err == nil {
		req.LLMConfig = writerCfg
	}

	// chapter_num: prefer JSON body field, fall back to query param
	chapterNum := req.ChapterNum
	if chapterNum == 0 {
		if n, err := strconv.Atoi(c.Query("chapter_num")); err == nil {
			chapterNum = n
		}
	}
	if chapterNum == 0 {
		chapterNum = 1
	}

	ch, err := h.chapters.Generate(c.Request.Context(), c.Param("id"), chapterNum, req)
	if err != nil {
		errStr := err.Error()
		if containsStr(errStr, "WF_001") {
			c.JSON(409, gin.H{"error": errStr, "code": "WF_001", "message": "请先通过整书资产包审核后再生成章节。"})
			return
		}
		if containsStr(errStr, "WF_002") {
			c.JSON(409, gin.H{"error": errStr, "code": "WF_002", "message": "上一章尚未审核通过，暂不能继续。"})
			return
		}
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": ch})
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

func (h *Handler) StreamChapter(c *gin.Context) {
	projectID := c.Param("id")

	var req models.GenerateChapterRequest
	c.ShouldBindJSON(&req)

	if writerCfg, wErr := h.resolveAgentLLMConfig(c.Request.Context(), "writer", projectID); wErr == nil {
		req.LLMConfig = writerCfg
	}

	// chapter_num: prefer JSON body field, fall back to query param
	chapterNum := req.ChapterNum
	if chapterNum == 0 {
		if n, err := strconv.Atoi(c.Query("chapter_num")); err == nil {
			chapterNum = n
		}
	}
	if chapterNum == 0 {
		chapterNum = 1
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(500, gin.H{"error": "streaming not supported"})
		return
	}

	err := h.chapters.StreamGenerate(c.Request.Context(), projectID, chapterNum, req, func(chunk gateway.StreamChunk) {
		if chunk.Done {
			fmt.Fprintf(c.Writer, "data: {\"done\": true}\n\n")
		} else {
			data, _ := json.Marshal(map[string]string{"content": chunk.Content})
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		}
		flusher.Flush()
	})
	if err != nil {
		fmt.Fprintf(c.Writer, "data: {\"error\": %q}\n\n", err.Error())
		flusher.Flush()
	}
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
	var req models.GenerateChapterRequest
	c.ShouldBindJSON(&req)
	// Resolve writer-agent config for regeneration
	if chID := c.Param("id"); chID != "" {
		if ch, err := h.chapters.Get(c.Request.Context(), chID); err == nil && ch != nil {
			if writerCfg, wErr := h.resolveAgentLLMConfig(c.Request.Context(), "writer", ch.ProjectID); wErr == nil {
				req.LLMConfig = writerCfg
			}
		}
	}
	ch, err := h.chapters.Regenerate(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": ch})
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

	llmCfg, err := h.resolveLLMConfig(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Run in background so HTTP returns immediately
	go func() {
		if err := h.imports.Process(context.Background(), importID, llmCfg); err != nil {
			h.logger.Error("import process failed", zap.String("import_id", importID), zap.Error(err))
		}
	}()

	c.JSON(202, gin.H{"status": "processing", "import_id": importID})
}
