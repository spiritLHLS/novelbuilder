package handlers

import (
	"context"
	"errors"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/services"
	"github.com/novelbuilder/backend/internal/workflow"
	"go.uber.org/zap"
)

type Handler struct {
	projects              *services.ProjectService
	blueprints            *services.BlueprintService
	chapters              *services.ChapterService
	worldBibles           *services.WorldBibleService
	characters            *services.CharacterService
	outlines              *services.OutlineService
	foreshadowings        *services.ForeshadowingService
	volumes               *services.VolumeService
	quality               *services.QualityService
	references            *services.ReferenceService
	rag                   *services.RAGService
	workflow              *workflow.Engine
	agentReview           *services.AgentReviewService
	export                *services.ExportService
	llmProfiles           *services.LLMProfileService
	propagation           *services.EditPropagationService
	promptPresets         *services.PromptPresetService
	glossary              *services.GlossaryService
	taskQueue             *services.TaskQueueService
	resourceLedger        *services.ResourceLedgerService
	webhooks              *services.WebhookService
	sidecar               *services.SidecarService
	systemSettings        *services.SystemSettingsService
	audit                 *services.AuditService
	bookRules             *services.BookRulesService
	imports               *services.ImportService
	agentRouting          *services.AgentRoutingService
	genreTemplates        *services.GenreTemplateService
	analytics             *services.AnalyticsService
	subplots              *services.SubplotService
	emotionalArcs         *services.EmotionalArcService
	characterInteractions *services.CharacterInteractionService
	radar                 *services.RadarService
	deepAnalysis          *services.ReferenceDeepAnalysisService
	logger                *zap.Logger

	// ragRebuildJobs tracks in-progress / recently completed RAG rebuild tasks
	// keyed by project ID so they survive page refreshes (server stays up).
	ragRebuildJobs sync.Map // projectID → *ragRebuildState
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
	audit *services.AuditService,
	bookRules *services.BookRulesService,
	imports *services.ImportService,
	agentRouting *services.AgentRoutingService,
	genreTemplates *services.GenreTemplateService,
	analytics *services.AnalyticsService,
	subplots *services.SubplotService,
	emotionalArcs *services.EmotionalArcService,
	characterInteractions *services.CharacterInteractionService,
	radar *services.RadarService,
	deepAnalysis *services.ReferenceDeepAnalysisService,
	logger *zap.Logger,
) *Handler {
	return &Handler{
		projects:              projects,
		blueprints:            blueprints,
		chapters:              chapters,
		worldBibles:           worldBibles,
		characters:            characters,
		outlines:              outlines,
		foreshadowings:        foreshadowings,
		volumes:               volumes,
		quality:               quality,
		references:            references,
		rag:                   rag,
		workflow:              wf,
		agentReview:           agentReview,
		export:                export,
		llmProfiles:           llmProfiles,
		propagation:           propagation,
		promptPresets:         promptPresets,
		glossary:              glossary,
		taskQueue:             taskQueue,
		resourceLedger:        resourceLedger,
		webhooks:              webhooks,
		sidecar:               sidecar,
		systemSettings:        systemSettings,
		audit:                 audit,
		bookRules:             bookRules,
		imports:               imports,
		agentRouting:          agentRouting,
		genreTemplates:        genreTemplates,
		analytics:             analytics,
		subplots:              subplots,
		emotionalArcs:         emotionalArcs,
		characterInteractions: characterInteractions,
		radar:                 radar,
		deepAnalysis:          deepAnalysis,
		logger:                logger,
	}
}

// RegisterRoutes wires all API routes onto r.
// Pass authMiddleware to require authentication on all routes except
// the public auth endpoints (login/logout) which must be registered separately.
func (h *Handler) RegisterRoutes(r *gin.Engine, authMiddleware ...gin.HandlerFunc) {
	api := r.Group("/api")
	if len(authMiddleware) > 0 {
		api.Use(authMiddleware...)
	}

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
	api.GET("/projects/:id/world-bible/export", h.ExportWorldBible)
	api.POST("/projects/:id/world-bible/import", h.ImportWorldBible)
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
	api.PUT("/foreshadowings/:id", h.UpdateForeshadowing)
	api.DELETE("/foreshadowings/:id", h.DeleteForeshadowing)

	api.GET("/projects/:id/volumes", h.ListVolumes)
	api.POST("/volumes/:id/submit-review", h.SubmitVolumeReview)
	api.POST("/volumes/:id/approve", h.ApproveVolume)
	api.POST("/volumes/:id/reject", h.RejectVolume)

	api.GET("/projects/:id/chapters", h.ListChapters)
	api.POST("/projects/:id/chapters/generate", h.GenerateChapter)
	api.POST("/projects/:id/chapters/continue", h.ContinueGenerate)
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
	api.POST("/projects/:id/references/import-url", h.ImportReferenceFromURL)
	api.POST("/projects/:id/references/search", h.SearchReferenceNovels)
	api.POST("/projects/:id/references/search-stream", h.SearchReferenceNovelsStream)
	api.POST("/projects/:id/references/book-info", h.GetReferenceBookInfo)
	api.POST("/projects/:id/references/fetch-import", h.FetchImportReference)
	api.POST("/projects/:id/references/import-local", h.ImportReferenceLocal)
	api.POST("/projects/:id/references/export-batch", h.ExportReferenceBatch)
	api.GET("/references/:id", h.GetReference)
	api.PUT("/references/:id/migration-config", h.UpdateMigrationConfig)
	api.POST("/references/:id/analyze", h.AnalyzeReference)
	// Deep (chunked, background) analysis
	api.POST("/references/:id/deep-analyze", h.StartDeepAnalysis)
	api.GET("/references/:id/deep-analyze/job", h.GetDeepAnalysisJob)
	api.POST("/references/:id/deep-analyze/cancel", h.CancelDeepAnalysisJob)
	api.POST("/references/:id/deep-analyze/reset", h.ResetDeepAnalysis)
	api.POST("/references/:id/deep-analyze/import", h.ImportDeepAnalysisResult)
	api.DELETE("/references/:id", h.DeleteReference)
	api.GET("/references/:id/export", h.ExportReferenceSingle)
	api.POST("/references/:id/resume-download", h.ResumeReferenceDownload)
	// Reference chapter management
	api.GET("/references/:id/chapters", h.ListReferenceChapters)
	api.DELETE("/reference-chapters/:id", h.DeleteReferenceChapter)
	api.POST("/references/:id/chapters/batch-delete", h.BatchDeleteReferenceChapters)

	// RAG knowledge base management
	api.POST("/projects/:id/rag/rebuild", h.RebuildRAG)
	api.GET("/projects/:id/rag/rebuild-status", h.GetRebuildRAGStatus)
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
	api.POST("/llm-profiles/test", h.TestLLMProfile)
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

	// ── 33-Dimension Audit ────────────────────────────────────────────────────
	api.POST("/chapters/:id/audit", h.AuditChapter)
	api.POST("/chapters/:id/audit-revise", h.AuditReviseChapter)
	api.GET("/chapters/:id/audit-report", h.GetChapterAuditReport)
	api.GET("/chapters/:id/snapshots", h.ListChapterSnapshots)
	api.POST("/chapters/:id/restore", h.RestoreChapterSnapshot)

	// ── Anti-AI Rewrite (去AI味) ───────────────────────────────────────────────
	api.POST("/chapters/:id/anti-detect", h.AntiDetectChapter)

	// ── Book Rules (style guide) ───────────────────────────────────────────────
	api.GET("/projects/:id/book-rules", h.GetBookRules)
	api.PUT("/projects/:id/book-rules", h.UpdateBookRules)

	// ── Creative Brief (创作简报) ──────────────────────────────────────────────
	api.POST("/projects/:id/creative-brief", h.GenerateCreativeBrief)

	// ── Chapter Import (续写已有作品) ──────────────────────────────────────────
	api.POST("/projects/:id/import-chapters", h.CreateChapterImport)
	api.GET("/projects/:id/import-chapters", h.ListChapterImports)
	api.GET("/imports/:id", h.GetChapterImport)
	api.POST("/imports/:id/process", h.ProcessChapterImport)

	// ── Fan Fiction Settings ───────────────────────────────────────────────────
	api.PUT("/projects/:id/fanfic", h.UpdateProjectFanfic)

	// ── Per-Agent Model Routing ────────────────────────────────────────────────
	api.GET("/agent-routes", h.ListAgentRoutes)
	api.PUT("/agent-routes/:agent_type", h.UpsertAgentRoute)
	api.DELETE("/agent-routes/:agent_type", h.DeleteAgentRoute)
	api.GET("/projects/:id/agent-routes", h.ListProjectAgentRoutes)
	api.PUT("/projects/:id/agent-routes/:agent_type", h.UpsertProjectAgentRoute)
	api.DELETE("/projects/:id/agent-routes/:agent_type", h.DeleteProjectAgentRoute)

	// ── Auto-Write Daemon ─────────────────────────────────────────────────────
	api.PUT("/projects/:id/auto-write", h.SetAutoWrite)

	// ── Analytics Dashboard ───────────────────────────────────────────────────
	api.GET("/projects/:id/analytics", h.GetProjectAnalytics)

	// ── EPUB Export ───────────────────────────────────────────────────────────
	api.GET("/projects/:id/export/epub", h.ExportEPUB)

	// ── Batch Chapter Write ───────────────────────────────────────────────────
	api.POST("/projects/:id/chapters/batch-generate", h.BatchGenerateChapters)

	// ── Subplot Board ─────────────────────────────────────────────────────────
	api.GET("/projects/:id/subplots", h.ListSubplots)
	api.POST("/projects/:id/subplots", h.CreateSubplot)
	api.PUT("/subplots/:id", h.UpdateSubplot)
	api.DELETE("/subplots/:id", h.DeleteSubplot)
	api.GET("/subplots/:id/checkpoints", h.ListSubplotCheckpoints)
	api.POST("/subplots/:id/checkpoints", h.AddSubplotCheckpoint)

	// ── Emotional Arcs ────────────────────────────────────────────────────────
	api.GET("/projects/:id/emotional-arcs", h.ListEmotionalArcs)
	api.POST("/projects/:id/emotional-arcs", h.UpsertEmotionalArc)
	api.DELETE("/emotional-arcs/:id", h.DeleteEmotionalArc)

	// ── Character Interaction Matrix ──────────────────────────────────────────
	api.GET("/projects/:id/character-interactions", h.ListCharacterInteractions)
	api.POST("/projects/:id/character-interactions", h.UpsertCharacterInteraction)
	api.DELETE("/character-interactions/:id", h.DeleteCharacterInteraction)

	// ── Radar Market Scan ─────────────────────────────────────────────────────
	api.POST("/projects/:id/radar/scan", h.RadarScan)
	api.GET("/projects/:id/radar/history", h.ListRadarHistory)

	// ── Genre Templates ──────────────────────────────────────────────────────────
	api.GET("/genre-templates", h.ListGenreTemplates)
	api.GET("/genre-templates/:genre", h.GetGenreTemplate)
	api.PUT("/genre-templates/:genre", h.UpsertGenreTemplate)
	api.DELETE("/genre-templates/:genre", h.DeleteGenreTemplate)

	// ── Runtime Diagnostics ───────────────────────────────────────────────────
	api.GET("/doctor", h.Doctor)

	// ── Service Logs ──────────────────────────────────────────────────────────
	api.GET("/logs", h.GetServiceLogs)
}

// ── Shared LLM Config Helpers ─────────────────────────────────────────────────

func (h *Handler) resolveLLMConfig(ctx context.Context) (map[string]interface{}, error) {
	profile, err := h.llmProfiles.GetDefault(ctx)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, errors.New("no default AI model configured: please set one in 设置 → AI 模型配置")
	}
	return map[string]interface{}{
		"api_key":          profile.APIKey,
		"model":            profile.ModelName,
		"base_url":         profile.BaseURL,
		"provider":         profile.Provider,
		"max_tokens":       profile.MaxTokens,
		"temperature":      profile.Temperature,
		"rpm_limit":        profile.RPMLimit,
		"omit_max_tokens":  profile.OmitMaxTokens,
		"omit_temperature": profile.OmitTemperature,
		"api_style":        profile.APIStyle,
	}, nil
}

// resolveAgentLLMConfig returns an LLM config map for a given agent type,
// respecting project-level -> global-level -> default-profile priority order.
func (h *Handler) resolveAgentLLMConfig(ctx context.Context, agentType, projectID string) (map[string]interface{}, error) {
	cfg, err := h.agentRouting.ResolveForAgent(ctx, agentType, projectID)
	if err != nil {
		return h.resolveLLMConfig(ctx)
	}
	if cfg == nil {
		return h.resolveLLMConfig(ctx)
	}
	apiKey, _ := cfg["api_key"].(string)
	if apiKey == "" {
		return h.resolveLLMConfig(ctx)
	}
	if _, hasTemp := cfg["temperature"]; !hasTemp {
		if defCfg, defErr := h.resolveLLMConfig(ctx); defErr == nil && defCfg != nil {
			cfg["temperature"] = defCfg["temperature"]
			cfg["max_tokens"] = defCfg["max_tokens"]
			cfg["rpm_limit"] = defCfg["rpm_limit"]
			cfg["omit_max_tokens"] = defCfg["omit_max_tokens"]
			cfg["omit_temperature"] = defCfg["omit_temperature"]
			cfg["api_style"] = defCfg["api_style"]
		}
	}
	return cfg, nil
}

// containsStr reports whether sub appears in s (case-sensitive byte search).
func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
