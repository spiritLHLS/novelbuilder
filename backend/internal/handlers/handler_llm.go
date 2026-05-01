package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

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

func validateCreateLLMProfileRequest(req models.CreateLLMProfileRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(req.Provider) == "" {
		return errors.New("provider is required")
	}
	if strings.TrimSpace(req.BaseURL) == "" {
		return errors.New("base_url is required")
	}
	if strings.TrimSpace(req.APIKey) == "" {
		return errors.New("api_key is required")
	}
	if strings.TrimSpace(req.ModelName) == "" {
		return errors.New("model_name is required")
	}
	if req.MaxTokens != 0 && req.MaxTokens < 100 {
		return fmt.Errorf("max_tokens must be at least 100 when provided")
	}
	if req.Temperature < 0 || req.Temperature > 2 {
		return fmt.Errorf("temperature must be between 0 and 2")
	}
	if req.RPMLimit < 0 {
		return fmt.Errorf("rpm_limit must be 0 or greater")
	}
	return nil
}

func validateUpdateLLMProfileRequest(req models.UpdateLLMProfileRequest) error {
	if req.Name != "" && strings.TrimSpace(req.Name) == "" {
		return errors.New("name cannot be blank")
	}
	if req.Provider != "" && strings.TrimSpace(req.Provider) == "" {
		return errors.New("provider cannot be blank")
	}
	if req.BaseURL != "" && strings.TrimSpace(req.BaseURL) == "" {
		return errors.New("base_url cannot be blank")
	}
	if req.APIKey != "" && strings.TrimSpace(req.APIKey) == "" {
		return errors.New("api_key cannot be blank")
	}
	if req.ModelName != "" && strings.TrimSpace(req.ModelName) == "" {
		return errors.New("model_name cannot be blank")
	}
	if req.MaxTokens != 0 && req.MaxTokens < 100 {
		return fmt.Errorf("max_tokens must be at least 100 when provided")
	}
	if req.Temperature != nil && (*req.Temperature < 0 || *req.Temperature > 2) {
		return fmt.Errorf("temperature must be between 0 and 2")
	}
	if req.RPMLimit != nil && *req.RPMLimit < 0 {
		return fmt.Errorf("rpm_limit must be 0 or greater")
	}
	return nil
}

func (h *Handler) CreateLLMProfile(c *gin.Context) {
	var req models.CreateLLMProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := validateCreateLLMProfileRequest(req); err != nil {
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
	if err := validateUpdateLLMProfileRequest(req); err != nil {
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

// isMaxTokensUnsupportedBody returns true when rawBody is an OpenAI-style 400 error
// indicating that "max_tokens" is not accepted for the requested model and the caller
// should use "max_completion_tokens" instead (e.g. o-series, certain new models).
func isMaxTokensUnsupportedBody(rawBody []byte) bool {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(rawBody, &body) != nil {
		return false
	}
	return body.Error.Code == "unsupported_parameter" ||
		(strings.Contains(body.Error.Message, "max_tokens") &&
			strings.Contains(body.Error.Message, "max_completion_tokens"))
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
	statusCode := resp.StatusCode

	// For chat/completions endpoints: if the provider rejects max_tokens and demands
	// max_completion_tokens (e.g. OpenAI o-series, certain new models), transparently
	// retry once with the correct parameter name.
	if statusCode == 400 &&
		!strings.HasSuffix(req.APIStyle, "/messages") &&
		!strings.HasSuffix(req.APIStyle, "/responses") &&
		req.APIStyle != "gemini" &&
		isMaxTokensUnsupportedBody(rawBody) {
		h.logger.Info("llm_test: max_tokens rejected, retrying with max_completion_tokens",
			zap.String("model", req.ModelName))
		fallbackMap := map[string]any{
			"model":                 req.ModelName,
			"max_completion_tokens": 16,
			"messages": []map[string]string{
				{"role": "user", "content": "Reply with the single word: ok"},
			},
		}
		if fbBytes, err2 := json.Marshal(fallbackMap); err2 == nil {
			if fbReq, err2 := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, endpoint, bytes.NewReader(fbBytes)); err2 == nil {
				fbReq.Header.Set("Content-Type", "application/json")
				fbReq.Header.Set("Authorization", "Bearer "+req.APIKey)
				if resp2, err2 := client.Do(fbReq); err2 == nil {
					rawBody2, _ := io.ReadAll(io.LimitReader(resp2.Body, 4096))
					resp2.Body.Close()
					rawBody = rawBody2
					statusCode = resp2.StatusCode
					durationMs = time.Since(start).Milliseconds()
				}
			}
		}
	}

	// Truncate for logging and response (first 500 bytes is enough to diagnose issues)
	rawBodySnippet := string(rawBody)
	if len(rawBodySnippet) > 500 {
		rawBodySnippet = rawBodySnippet[:500] + "..."
	}

	h.logger.Info("llm_test raw response",
		zap.String("endpoint", endpoint),
		zap.String("model", req.ModelName),
		zap.Int("status", statusCode),
		zap.Int64("duration_ms", durationMs),
		zap.String("raw_body", rawBodySnippet),
	)

	if statusCode >= 400 {
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
