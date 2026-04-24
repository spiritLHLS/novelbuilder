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
	"go.uber.org/zap/zapcore"
)

func attachTaskSession(ctx context.Context, cfg map[string]interface{}, sessionID string) (context.Context, map[string]interface{}) {
	if sessionID == "" {
		return ctx, cfg
	}
	cloned := make(map[string]interface{}, len(cfg)+1)
	for key, value := range cfg {
		cloned[key] = value
	}
	cloned["session_id"] = sessionID
	return gateway.WithSessionID(ctx, sessionID), cloned
}

func main() {
	// Build a human-readable console logger (GVA style).
	encCfg := zap.NewProductionEncoderConfig()
	encCfg.EncodeTime = zapcore.TimeEncoderOfLayout("2006/01/02 - 15:04:05.000")
	encCfg.EncodeLevel = zapcore.CapitalLevelEncoder
	encCfg.EncodeCaller = zapcore.ShortCallerEncoder
	encCfg.ConsoleSeparator = " "
	logCore := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encCfg),
		zapcore.AddSync(os.Stdout),
		zapcore.InfoLevel,
	)
	logger := zap.New(logCore, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
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
	aiGateway := gateway.NewAIGateway(llmProfileService, rdb, logger)

	// Initialize Workflow Engine
	wfEngine := workflow.NewEngine(db, logger)

	// Initialize Services
	projectService := services.NewProjectService(db, logger)
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
	blueprintService := services.NewBlueprintService(
		db, aiGateway, wfEngine,
		worldBibleService, characterService, foreshadowingService,
		glossaryService, outlineService, referenceService,
		genreTemplateService, logger,
	)

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
				func() {
					defer rows.Close()
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
				}()
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
	autoApproveIfAllowed := func(ctx context.Context, projectID string, chapter *models.Chapter, report *models.QualityReport) error {
		if chapter == nil || report == nil || !report.Pass || wfEngine.IsStrictReview(ctx, projectID) {
			return nil
		}
		return chapterService.AutoApprove(ctx, chapter.ID, "auto-approved after passing generation quality gate")
	}

	taskQueueService.RegisterHandler("chapter_generate", func(ctx context.Context, task models.TaskQueueItem) error {
		if task.ProjectID == nil || *task.ProjectID == "" {
			return fmt.Errorf("chapter_generate requires project_id")
		}

		var payload models.ChapterGenerateTaskPayload
		if len(task.Payload) > 0 {
			if err := json.Unmarshal(task.Payload, &payload); err != nil {
				return fmt.Errorf("chapter_generate: parse payload: %w", err)
			}
		}
		if payload.Request.ChapterNum <= 0 {
			return fmt.Errorf("chapter_generate: invalid chapter_num")
		}

		llmCfg, err := agentRoutingService.ResolveForAgent(ctx, "writer", *task.ProjectID)
		if err != nil {
			return fmt.Errorf("chapter_generate: resolve llm config: %w", err)
		}
		if llmCfg == nil {
			return fmt.Errorf("chapter_generate: no writer llm profile configured")
		}
		sessionID := fmt.Sprintf("chapter_generate:%s:chapter-%d:task", *task.ProjectID, payload.Request.ChapterNum)
		ctx, payload.Request.LLMConfig = attachTaskSession(ctx, llmCfg, sessionID)

		chapter, report, err := services.GenerateChapterWithQualityRetries(ctx, chapterService, qualityService, *task.ProjectID, payload.Request.ChapterNum, payload.Request)
		if err != nil {
			return err
		}
		return autoApproveIfAllowed(ctx, *task.ProjectID, chapter, report)
	})

	taskQueueService.RegisterHandler("chapter_regenerate", func(ctx context.Context, task models.TaskQueueItem) error {
		if task.ProjectID == nil || *task.ProjectID == "" {
			return fmt.Errorf("chapter_regenerate requires project_id")
		}

		var payload models.ChapterRegenerateTaskPayload
		if len(task.Payload) > 0 {
			if err := json.Unmarshal(task.Payload, &payload); err != nil {
				return fmt.Errorf("chapter_regenerate: parse payload: %w", err)
			}
		}
		if payload.ChapterID == "" {
			return fmt.Errorf("chapter_regenerate: missing chapter_id")
		}

		llmCfg, err := agentRoutingService.ResolveForAgent(ctx, "writer", *task.ProjectID)
		if err != nil {
			return fmt.Errorf("chapter_regenerate: resolve llm config: %w", err)
		}
		if llmCfg == nil {
			return fmt.Errorf("chapter_regenerate: no writer llm profile configured")
		}
		sessionID := fmt.Sprintf("chapter_regenerate:%s:chapter-%d:task", *task.ProjectID, payload.Request.ChapterNum)
		ctx, payload.Request.LLMConfig = attachTaskSession(ctx, llmCfg, sessionID)

		chapter, report, err := services.RegenerateChapterWithQualityRetries(ctx, chapterService, qualityService, payload.ChapterID, payload.Request)
		if err != nil {
			return err
		}
		return autoApproveIfAllowed(ctx, *task.ProjectID, chapter, report)
	})

	taskQueueService.RegisterHandler("generate_next_chapter", func(ctx context.Context, task models.TaskQueueItem) error {
		if task.ProjectID == nil || *task.ProjectID == "" {
			return fmt.Errorf("generate_next_chapter requires project_id")
		}

		var payload models.GenerateNextChapterTaskPayload
		if len(task.Payload) > 0 {
			if err := json.Unmarshal(task.Payload, &payload); err != nil {
				return fmt.Errorf("generate_next_chapter: parse payload: %w", err)
			}
		}

		nextNum, err := chapterService.MaxChapterNum(ctx, *task.ProjectID)
		if err != nil {
			return fmt.Errorf("generate_next_chapter: get max chapter: %w", err)
		}
		nextNum++

		llmCfg, err := agentRoutingService.ResolveForAgent(ctx, "writer", *task.ProjectID)
		if err != nil {
			return fmt.Errorf("generate_next_chapter: resolve llm config: %w", err)
		}
		if llmCfg == nil {
			return fmt.Errorf("generate_next_chapter: no writer llm profile configured")
		}
		payload.Request.ChapterNum = nextNum
		sessionID := fmt.Sprintf("generate_next_chapter:%s:chapter-%d:task", *task.ProjectID, nextNum)
		ctx, payload.Request.LLMConfig = attachTaskSession(ctx, llmCfg, sessionID)

		chapter, report, err := services.GenerateChapterWithQualityRetries(ctx, chapterService, qualityService, *task.ProjectID, nextNum, payload.Request)
		if err != nil {
			return err
		}
		return autoApproveIfAllowed(ctx, *task.ProjectID, chapter, report)
	})

	// chapter_import_process: enqueued by ProcessChapterImport handler.
	// Runs the AI-assisted chapter-split and reverse-engineering pipeline for an import record.
	taskQueueService.RegisterHandler("chapter_import_process", func(ctx context.Context, task models.TaskQueueItem) error {
		var payload struct {
			ImportID string `json:"import_id"`
		}
		if len(task.Payload) > 0 {
			_ = json.Unmarshal(task.Payload, &payload)
		}
		if payload.ImportID == "" {
			return fmt.Errorf("chapter_import_process: missing import_id")
		}

		// Resolve LLM config at execution time — never stored in task payload.
		var llmCfg map[string]interface{}
		if task.ProjectID != nil && *task.ProjectID != "" {
			if writerCfg, wErr := agentRoutingService.ResolveForAgent(ctx, "writer", *task.ProjectID); wErr == nil && writerCfg != nil {
				llmCfg = writerCfg
			}
		}

		return importService.Process(ctx, payload.ImportID, llmCfg)
	})

	// generate_chapter_outlines: enqueued by GenerateChapterOutlines handler.
	// Generates chapter outlines for a specific volume in batches.
	// Auto-chains: after completing one batch, enqueues next batch if chapters remain.
	taskQueueService.RegisterHandler("generate_chapter_outlines", func(ctx context.Context, task models.TaskQueueItem) error {
		if task.ProjectID == nil || *task.ProjectID == "" {
			return fmt.Errorf("generate_chapter_outlines requires project_id")
		}

		var payload struct {
			VolumeNum    int `json:"volume_num"`
			BatchSize    int `json:"batch_size"`
			StartChapter int `json:"start_chapter"` // 0=continue from last, >0=regenerate from specific chapter
		}
		if len(task.Payload) > 0 {
			_ = json.Unmarshal(task.Payload, &payload)
		}
		if payload.VolumeNum <= 0 {
			return fmt.Errorf("generate_chapter_outlines: invalid volume_num")
		}
		if payload.BatchSize <= 0 {
			payload.BatchSize = 10
		}

		// Generate one batch
		if err := blueprintService.GenerateChapterOutlines(ctx, *task.ProjectID, payload.VolumeNum, payload.BatchSize, payload.StartChapter); err != nil {
			return err
		}

		// After successful generation, check if more chapters remain in this volume
		// Query volume range and existing outlines to decide if we need to enqueue next batch
		var volStart, volEnd int
		if err := db.QueryRow(ctx,
			`SELECT chapter_start, chapter_end FROM volumes WHERE project_id = $1 AND volume_num = $2`,
			*task.ProjectID, payload.VolumeNum).Scan(&volStart, &volEnd); err != nil {
			logger.Warn("failed to check volume range for chaining (non-fatal)",
				zap.String("project_id", *task.ProjectID),
				zap.Int("volume_num", payload.VolumeNum),
				zap.Error(err))
			return nil // Non-fatal: batch completed successfully
		}

		totalChapters := volEnd - volStart + 1
		var existingCount int
		if err := db.QueryRow(ctx,
			`SELECT COUNT(*) FROM outlines 
			 WHERE project_id = $1 AND level = 'chapter' 
			 AND order_num >= $2 AND order_num <= $3`,
			*task.ProjectID, volStart, volEnd).Scan(&existingCount); err != nil {
			logger.Warn("failed to count existing outlines for chaining (non-fatal)",
				zap.String("project_id", *task.ProjectID),
				zap.Error(err))
			return nil
		}

		remaining := totalChapters - existingCount
		if remaining > 0 {
			// More chapters remain: enqueue next batch (auto-continue from where we left off)
			nextPayload, _ := json.Marshal(map[string]interface{}{
				"volume_num":    payload.VolumeNum,
				"batch_size":    payload.BatchSize,
				"start_chapter": 0, // Auto-continue
			})
			_, enqErr := taskQueueService.Enqueue(ctx, models.CreateTaskRequest{
				ProjectID: *task.ProjectID,
				TaskType:  "generate_chapter_outlines",
				Payload:   nextPayload,
				Priority:  5,
			})
			if enqErr == nil {
				logger.Info("auto-enqueued next chapter outline batch",
					zap.String("project_id", *task.ProjectID),
					zap.Int("volume_num", payload.VolumeNum),
					zap.Int("remaining", remaining))
			} else {
				logger.Warn("failed to auto-enqueue next batch (non-fatal)",
					zap.String("project_id", *task.ProjectID),
					zap.Error(enqErr))
			}
		} else {
			logger.Info("chapter outline generation complete for volume",
				zap.String("project_id", *task.ProjectID),
				zap.Int("volume_num", payload.VolumeNum),
				zap.Int("total_chapters", totalChapters))
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

	// ── Authentication ────────────────────────────────────────────────────────
	authHandler := handlers.NewAuthHandler(
		cfg.Auth.Username,
		cfg.Auth.Password,
		rdb,
		cfg.Auth.SessionTTLHours,
	)
	sessionTTL := time.Duration(cfg.Auth.SessionTTLHours) * time.Hour
	authMiddleware := middleware.RequireAuth(rdb, sessionTTL)

	// Public auth endpoints — no token required.
	r.POST("/api/auth/login", authHandler.Login)
	r.POST("/api/auth/logout", authHandler.Logout) // reads token from header; no middleware needed

	// Protected auth check — requires valid token.
	r.GET("/api/auth/check", authMiddleware, authHandler.Check)

	// Register all main API routes with auth middleware.
	h.RegisterRoutes(r, authMiddleware)

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
