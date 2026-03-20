package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/config"
	"github.com/novelbuilder/backend/internal/database"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/handlers"
	"github.com/novelbuilder/backend/internal/middleware"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/services"
	"github.com/novelbuilder/backend/internal/workflow"
	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Load infrastructure config (env-vars only, no config files)
	cfg := config.Load()

	// Set Gin mode
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize database
	db, err := database.NewPool(cfg.Database, logger)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer db.Close()

	// Initialize Redis
	rdb, err := database.NewRedis(cfg.Redis, logger)
	if err != nil {
		logger.Fatal("failed to connect to redis", zap.Error(err))
	}
	defer rdb.Close()

	// Bootstrap system settings: auto-generate encryption key on first run.
	// No ENCRYPTION_KEY env var is needed; the key is stored in system_settings.
	sysSettings := services.NewSystemSettingsService(db, logger)
	encryptionKey, err := sysSettings.BootstrapEncryptionKey(context.Background())
	if err != nil {
		logger.Fatal("failed to bootstrap encryption key", zap.Error(err))
	}

	// All AI model config is stored in the database (llm_profiles table).
	// The frontend Settings → AI 模型配置 page manages these profiles.
	llmProfileService := services.NewLLMProfileService(db, encryptionKey, logger)
	aiGateway := gateway.NewAIGateway(llmProfileService, logger)

	// Initialize Workflow Engine
	wfEngine := workflow.NewEngine(db, logger)

	// Initialize Services
	projectService := services.NewProjectService(db, logger)
	blueprintService := services.NewBlueprintService(db, aiGateway, wfEngine, logger)
	ragService := services.NewRAGService(db, cfg.PythonSidecar.URL, logger)
	originalityService := services.NewOriginalityService(db, cfg.PythonSidecar.URL, logger)
	propagationService := services.NewEditPropagationService(db, aiGateway, logger)
	glossaryService := services.NewGlossaryService(db, logger)
	webhookService := services.NewWebhookService(db, logger)
	chapterService := services.NewChapterService(db, rdb, aiGateway, wfEngine, ragService, originalityService, propagationService, glossaryService, webhookService, cfg.PythonSidecar.URL, logger)
	worldBibleService := services.NewWorldBibleService(db, aiGateway, logger)
	characterService := services.NewCharacterService(db, aiGateway, logger)
	outlineService := services.NewOutlineService(db, aiGateway, logger)
	foreshadowingService := services.NewForeshadowingService(db, logger)
	volumeService := services.NewVolumeService(db, logger)
	qualityService := services.NewQualityService(db, aiGateway, logger)
	referenceService := services.NewReferenceService(db, cfg.PythonSidecar.URL, ragService, logger)
	agentReviewService := services.NewAgentReviewService(db, aiGateway, logger)
	exportService := services.NewExportService(db, logger)
	promptPresetService := services.NewPromptPresetService(db, logger)
	taskQueueService := services.NewTaskQueueService(db, cfg.TaskQueue.Workers, cfg.TaskQueue.MaxRetries, logger)
	resourceLedgerService := services.NewResourceLedgerService(db, logger)

	// Sidecar proxy service (agent / graph / vector)
	sidecarService := services.NewSidecarService(cfg.PythonSidecar.URL, logger)

	// ── New inkos-parity services ─────────────────────────────────────────────
	auditService := services.NewAuditService(db, cfg.PythonSidecar.URL, logger)
	bookRulesService := services.NewBookRulesService(db, cfg.PythonSidecar.URL, logger)
	importService := services.NewImportService(db, cfg.PythonSidecar.URL, logger)
	agentRoutingService := services.NewAgentRoutingService(db, encryptionKey, logger)

	// ── Phase-2 feature services ──────────────────────────────────────────────
	analyticsService := services.NewAnalyticsService(db, logger)
	subplotService := services.NewSubplotService(db, logger)
	emotionalArcService := services.NewEmotionalArcService(db, logger)
	characterInteractionService := services.NewCharacterInteractionService(db, logger)
	radarService := services.NewRadarService(db, aiGateway, logger)
	genreTemplateService := services.NewGenreTemplateService(db, logger)

	// ── Deep reference analysis service (chunked, background) ────────────────
	// Must be created AFTER taskQueueService so it can register its handler.
	deepAnalysisService := services.NewReferenceDeepAnalysisService(
		db,
		cfg.PythonSidecar.URL,
		referenceService,
		characterService,
		outlineService,
		worldBibleService,
		taskQueueService,
		agentRoutingService,
		logger,
	)

	// Start background task worker pool
	taskQueueService.Start()
	defer taskQueueService.Stop()

	// ── Auto-Write background daemon ──────────────────────────────────────────────
	// Every minute, enqueue generate_next_chapter for each project with auto_write_enabled.
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rows, qErr := db.Query(serverCtx,
					`SELECT id, auto_write_interval FROM projects WHERE auto_write_enabled = TRUE AND status = 'active'`)
				if qErr != nil {
					logger.Warn("auto_write query failed", zap.Error(qErr))
					continue
				}
				for rows.Next() {
					var pid string
					var intervalMins int
					if err := rows.Scan(&pid, &intervalMins); err != nil {
						continue
					}
					if intervalMins <= 0 {
						intervalMins = 60
					}
					// Check Redis to see when last auto-write ran for this project.
					lastKey := fmt.Sprintf("auto_write_last:%s", pid)
					var shouldEnqueue bool
					lastVal, rErr := rdb.Get(serverCtx, lastKey).Int64()
					if rErr != nil {
						shouldEnqueue = true // never ran
					} else {
						elapsed := time.Now().Unix() - lastVal
						shouldEnqueue = elapsed >= int64(intervalMins)*60
					}
					if !shouldEnqueue {
						continue
					}
					rdb.Set(serverCtx, lastKey, time.Now().Unix(), time.Duration(intervalMins*2)*time.Minute)
					if _, enqErr := taskQueueService.Enqueue(serverCtx, models.CreateTaskRequest{
						TaskType:  "generate_next_chapter",
						ProjectID: pid,
					}); enqErr != nil {
						logger.Warn("auto_write enqueue failed",
							zap.String("project_id", pid), zap.Error(enqErr))
					} else {
						logger.Info("auto_write task enqueued", zap.String("project_id", pid))
					}
				}
				rows.Close()
			case <-serverCtx.Done():
				return
			}
		}
	}()

	// Initialize Handler
	h := handlers.NewHandler(
		projectService,
		blueprintService,
		chapterService,
		worldBibleService,
		characterService,
		outlineService,
		foreshadowingService,
		volumeService,
		qualityService,
		referenceService,
		ragService,
		wfEngine,
		agentReviewService,
		exportService,
		llmProfileService,
		propagationService,
		promptPresetService,
		glossaryService,
		taskQueueService,
		resourceLedgerService,
		webhookService,
		sidecarService,
		sysSettings,
		auditService,
		bookRulesService,
		importService,
		agentRoutingService,
		genreTemplateService,
		analyticsService,
		subplotService,
		emotionalArcService,
		characterInteractionService,
		radarService,
		deepAnalysisService,
		logger,
	)

	// Register background task handlers.
	taskQueueService.RegisterHandler("generate_next_chapter", func(ctx context.Context, task models.TaskQueueItem) error {
		if task.ProjectID == nil || *task.ProjectID == "" {
			return fmt.Errorf("generate_next_chapter requires project_id")
		}
		projectID := *task.ProjectID

		if err := wfEngine.CanGenerateNextChapter(ctx, projectID); err != nil {
			return err
		}

		var payload struct {
			Generate    models.GenerateChapterRequest `json:"generate"`
			AuditRevise models.AuditReviseRequest     `json:"audit_revise"`
		}
		if len(task.Payload) > 0 {
			_ = json.Unmarshal(task.Payload, &payload)
		}

		// Resolve writer-agent routing config for the auto-write task.
		if writerCfg, wErr := agentRoutingService.ResolveForAgent(ctx, "writer", projectID); wErr == nil && writerCfg != nil {
			payload.Generate.LLMConfig = writerCfg
		}

		lastNum, err := chapterService.MaxChapterNum(ctx, projectID)
		if err != nil {
			return fmt.Errorf("get max chapter num: %w", err)
		}

		ch, err := chapterService.Generate(ctx, projectID, lastNum+1, payload.Generate)
		if err != nil {
			return err
		}

		if _, err := h.RunAuditRevisePipeline(ctx, ch.ID, payload.AuditRevise); err != nil {
			return fmt.Errorf("audit-revise pipeline: %w", err)
		}
		return nil
	})

	// Setup Gin router
	r := gin.Default()

	// CORS middleware
	r.Use(cors.New(cors.Config{
		AllowOrigins:  []string{"*"},
		AllowMethods:  []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Authorization", "Idempotency-Key", "X-Request-Id"},
		ExposeHeaders: []string{"Content-Length", "X-Request-Id"},
		MaxAge:        12 * time.Hour,
	}))

	r.Use(middleware.RequestID())
	r.Use(middleware.Logger(logger))

	// Register API routes
	h.RegisterRoutes(r)

	// Serve Vue frontend static files
	r.Static("/assets", "./frontend/dist/assets")
	r.StaticFile("/favicon.ico", "./frontend/dist/favicon.ico")
	r.NoRoute(func(c *gin.Context) {
		// Return index.html for all non-API routes (Vue SPA routing)
		if len(c.Request.URL.Path) < 4 || c.Request.URL.Path[:4] != "/api" {
			c.File("./frontend/dist/index.html")
			return
		}
		c.JSON(404, gin.H{"error": "not found"})
	})

	// Create HTTP server with extended write timeout for SSE streaming
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("starting server", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("server forced to shutdown", zap.Error(err))
	}
	logger.Info("server exited")
}
