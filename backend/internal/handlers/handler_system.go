package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// ── Health Checks ─────────────────────────────────────────────────────────────

func (h *Handler) Health(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok", "service": "novelbuilder"})
}

func (h *Handler) Doctor(c *gin.Context) {
	ctx := c.Request.Context()
	out := gin.H{
		"status":   "ok",
		"checks":   gin.H{},
		"warnings": []string{},
	}
	checks := out["checks"].(gin.H)
	warnings := out["warnings"].([]string)

	if err := h.projects.Ping(ctx); err != nil {
		checks["postgres"] = gin.H{"ok": false, "error": err.Error()}
		out["status"] = "degraded"
	} else {
		checks["postgres"] = gin.H{"ok": true}
	}

	if err := h.chapters.PingRedis(ctx); err != nil {
		checks["redis"] = gin.H{"ok": false, "error": err.Error()}
		warnings = append(warnings, "Redis 不可用：短期记忆与缓存能力降级")
		out["status"] = "degraded"
	} else {
		checks["redis"] = gin.H{"ok": true}
	}

	if _, err := h.sidecar.GetVectorStatus(ctx, "00000000-0000-0000-0000-000000000000"); err != nil {
		checks["qdrant"] = gin.H{"ok": false, "error": err.Error()}
		warnings = append(warnings, "Qdrant 检查失败：向量检索可能不可用")
		out["status"] = "degraded"
	} else {
		checks["qdrant"] = gin.H{"ok": true}
	}

	if _, err := h.sidecar.GetGraphEntities(ctx, "00000000-0000-0000-0000-000000000000"); err != nil {
		checks["neo4j"] = gin.H{"ok": false, "error": err.Error()}
		warnings = append(warnings, "Neo4j 检查失败：图记忆相关能力可能不可用")
		out["status"] = "degraded"
	} else {
		checks["neo4j"] = gin.H{"ok": true}
	}

	if profile, err := h.llmProfiles.GetDefault(ctx); err != nil {
		checks["llm_default_profile"] = gin.H{"ok": false, "error": err.Error()}
		out["status"] = "degraded"
	} else if profile == nil || profile.APIKey == "" {
		checks["llm_default_profile"] = gin.H{"ok": false, "error": "no default profile or empty api key"}
		warnings = append(warnings, "未配置默认 LLM，审计/生成能力将不可用")
		out["status"] = "degraded"
	} else {
		checks["llm_default_profile"] = gin.H{"ok": true, "model": profile.ModelName, "provider": profile.Provider}
	}

	out["warnings"] = warnings
	c.JSON(200, out)
}

// ── System Settings ───────────────────────────────────────────────────────────

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
	if err := h.systemSettings.Delete(c.Request.Context(), c.Param("key")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

// ── LLM Profiles ─────────────────────────────────────────────────────────────

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
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if profile == nil {
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
	t := true
	req := models.UpdateLLMProfileRequest{IsDefault: &t}
	profile, err := h.llmProfiles.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": profile})
}

// TestLLMProfile sends a minimal probe request to the provider to verify that the
// supplied credentials and endpoint are reachable and return a valid response.
// It accepts either an explicit api_key or falls back to looking up a saved
// profile by profile_id so callers can test without revealing the key to the
// frontend again.
func (h *Handler) TestLLMProfile(c *gin.Context) {
	var req struct {
		ProfileID string  `json:"profile_id"` // optional: re-test a saved profile
		BaseURL   string  `json:"base_url"`
		APIKey    string  `json:"api_key"`
		ModelName string  `json:"model_name"`
		APIStyle  string  `json:"api_style"` // "chat_completions" | "responses" | "claude" | "gemini"
		Provider  string  `json:"provider"`  // hint used to fill missing api_style
		MaxTokens int     `json:"max_tokens"`
		Temp      float64 `json:"temperature"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// If a profile_id is given and no inline key is present, load from DB for the key.
	if req.ProfileID != "" && req.APIKey == "" {
		full, err := h.llmProfiles.GetFull(c.Request.Context(), req.ProfileID)
		if err != nil || full == nil {
			c.JSON(404, gin.H{"error": "profile not found"})
			return
		}
		req.APIKey = full.APIKey
		if req.BaseURL == "" {
			req.BaseURL = full.BaseURL
		}
		if req.ModelName == "" {
			req.ModelName = full.ModelName
		}
		if req.APIStyle == "" {
			req.APIStyle = full.APIStyle
		}
		if req.Provider == "" {
			req.Provider = full.Provider
		}
	}

	if req.BaseURL == "" || req.APIKey == "" || req.ModelName == "" {
		c.JSON(400, gin.H{"error": "base_url, api_key and model_name are required"})
		return
	}

	// Infer api_style from provider when not explicitly set.
	if req.APIStyle == "" {
		switch req.Provider {
		case "anthropic":
			req.APIStyle = "/messages"
		case "gemini":
			req.APIStyle = "gemini"
		default:
			req.APIStyle = "/chat/completions"
		}
	}

	// Normalize legacy style names (stored before path-based values were introduced).
	switch req.APIStyle {
	case "chat_completions":
		req.APIStyle = "/chat/completions"
	case "responses":
		req.APIStyle = "/responses"
	case "claude":
		req.APIStyle = "/messages"
	}

	baseURL := strings.TrimRight(req.BaseURL, "/")
	start := time.Now()

	var (
		endpoint string
		bodyMap  map[string]any
	)

	switch {
	case strings.HasSuffix(req.APIStyle, "/responses"):
		// OpenAI Responses API: POST {base_url}/responses  or  {base_url}/v1/responses
		endpoint = baseURL + req.APIStyle
		bodyMap = map[string]any{
			"model": req.ModelName,
			"input": "Reply with the single word: ok",
		}
		if req.MaxTokens > 0 {
			bodyMap["max_output_tokens"] = req.MaxTokens
		}
	case strings.HasSuffix(req.APIStyle, "/messages"):
		// Anthropic Messages API: POST {base_url}/messages  or  {base_url}/v1/messages
		endpoint = baseURL + req.APIStyle
		bodyMap = map[string]any{
			"model":      req.ModelName,
			"max_tokens": 16,
			"messages": []map[string]string{
				{"role": "user", "content": "Reply with the single word: ok"},
			},
		}
	case req.APIStyle == "gemini":
		// Google Gemini REST API: POST {base_url}/models/{model}:generateContent?key=…
		endpoint = baseURL + "/models/" + req.ModelName + ":generateContent?key=" + req.APIKey
		bodyMap = map[string]any{
			"contents": []map[string]any{
				{
					"role":  "user",
					"parts": []map[string]string{{"text": "Reply with the single word: ok"}},
				},
			},
			"generationConfig": map[string]any{"maxOutputTokens": 16},
		}
	default:
		// Chat Completions: POST {base_url}/chat/completions  or  {base_url}/v1/chat/completions
		endpoint = baseURL + req.APIStyle
		bodyMap = map[string]any{
			"model":      req.ModelName,
			"max_tokens": 16,
			"messages": []map[string]string{
				{"role": "user", "content": "Reply with the single word: ok"},
			},
		}
	}

	bodyBytes, _ := json.Marshal(bodyMap)
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		c.JSON(500, gin.H{"ok": false, "error": fmt.Sprintf("build request: %v", err)})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Auth: Gemini uses key in query string; Anthropic uses x-api-key; others use Bearer.
	switch {
	case req.APIStyle == "gemini":
		// no Authorization header — key already embedded in URL
	case strings.HasSuffix(req.APIStyle, "/messages"):
		httpReq.Header.Set("x-api-key", req.APIKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	default:
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(200, gin.H{"ok": false, "error": fmt.Sprintf("连接失败: %v", err), "duration_ms": time.Since(start).Milliseconds()})
		return
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	durationMs := time.Since(start).Milliseconds()
	// Truncate for logging and response (first 500 bytes is enough to diagnose issues)
	rawBodySnippet := string(rawBody)
	if len(rawBodySnippet) > 500 {
		rawBodySnippet = rawBodySnippet[:500] + "..."
	}

	h.logger.Info("llm_test raw response",
		zap.String("endpoint", endpoint),
		zap.String("model", req.ModelName),
		zap.Int("status", resp.StatusCode),
		zap.Int64("duration_ms", durationMs),
		zap.String("raw_body", rawBodySnippet),
	)

	if resp.StatusCode >= 400 {
		// Try to extract human-readable error across different provider response shapes.
		var rawErr map[string]json.RawMessage
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if json.Unmarshal(rawBody, &rawErr) == nil {
			// OpenAI / compatible: {"error":{"message":"..."}}
			if errField, ok := rawErr["error"]; ok {
				var oaiErr struct {
					Message string `json:"message"`
				}
				if json.Unmarshal(errField, &oaiErr) == nil && oaiErr.Message != "" {
					errMsg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, oaiErr.Message)
				}
			}
			// Gemini: {"error":{"message":"...","status":"..."}}
			// (same shape as OpenAI, already handled above)
			// Anthropic: {"type":"error","error":{"type":"...","message":"..."}}
			if errMsg == fmt.Sprintf("HTTP %d", resp.StatusCode) {
				if errField, ok := rawErr["error"]; ok {
					var anthropicErr struct {
						Message string `json:"message"`
					}
					if json.Unmarshal(errField, &anthropicErr) == nil && anthropicErr.Message != "" {
						errMsg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, anthropicErr.Message)
					}
				}
			}
		}
		c.JSON(200, gin.H{"ok": false, "error": errMsg, "duration_ms": durationMs, "raw_body": rawBodySnippet})
		return
	}

	// Extract model name from response — each provider uses a different field.
	modelName := req.ModelName
	switch {
	case strings.HasSuffix(req.APIStyle, "/messages"):
		// Anthropic: {"model":"claude-...", ...}
		var parsed struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(rawBody, &parsed) == nil && parsed.Model != "" {
			modelName = parsed.Model
		}
	case req.APIStyle == "gemini":
		// Gemini does not echo back model name; use the requested name.
	default:
		var parsed struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(rawBody, &parsed) == nil && parsed.Model != "" {
			modelName = parsed.Model
		}
	}

	c.JSON(200, gin.H{"ok": true, "model": modelName, "duration_ms": durationMs, "raw_body": rawBodySnippet})
}

// ── Prompt Presets ────────────────────────────────────────────────────────────

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
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if preset == nil {
		c.JSON(404, gin.H{"error": "prompt preset not found"})
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

// ── Glossary ──────────────────────────────────────────────────────────────────

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

// ── Task Queue ────────────────────────────────────────────────────────────────

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
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(404, gin.H{"error": "task not found"})
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

// ── Resource Ledger ───────────────────────────────────────────────────────────

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
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if resource == nil {
		c.JSON(404, gin.H{"error": "resource not found"})
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

// ── Vocab Fatigue ─────────────────────────────────────────────────────────────

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

// ── Webhook Notifications ─────────────────────────────────────────────────────

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

// ── Genre Templates ───────────────────────────────────────────────────────────

func (h *Handler) ListGenreTemplates(c *gin.Context) {
	list, err := h.genreTemplates.List(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": list})
}

func (h *Handler) GetGenreTemplate(c *gin.Context) {
	t, err := h.genreTemplates.Get(c.Request.Context(), c.Param("genre"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if t == nil {
		c.JSON(404, gin.H{"error": "genre template not found"})
		return
	}
	c.JSON(200, gin.H{"data": t})
}

func (h *Handler) UpsertGenreTemplate(c *gin.Context) {
	var req models.UpsertGenreTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	t, err := h.genreTemplates.Upsert(c.Request.Context(), c.Param("genre"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": t})
}

func (h *Handler) DeleteGenreTemplate(c *gin.Context) {
	if err := h.genreTemplates.Delete(c.Request.Context(), c.Param("genre")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "deleted"})
}

// ── Service Logs ──────────────────────────────────────────────────────────────

// logSources maps service names (accepted via ?service=) to supervisord log file paths.
var logSources = map[string][]string{
	"go-backend":     {"/var/log/go-backend.log", "/var/log/go-backend_err.log"},
	"python-sidecar": {"/var/log/python-sidecar.log", "/var/log/python-sidecar_err.log"},
	"postgresql":     {"/var/log/postgresql.log", "/var/log/postgresql_err.log"},
	"redis":          {"/var/log/redis.log", "/var/log/redis_err.log"},
	"neo4j":          {"/var/log/neo4j.log", "/var/log/neo4j_err.log"},
	"qdrant":         {"/var/log/qdrant.log", "/var/log/qdrant_err.log"},
	"supervisord":    {"/var/log/supervisord.log"},
}

// GetServiceLogs returns the last N lines from a supervisord-managed service log.
//
//	GET /api/logs                         → {"services": [...]}
//	GET /api/logs?service=go-backend      → {"service":"...","lines":[...],"total":N}
//	GET /api/logs?service=go-backend&lines=500
func (h *Handler) GetServiceLogs(c *gin.Context) {
	service := strings.TrimSpace(c.Query("service"))

	if service == "" {
		names := make([]string, 0, len(logSources))
		for k := range logSources {
			names = append(names, k)
		}
		sort.Strings(names)
		c.JSON(200, gin.H{"services": names})
		return
	}

	paths, ok := logSources[service]
	if !ok {
		valid := make([]string, 0, len(logSources))
		for k := range logSources {
			valid = append(valid, k)
		}
		sort.Strings(valid)
		c.JSON(400, gin.H{"error": "unknown service; valid: " + strings.Join(valid, ", ")})
		return
	}

	maxLines := 200
	if n, err := strconv.Atoi(c.Query("lines")); err == nil && n > 0 {
		if n > 5000 {
			n = 5000
		}
		maxLines = n
	}

	var combined []string
	for _, fp := range paths {
		lines, _ := tailLogFile(fp, maxLines)
		combined = append(combined, lines...)
	}
	if len(combined) > maxLines {
		combined = combined[len(combined)-maxLines:]
	}

	c.JSON(200, gin.H{
		"service": service,
		"lines":   combined,
		"total":   len(combined),
	})
}

// tailLogFile reads the last n lines from a file by seeking from the end in
// 32 KiB chunks, avoiding loading large log files into memory.
func tailLogFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	if size == 0 {
		return nil, nil
	}

	const chunkSize = 32 * 1024
	var buf []byte
	pos := size
	for {
		readSize := int64(chunkSize)
		if readSize > pos {
			readSize = pos
		}
		pos -= readSize
		tmp := make([]byte, readSize)
		if _, err := f.ReadAt(tmp, pos); err != nil {
			return nil, err
		}
		buf = append(tmp, buf...)
		lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
		if len(lines) >= n || pos == 0 {
			if len(lines) > n {
				lines = lines[len(lines)-n:]
			}
			return lines, nil
		}
	}
}
