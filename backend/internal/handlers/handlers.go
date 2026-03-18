package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/services"
	"github.com/novelbuilder/backend/internal/workflow"
	"go.uber.org/zap"
)

type Handler struct {
	projects       *services.ProjectService
	blueprints     *services.BlueprintService
	chapters       *services.ChapterService
	worldBibles    *services.WorldBibleService
	characters     *services.CharacterService
	outlines       *services.OutlineService
	foreshadowings *services.ForeshadowingService
	volumes        *services.VolumeService
	quality        *services.QualityService
	references     *services.ReferenceService
	rag            *services.RAGService
	workflow       *workflow.Engine
	agentReview    *services.AgentReviewService
	export         *services.ExportService
	llmProfiles    *services.LLMProfileService
	propagation    *services.EditPropagationService
	promptPresets  *services.PromptPresetService
	glossary       *services.GlossaryService
	taskQueue      *services.TaskQueueService
	resourceLedger *services.ResourceLedgerService
	webhooks       *services.WebhookService
	sidecar        *services.SidecarService
	systemSettings *services.SystemSettingsService
	logger         *zap.Logger
}

func NewHandler(
	projects *services.ProjectService,
	blueprints *services.BlueprintService,
	chapters *services.ChapterService,
	worldBibles *services.WorldBibleService,
	characters *services.CharacterService,
	outlines *services.OutlineService,
	foreshadowings *services.ForeshadowingService,
	volumes *services.VolumeService,
	quality *services.QualityService,
	references *services.ReferenceService,
	rag *services.RAGService,
	wf *workflow.Engine,
	agentReview *services.AgentReviewService,
	export *services.ExportService,
	llmProfiles *services.LLMProfileService,
	propagation *services.EditPropagationService,
	promptPresets *services.PromptPresetService,
	glossary *services.GlossaryService,
	taskQueue *services.TaskQueueService,
	resourceLedger *services.ResourceLedgerService,
	webhooks *services.WebhookService,
	sidecar *services.SidecarService,
	systemSettings *services.SystemSettingsService,
	logger *zap.Logger,
) *Handler {
	return &Handler{
		projects:       projects,
		blueprints:     blueprints,
		chapters:       chapters,
		worldBibles:    worldBibles,
		characters:     characters,
		outlines:       outlines,
		foreshadowings: foreshadowings,
		volumes:        volumes,
		quality:        quality,
		references:     references,
		rag:            rag,
		workflow:       wf,
		agentReview:    agentReview,
		export:         export,
		llmProfiles:    llmProfiles,
		propagation:    propagation,
		promptPresets:  promptPresets,
		glossary:       glossary,
		taskQueue:      taskQueue,
		resourceLedger: resourceLedger,
		webhooks:       webhooks,
		sidecar:        sidecar,
		systemSettings: systemSettings,
		logger:         logger,
	}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api")

	api.GET("/projects", h.ListProjects)
	api.POST("/projects", h.CreateProject)
	api.GET("/projects/:id", h.GetProject)
	api.PUT("/projects/:id", h.UpdateProject)
	api.DELETE("/projects/:id", h.DeleteProject)

	api.POST("/projects/:id/blueprint/generate", h.GenerateBlueprint)
	api.GET("/projects/:id/blueprint", h.GetBlueprint)
	api.POST("/blueprints/:id/submit-review", h.SubmitBlueprintReview)
	api.POST("/blueprints/:id/approve", h.ApproveBlueprint)
	api.POST("/blueprints/:id/reject", h.RejectBlueprint)

	api.GET("/projects/:id/world-bible", h.GetWorldBible)
	api.PUT("/projects/:id/world-bible", h.UpdateWorldBible)
	api.GET("/projects/:id/constitution", h.GetConstitution)
	api.PUT("/projects/:id/constitution", h.UpdateConstitution)

	api.GET("/projects/:id/characters", h.ListCharacters)
	api.POST("/projects/:id/characters", h.CreateCharacter)
	api.GET("/characters/:id", h.GetCharacter)
	api.PUT("/characters/:id", h.UpdateCharacter)
	api.DELETE("/characters/:id", h.DeleteCharacter)

	api.GET("/projects/:id/outlines", h.ListOutlines)
	api.POST("/projects/:id/outlines", h.CreateOutline)
	api.PUT("/outlines/:id", h.UpdateOutline)
	api.DELETE("/outlines/:id", h.DeleteOutline)

	api.GET("/projects/:id/foreshadowings", h.ListForeshadowings)
	api.POST("/projects/:id/foreshadowings", h.CreateForeshadowing)
	api.PUT("/foreshadowings/:id/status", h.UpdateForeshadowingStatus)
	api.DELETE("/foreshadowings/:id", h.DeleteForeshadowing)

	api.GET("/projects/:id/volumes", h.ListVolumes)
	api.POST("/volumes/:id/submit-review", h.SubmitVolumeReview)
	api.POST("/volumes/:id/approve", h.ApproveVolume)
	api.POST("/volumes/:id/reject", h.RejectVolume)

	api.GET("/projects/:id/chapters", h.ListChapters)
	api.POST("/projects/:id/chapters/generate", h.GenerateChapter)
	api.POST("/projects/:id/chapters/continue", h.ContinueGenerate)
	api.POST("/projects/:id/chapters/stream", h.StreamChapter)
	api.GET("/chapters/:id", h.GetChapter)
	api.POST("/chapters/:id/submit-review", h.SubmitChapterReview)
	api.POST("/chapters/:id/approve", h.ApproveChapter)
	api.POST("/chapters/:id/reject", h.RejectChapter)
	api.POST("/chapters/:id/regenerate", h.RegenerateChapter)
	api.POST("/chapters/:id/quality-check", h.QualityCheck)

	api.POST("/projects/:id/workflow/start", h.StartWorkflow)
	api.GET("/workflows/:id/history", h.GetWorkflowHistory)
	api.POST("/workflows/:id/rollback", h.WorkflowRollback)

	api.GET("/projects/:id/references", h.ListReferences)
	api.POST("/projects/:id/references/upload", h.UploadReference)
	api.GET("/references/:id", h.GetReference)
	api.PUT("/references/:id/migration-config", h.UpdateMigrationConfig)
	api.POST("/references/:id/analyze", h.AnalyzeReference)

	// RAG knowledge base management
	api.POST("/projects/:id/rag/rebuild", h.RebuildRAG)
	api.GET("/projects/:id/rag/status", h.GetRAGStatus)

	api.POST("/projects/:id/agent-reviews", h.StartAgentReview)
	api.GET("/projects/:id/agent-reviews", h.ListAgentReviews)
	api.GET("/projects/:id/agent-reviews/stream", h.StreamAgentReview)
	api.GET("/agent-reviews/:id", h.GetAgentReview)

	// Export
	api.GET("/projects/:id/export/txt", h.ExportTXT)
	api.GET("/projects/:id/export/markdown", h.ExportMarkdown)

	// Workflow diff
	api.GET("/workflows/:id/diff", h.GetWorkflowDiff)

	// LLM Profiles (database-driven model configuration)
	api.GET("/llm-profiles", h.ListLLMProfiles)
	api.POST("/llm-profiles", h.CreateLLMProfile)
	api.GET("/llm-profiles/:id", h.GetLLMProfile)
	api.PUT("/llm-profiles/:id", h.UpdateLLMProfile)
	api.DELETE("/llm-profiles/:id", h.DeleteLLMProfile)
	api.POST("/llm-profiles/:id/set-default", h.SetDefaultLLMProfile)

	// System Settings (replaces config files and env vars for app-level config)
	api.GET("/settings", h.GetSystemSettings)
	api.PUT("/settings/:key", h.SetSystemSetting)
	api.DELETE("/settings/:key", h.DeleteSystemSetting)

	api.GET("/health", h.Health)

	// Change propagation
	api.POST("/projects/:id/change-events", h.CreateChangeEvent)
	api.GET("/projects/:id/change-events", h.ListChangeEvents)
	api.GET("/patch-plans/:id", h.GetPatchPlan)
	api.PUT("/patch-items/:id/status", h.UpdatePatchItemStatus)
	api.POST("/patch-items/:id/execute", h.ExecutePatchItem)

	// Prompt Presets
	api.GET("/prompt-presets", h.ListGlobalPromptPresets)
	api.GET("/projects/:id/prompt-presets", h.ListProjectPromptPresets)
	api.POST("/prompt-presets", h.CreatePromptPreset)
	api.GET("/prompt-presets/:id", h.GetPromptPreset)
	api.PUT("/prompt-presets/:id", h.UpdatePromptPreset)
	api.DELETE("/prompt-presets/:id", h.DeletePromptPreset)

	// Glossary 术语表
	api.GET("/projects/:id/glossary", h.ListGlossary)
	api.POST("/projects/:id/glossary", h.CreateGlossaryTerm)
	api.DELETE("/glossary/:id", h.DeleteGlossaryTerm)

	// Task Queue
	api.GET("/projects/:id/tasks", h.ListTasks)
	api.POST("/tasks", h.EnqueueTask)
	api.GET("/tasks/:id", h.GetTask)
	api.POST("/tasks/:id/cancel", h.CancelTask)
	api.POST("/tasks/:id/retry", h.RetryTask)

	// Resource Ledger (InkOS: particle_ledger)
	api.GET("/projects/:id/resources", h.ListResources)
	api.POST("/projects/:id/resources", h.CreateResource)
	api.GET("/resources/:id", h.GetResource)
	api.PUT("/resources/:id", h.UpdateResource)
	api.DELETE("/resources/:id", h.DeleteResource)
	api.POST("/resources/:id/changes", h.RecordResourceChange)
	api.GET("/resources/:id/changes", h.ListResourceChanges)

	// Vocab Fatigue (InkOS-inspired quality signal)
	api.GET("/projects/:id/quality/vocab-stats", h.GetVocabStats)

	// Webhook Notifications
	api.GET("/projects/:id/webhooks", h.ListWebhooks)
	api.POST("/projects/:id/webhooks", h.CreateWebhook)
	api.PUT("/webhooks/:id", h.UpdateWebhook)
	api.DELETE("/webhooks/:id", h.DeleteWebhook)

	// ── LangGraph Agent ──────────────────────────────────────────────────────
	api.POST("/projects/:id/agent/run", h.AgentRun)
	api.GET("/agent/sessions/:sid/status", h.AgentSessionStatus)
	api.GET("/agent/sessions/:sid/stream", h.AgentSessionStream)

	// ── Knowledge Graph (Neo4j) ───────────────────────────────────────────────
	api.GET("/projects/:id/graph/entities", h.GetGraphEntities)
	api.POST("/projects/:id/graph/query", h.QueryGraph)
	api.POST("/projects/:id/graph/upsert", h.UpsertGraphEntity)
	api.POST("/projects/:id/graph/sync", h.SyncProjectGraph)

	// ── Vector Store (Qdrant) ──────────────────────────────────────────────────
	api.GET("/projects/:id/vector/status", h.GetVectorStatus)
	api.POST("/projects/:id/vector/rebuild", h.RebuildVectorIndex)
	api.POST("/projects/:id/vector/search", h.SearchVector)
}

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
	c.JSON(201, gin.H{"data": bp})
}

func (h *Handler) GetBlueprint(c *gin.Context) {
	bp, err := h.blueprints.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
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

func (h *Handler) StartWorkflow(c *gin.Context) {
	runID, err := h.workflow.CreateRun(c.Request.Context(), c.Param("id"), true)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
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

func (h *Handler) ListReferences(c *gin.Context) {
	refs, err := h.references.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": refs})
}

func (h *Handler) UploadReference(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	uploadDir := "/data/uploads"
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		c.JSON(500, gin.H{"error": "failed to create upload directory"})
		return
	}
	fileName := uuid.New().String() + filepath.Ext(header.Filename)
	filePath := filepath.Join(uploadDir, fileName)
	dst, err := os.Create(filePath)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to save file"})
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		c.JSON(500, gin.H{"error": "failed to save file"})
		return
	}

	title := c.PostForm("title")
	author := c.PostForm("author")
	genre := c.PostForm("genre")

	ref, err := h.references.Create(c.Request.Context(), c.Param("id"), title, author, genre, filePath)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": ref})
}

func (h *Handler) GetReference(c *gin.Context) {
	ref, err := h.references.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(404, gin.H{"error": "reference not found"})
		return
	}
	c.JSON(200, gin.H{"data": ref})
}

func (h *Handler) UpdateMigrationConfig(c *gin.Context) {
	var body json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := h.references.UpdateMigrationConfig(c.Request.Context(), c.Param("id"), body); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "updated"})
}

func (h *Handler) AnalyzeReference(c *gin.Context) {
	refID := c.Param("id")
	ref, err := h.references.Get(c.Request.Context(), refID)
	if err != nil {
		c.JSON(404, gin.H{"error": "reference not found"})
		return
	}

	sidecarURL := os.Getenv("PYTHON_SIDECAR_URL")
	if sidecarURL == "" {
		sidecarURL = "http://localhost:8081"
	}

	reqBody, _ := json.Marshal(map[string]string{
		"file_path":   ref.FilePath,
		"material_id": refID,
		"project_id":  ref.ProjectID,
	})

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, sidecarURL+"/analyze", bytes.NewReader(reqBody))
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to build sidecar request"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if resp != nil {
		defer resp.Body.Close()
	}

	if err != nil || resp == nil || resp.StatusCode != 200 {
		h.logger.Warn("Python sidecar unavailable, using AI fallback", zap.Error(err))
		styleJSON := json.RawMessage(`{"nl_description": "默认风格分析（Python分析服务不可用）"}`)
		narrativeJSON := json.RawMessage(`{"pov_type": "限制性第三人称"}`)
		atmosphereJSON := json.RawMessage(`{"tone_descriptions": ["待分析"]}`)
		h.references.UpdateAnalysis(c.Request.Context(), refID, styleJSON, narrativeJSON, atmosphereJSON)
		c.JSON(200, gin.H{"status": "completed_fallback", "message": "使用AI回退分析完成"})
		return
	}

	var analysisResult struct {
		StyleLayer      json.RawMessage `json:"style_layer"`
		NarrativeLayer  json.RawMessage `json:"narrative_layer"`
		AtmosphereLayer json.RawMessage `json:"atmosphere_layer"`
		StyleSamples    []string        `json:"style_samples"`
		SensorySamples  []string        `json:"sensory_samples"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&analysisResult); err == nil {
		h.references.UpdateAnalysis(c.Request.Context(), refID,
			analysisResult.StyleLayer, analysisResult.NarrativeLayer, analysisResult.AtmosphereLayer)

		// Ingest text samples into the vector store (async to avoid blocking the HTTP response)
		go func() {
			if ingestErr := h.references.IngestSamples(
				c.Request.Context(), ref.ProjectID, refID,
				analysisResult.StyleSamples, analysisResult.SensorySamples,
			); ingestErr != nil {
				h.logger.Warn("RAG ingest failed", zap.String("ref_id", refID), zap.Error(ingestErr))
			}
		}()
	}

	c.JSON(200, gin.H{
		"status":          "completed",
		"style_samples":   len(analysisResult.StyleSamples),
		"sensory_samples": len(analysisResult.SensorySamples),
	})
}

// ---- RAG knowledge-base handlers ----

func (h *Handler) RebuildRAG(c *gin.Context) {
	projectID := c.Param("id")
	rebuilt, err := h.references.RebuildProject(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"rebuilt_sources": rebuilt, "project_id": projectID})
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

// ---- Agent Review Handlers ----

func (h *Handler) StartAgentReview(c *gin.Context) {
	projectID := c.Param("id")
	var req models.AgentReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Rounds <= 0 {
		req.Rounds = 3
	}

	session, err := h.agentReview.StreamReview(c.Request.Context(), projectID, req, func(msg models.AgentMessage) {})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, session)
}

func (h *Handler) StreamAgentReview(c *gin.Context) {
	projectID := c.Param("id")
	scope := c.Query("scope")
	if scope == "" {
		scope = "full"
	}
	targetID := c.Query("target_id")
	roundsStr := c.DefaultQuery("rounds", "3")
	rounds := 3
	if n, err := strconv.Atoi(roundsStr); err == nil && n > 0 {
		rounds = n
	}

	req := models.AgentReviewRequest{Scope: scope, TargetID: targetID, Rounds: rounds}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	_, err := h.agentReview.StreamReview(c.Request.Context(), projectID, req, func(msg models.AgentMessage) {
		data, e := json.Marshal(msg)
		if e != nil {
			return
		}
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
		c.Writer.Flush()
	})
	if err != nil {
		fmt.Fprintf(c.Writer, "data: {\"error\":\"%s\"}\n\n", err.Error())
		c.Writer.Flush()
		return
	}
	fmt.Fprintf(c.Writer, "data: {\"done\":true}\n\n")
	c.Writer.Flush()
}

func (h *Handler) GetAgentReview(c *gin.Context) {
	sessionID := c.Param("id")
	session, err := h.agentReview.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(404, gin.H{"error": "session not found"})
		return
	}
	c.JSON(200, session)
}

func (h *Handler) ListAgentReviews(c *gin.Context) {
	projectID := c.Param("id")
	sessions, err := h.agentReview.ListSessions(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, sessions)
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok", "service": "novelbuilder"})
}

// ---- System Settings Handlers ----

func (h *Handler) GetSystemSettings(c *gin.Context) {
	settings, err := h.systemSettings.GetAll(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": settings})
}

func (h *Handler) SetSystemSetting(c *gin.Context) {
	key := c.Param("key")
	var body struct {
		Value string `json:"value" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := h.systemSettings.Set(c.Request.Context(), key, body.Value); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"key": key, "value": body.Value})
}

func (h *Handler) DeleteSystemSetting(c *gin.Context) {
	key := c.Param("key")
	if err := h.systemSettings.Delete(c.Request.Context(), key); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

// ---- Change Propagation Handlers ----

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

// ---- Export Handlers ----

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

// ---- Workflow Diff Handler ----

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
		c.JSON(404, gin.H{"error": fmt.Sprintf("snapshot not found for step '%s'", fromStep)})
		return
	}
	to, err := h.workflow.GetSnapshot(c.Request.Context(), runID, toStep)
	if err != nil {
		c.JSON(404, gin.H{"error": fmt.Sprintf("snapshot not found for step '%s'", toStep)})
		return
	}

	c.JSON(200, gin.H{"data": gin.H{"from": from, "to": to}})
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---- LLM Profile Handlers ----

func (h *Handler) ListLLMProfiles(c *gin.Context) {
	profiles, err := h.llmProfiles.List(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if profiles == nil {
		profiles = []models.LLMProfile{}
	}
	c.JSON(200, gin.H{"data": profiles})
}

func (h *Handler) CreateLLMProfile(c *gin.Context) {
	var req models.CreateLLMProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	profile, err := h.llmProfiles.Create(c.Request.Context(), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": profile})
}

func (h *Handler) GetLLMProfile(c *gin.Context) {
	profile, err := h.llmProfiles.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(404, gin.H{"error": "profile not found"})
		return
	}
	c.JSON(200, gin.H{"data": profile})
}

func (h *Handler) UpdateLLMProfile(c *gin.Context) {
	var req models.UpdateLLMProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	profile, err := h.llmProfiles.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": profile})
}

func (h *Handler) DeleteLLMProfile(c *gin.Context) {
	if err := h.llmProfiles.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

func (h *Handler) SetDefaultLLMProfile(c *gin.Context) {
	req := models.UpdateLLMProfileRequest{IsDefault: true}
	profile, err := h.llmProfiles.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": profile})
}

// ============================================================
// Prompt Presets
// ============================================================

func (h *Handler) ListGlobalPromptPresets(c *gin.Context) {
	presets, err := h.promptPresets.List(c.Request.Context(), nil)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": presets})
}

func (h *Handler) ListProjectPromptPresets(c *gin.Context) {
	pid := c.Param("id")
	presets, err := h.promptPresets.List(c.Request.Context(), &pid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": presets})
}

func (h *Handler) CreatePromptPreset(c *gin.Context) {
	var req models.CreatePromptPresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// A non-empty project_id can be passed as a query param when creating under a project
	var projectID *string
	if pid := c.Query("project_id"); pid != "" {
		projectID = &pid
	}
	preset, err := h.promptPresets.Create(c.Request.Context(), projectID, req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": preset})
}

func (h *Handler) GetPromptPreset(c *gin.Context) {
	preset, err := h.promptPresets.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": preset})
}

func (h *Handler) UpdatePromptPreset(c *gin.Context) {
	var req models.UpdatePromptPresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	preset, err := h.promptPresets.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": preset})
}

func (h *Handler) DeletePromptPreset(c *gin.Context) {
	if err := h.promptPresets.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

// ============================================================
// Glossary
// ============================================================

func (h *Handler) ListGlossary(c *gin.Context) {
	terms, err := h.glossary.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": terms})
}

func (h *Handler) CreateGlossaryTerm(c *gin.Context) {
	var req models.CreateGlossaryTermRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	term, err := h.glossary.Create(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": term})
}

func (h *Handler) DeleteGlossaryTerm(c *gin.Context) {
	if err := h.glossary.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

// ============================================================
// Task Queue
// ============================================================

func (h *Handler) ListTasks(c *gin.Context) {
	tasks, err := h.taskQueue.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": tasks})
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
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": task})
}

func (h *Handler) CancelTask(c *gin.Context) {
	if err := h.taskQueue.Cancel(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"message": "task cancelled"})
}

func (h *Handler) RetryTask(c *gin.Context) {
	if err := h.taskQueue.Retry(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"message": "task queued for retry"})
}

// ============================================================
// Resource Ledger (InkOS particle_ledger concept)
// ============================================================

func (h *Handler) ListResources(c *gin.Context) {
	resources, err := h.resourceLedger.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": resources})
}

func (h *Handler) CreateResource(c *gin.Context) {
	var req models.CreateStoryResourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	resource, err := h.resourceLedger.Create(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": resource})
}

func (h *Handler) GetResource(c *gin.Context) {
	resource, err := h.resourceLedger.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": resource})
}

func (h *Handler) UpdateResource(c *gin.Context) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	resource, err := h.resourceLedger.Update(c.Request.Context(), c.Param("id"), body.Name, body.Description)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": resource})
}

func (h *Handler) DeleteResource(c *gin.Context) {
	if err := h.resourceLedger.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

func (h *Handler) RecordResourceChange(c *gin.Context) {
	var req models.RecordResourceChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	change, err := h.resourceLedger.RecordChange(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": change})
}

func (h *Handler) ListResourceChanges(c *gin.Context) {
	changes, err := h.resourceLedger.ListChanges(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": changes})
}

// ============================================================
// Vocab Fatigue (InkOS-inspired quality signal)
// ============================================================

func (h *Handler) GetVocabStats(c *gin.Context) {
	projectID := c.Param("id")
	topN := 50
	if s := c.Query("top"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			topN = n
		}
	}
	report, err := h.quality.VocabFatigueReport(c.Request.Context(), projectID, topN)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": report})
}

// ============================================================
// Webhook Notifications
// ============================================================

func (h *Handler) ListWebhooks(c *gin.Context) {
	hooks, err := h.webhooks.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": hooks})
}

func (h *Handler) CreateWebhook(c *gin.Context) {
	var req models.CreateWebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	hook, err := h.webhooks.Create(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": hook})
}

func (h *Handler) UpdateWebhook(c *gin.Context) {
	var req models.CreateWebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	hook, err := h.webhooks.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": hook})
}

func (h *Handler) DeleteWebhook(c *gin.Context) {
	if err := h.webhooks.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

// ═══════════════════════════════════════════════════════════════════════════════
// LangGraph Agent Handlers
// ═══════════════════════════════════════════════════════════════════════════════

func (h *Handler) AgentRun(c *gin.Context) {
	projectID := c.Param("id")
	var req models.AgentRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.TaskType == "" {
		req.TaskType = "generate_chapter"
	}
	sessionID, err := h.sidecar.RunAgent(c.Request.Context(), projectID, req)
	if err != nil {
		h.logger.Error("agent run failed", zap.Error(err))
		c.JSON(502, gin.H{"error": "agent service unavailable: " + err.Error()})
		return
	}
	c.JSON(202, gin.H{"session_id": sessionID, "status": "running"})
}

func (h *Handler) AgentSessionStatus(c *gin.Context) {
	sid := c.Param("sid")
	status, err := h.sidecar.GetAgentStatus(c.Request.Context(), sid)
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, status)
}

func (h *Handler) AgentSessionStream(c *gin.Context) {
	// Proxy the SSE stream from Python sidecar
	sid := c.Param("sid")
	sidecarURL := h.sidecar.StreamURL(sid)

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, sidecarURL, nil)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(502, gin.H{"error": "sidecar stream unavailable"})
		return
	}
	defer resp.Body.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(200)

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			c.Writer.Write(buf[:n])
			c.Writer.Flush()
		}
		if err != nil {
			break
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Knowledge Graph (Neo4j) Handlers
// ═══════════════════════════════════════════════════════════════════════════════

func (h *Handler) GetGraphEntities(c *gin.Context) {
	data, err := h.sidecar.GetGraphEntities(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, data)
}

func (h *Handler) QueryGraph(c *gin.Context) {
	var req models.GraphQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	raw, err := h.sidecar.QueryGraph(c.Request.Context(), req)
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.Data(200, "application/json", raw)
}

func (h *Handler) UpsertGraphEntity(c *gin.Context) {
	var req models.GraphUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := h.sidecar.UpsertGraphEntity(c.Request.Context(), c.Param("id"), req); err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) SyncProjectGraph(c *gin.Context) {
	if err := h.sidecar.SyncProjectGraph(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "graph sync triggered"})
}

// ═══════════════════════════════════════════════════════════════════════════════
// Vector Store (Qdrant) Handlers
// ═══════════════════════════════════════════════════════════════════════════════

func (h *Handler) GetVectorStatus(c *gin.Context) {
	status, err := h.sidecar.GetVectorStatus(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, status)
}

func (h *Handler) RebuildVectorIndex(c *gin.Context) {
	var req models.VectorRebuildRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := h.sidecar.RebuildVectorIndex(c.Request.Context(), c.Param("id"), req.Items); err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) SearchVector(c *gin.Context) {
	var req models.VectorSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Limit == 0 {
		req.Limit = 5
	}
	raw, err := h.sidecar.SearchVector(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.Data(200, "application/json", raw)
}
