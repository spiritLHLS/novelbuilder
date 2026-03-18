package main

import (
	"context"
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
	"github.com/novelbuilder/backend/internal/services"
	"github.com/novelbuilder/backend/internal/workflow"
	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Load config
	cfg, err := config.Load(logger)
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

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

	// Initialize AI Gateway (DB profiles take priority over config-file models)
	llmProfileService := services.NewLLMProfileService(db, cfg.Security.EncryptionKey, logger)
	aiGateway := gateway.NewAIGateway(cfg.AIGateway, llmProfileService, logger)

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

	// Start background task worker pool
	taskQueueService.Start()
	defer taskQueueService.Stop()

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
		logger,
	)

	// Setup Gin router
	r := gin.Default()

	// CORS middleware
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Idempotency-Key", "X-Request-Id"},
		ExposeHeaders:    []string{"Content-Length", "X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.Use(middleware.RequestID())
	r.Use(middleware.Logger(logger))

	// SSE middleware for streaming endpoints
	r.Use(func(c *gin.Context) {
		if c.Request.URL.Path == "/api/projects/"+c.Param("id")+"/chapters/stream" {
			c.Header("Cache-Control", "no-cache")
			c.Header("Content-Type", "text/event-stream")
			c.Header("Connection", "keep-alive")
			c.Header("X-Accel-Buffering", "no")
		}
		c.Next()
	})

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
