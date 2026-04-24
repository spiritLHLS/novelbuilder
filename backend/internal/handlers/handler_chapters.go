package handlers

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/services"
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
	req.ChapterNum = chapterNum
	req.LLMConfig = nil
	payload, _ := json.Marshal(models.ChapterGenerateTaskPayload{Request: req})
	task, err := h.taskQueue.Enqueue(c.Request.Context(), models.CreateTaskRequest{
		ProjectID:   projectID,
		TaskType:    "chapter_generate",
		Payload:     payload,
		Priority:    5,
		MaxAttempts: 1,
	})
	if err != nil {
		h.logger.Error("failed to enqueue chapter generation",
			zap.String("project_id", projectID),
			zap.Int("chapter_num", chapterNum),
			zap.Error(err))
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"status": "queued", "task_id": task.ID, "chapter_num": chapterNum, "message": "章节生成任务已创建，请在任务队列查看进度"})
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
		req.LLMConfig = injectTaskSession(writerCfg, taskSessionID("continue_generate", projectID, &nextNum, "sync"))
	}

	ch, report, genErr := services.GenerateChapterWithQualityRetries(c.Request.Context(), h.chapters, h.quality, projectID, nextNum, req)
	if genErr != nil {
		c.JSON(500, gin.H{"error": genErr.Error()})
		return
	}

	nextAction := "chapter_review"
	qualityGate := gin.H{}
	if report != nil && report.GenerationControl != nil {
		qualityGate = gin.H{
			"passed":             report.Pass,
			"overall_score":      report.OverallScore,
			"attempt_count":      report.GenerationControl.AttemptCount,
			"max_attempts":       report.GenerationControl.MaxAttempts,
			"paused":             report.GenerationControl.Paused,
			"recommended_action": report.GenerationControl.RecommendedAction,
			"last_issues":        report.GenerationControl.LastIssues,
		}
		if report.GenerationControl.Paused {
			nextAction = "manual_revision_required"
		}
	}
	response := gin.H{"data": ch, "next_action": nextAction, "quality_gate": qualityGate}
	respBody, _ := json.Marshal(response)
	h.workflow.SaveIdempotency(c.Request.Context(), idempotencyKey, "chapters/continue", "", 200, respBody)

	c.JSON(201, response)
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

func (h *Handler) UpdateChapter(c *gin.Context) {
	var req models.UpdateChapterContentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	updated, err := h.chapters.UpdateManualContent(c.Request.Context(), c.Param("id"), req.Title, req.Content, req.Version)
	if err != nil {
		if errors.Is(err, workflow.ErrOptimisticLock) {
			c.JSON(409, gin.H{"error": err.Error(), "code": "WF_006", "message": "当前章节版本已过期，请刷新后重试。"})
		} else {
			c.JSON(500, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(200, gin.H{"data": updated})
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
	var req models.GenerateChapterRequest
	_ = c.ShouldBindJSON(&req)

	// Fetch the chapter to get project_id and chapter_num.
	ch, err := h.chapters.Get(c.Request.Context(), chapterID)
	if err != nil || ch == nil {
		c.JSON(404, gin.H{"error": "chapter not found"})
		return
	}
	req.ChapterNum = ch.ChapterNum
	req.LLMConfig = nil
	payload, _ := json.Marshal(models.ChapterRegenerateTaskPayload{ChapterID: chapterID, Request: req})
	task, err := h.taskQueue.Enqueue(c.Request.Context(), models.CreateTaskRequest{
		ProjectID:   ch.ProjectID,
		TaskType:    "chapter_regenerate",
		Payload:     payload,
		Priority:    6,
		MaxAttempts: 1,
	})
	if err != nil {
		h.logger.Error("failed to enqueue chapter regenerate",
			zap.String("project_id", ch.ProjectID),
			zap.String("chapter_id", chapterID),
			zap.Error(err))
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"status": "queued", "task_id": task.ID, "chapter_num": ch.ChapterNum, "message": "章节重生成任务已创建，请在任务队列查看进度"})
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
	Count           int      `json:"count"`             // number of chapters; used when VolumeID is not set
	VolumeID        *string  `json:"volume_id"`         // generate all chapters in this volume (chapter_start … chapter_end)
	OutlineHints    []string `json:"outline_hints"`     // optional per-chapter hints (in order)
	ChapterWordsMin int      `json:"chapter_words_min"` // per-chapter word floor (0 = use default)
	ChapterWordsMax int      `json:"chapter_words_max"` // per-chapter word ceiling (0 = use default 3500)
}

func (h *Handler) BatchGenerateChapters(c *gin.Context) {
	projectID := c.Param("id")
	var req BatchGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Build an ordered list of chapter numbers to generate.
	var chapterNums []int
	outlineHints := map[string]string{}

	if req.VolumeID != nil && *req.VolumeID != "" {
		// Volume-based: generate every chapter in [chapter_start, chapter_end].
		vol, err := h.volumes.Get(c.Request.Context(), *req.VolumeID)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if vol == nil {
			c.JSON(404, gin.H{"error": "volume not found"})
			return
		}
		if vol.ProjectID != projectID {
			c.JSON(403, gin.H{"error": "volume does not belong to this project"})
			return
		}
		if vol.ChapterStart <= 0 || vol.ChapterEnd < vol.ChapterStart {
			c.JSON(400, gin.H{"error": "volume has no valid chapter range; set chapter_start and chapter_end first"})
			return
		}
		for i := vol.ChapterStart; i <= vol.ChapterEnd; i++ {
			chapterNums = append(chapterNums, i)
		}
		for idx, hint := range req.OutlineHints {
			if idx < len(chapterNums) {
				outlineHints[strconv.Itoa(chapterNums[idx])] = hint
			}
		}
	} else {
		// Count-based.
		count := req.Count
		if count <= 0 {
			c.JSON(400, gin.H{"error": "count must be at least 1"})
			return
		}
		if count > 200 {
			c.JSON(400, gin.H{"error": "count must not exceed 200"})
			return
		}
		// Determine starting chapter number.
		lastNum, err := h.chapters.MaxChapterNum(c.Request.Context(), projectID)
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to determine next chapter number"})
			return
		}
		start := lastNum + 1
		for i := 0; i < count; i++ {
			chapterNums = append(chapterNums, start+i)
		}
		for idx, hint := range req.OutlineHints {
			if idx < len(chapterNums) {
				outlineHints[strconv.Itoa(chapterNums[idx])] = hint
			}
		}
	}

	if len(chapterNums) == 0 {
		c.JSON(400, gin.H{"error": "no chapters to generate"})
		return
	}
	if len(chapterNums) > 1 && h.workflow.IsStrictReview(c.Request.Context(), projectID) {
		c.JSON(409, gin.H{
			"code":    "WF_007",
			"error":   workflow.ErrStrictReviewRequired.Error(),
			"message": "严格审核模式下暂不支持批量生成多章，请逐章生成或先关闭严格审核",
		})
		return
	}

	tasks := make([]models.CreateTaskRequest, 0, len(chapterNums))
	for _, chapterNum := range chapterNums {
		payloadReq := models.GenerateChapterRequest{
			ChapterNum:      chapterNum,
			ChapterWordsMin: req.ChapterWordsMin,
			ChapterWordsMax: req.ChapterWordsMax,
			ContextHint:     outlineHints[strconv.Itoa(chapterNum)],
		}
		payload, _ := json.Marshal(models.ChapterGenerateTaskPayload{Request: payloadReq})
		tasks = append(tasks, models.CreateTaskRequest{
			ProjectID:   projectID,
			TaskType:    "chapter_generate",
			Payload:     payload,
			Priority:    5,
			MaxAttempts: 1,
		})
	}
	taskIDs, err := h.taskQueue.EnqueueBatch(c.Request.Context(), tasks)
	if err != nil {
		h.logger.Error("failed to enqueue batch chapter generation",
			zap.String("project_id", projectID),
			zap.Error(err))
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"status": "queued", "task_ids": taskIDs, "total": len(chapterNums), "chapter_nums": chapterNums, "message": "批量生成任务已创建，请在任务队列查看进度"})
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
