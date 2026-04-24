package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	anthropic "github.com/liushuangls/go-anthropic/v2"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
)

// ProfileResolver is a minimal interface for looking up the active LLM profile from the database.
// This avoids a circular import with the services package.
type ProfileResolver interface {
	GetDefault(ctx context.Context) (*models.LLMProfileFull, error)
}

// AIGateway routes all AI calls through the DB-configured default LLM profile.
// There is no config-file fallback; all model configuration lives in the database
// and is managed through the frontend Settings → AI 模型配置 page.
type AIGateway struct {
	profiles ProfileResolver
	sessions *redis.Client
	logger   *zap.Logger
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Task        string        `json:"task"`
	TaskType    string        `json:"task_type"`
	SessionID   string        `json:"session_id"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	ModelName   string        `json:"model_name"`
	Temperature float64       `json:"temperature"`
}

type ChatResponse struct {
	Content      string `json:"content"`
	Model        string `json:"model"`
	TokensUsed   int    `json:"tokens_used"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// resolvedModel holds all parameters needed to make an API call.
type resolvedModel struct {
	Provider        string
	APIStyle        string // normalized: "/chat/completions", "/messages", "/responses", "gemini", etc.
	APIKey          string
	ModelID         string // the model string sent to the provider (e.g. "gpt-4o")
	BaseURL         string
	MaxTokens       int
	Temperature     float64
	OmitMaxTokens   bool
	OmitTemperature bool
	ProfileName     string
	RPMLimit        int // 0 = unlimited
}

// ── Sliding-window RPM rate limiter ──────────────────────────────────────────

var (
	rpmMu      sync.Mutex
	rpmBuckets = map[string]*rpmBucket{}
)

type rpmBucket struct {
	mu         sync.Mutex
	timestamps []time.Time
}

type chatSessionState struct {
	Summary  string        `json:"summary,omitempty"`
	Messages []ChatMessage `json:"messages,omitempty"`
}

type sessionContextKey string

const (
	chatSessionTTL          = 24 * time.Hour
	chatSessionMaxMessages  = 8
	chatSessionKeepRecent   = 4
	chatSessionMaxRunes     = 12000
	chatSessionSummaryRunes = 4000
	chatSessionExcerptRunes = 240
	sessionIDContextKey     = sessionContextKey("ai_gateway_session_id")
)

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	resolved := normalizeSessionID(sessionID)
	if resolved == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDContextKey, resolved)
}

func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, ok := ctx.Value(sessionIDContextKey).(string)
	if !ok {
		return ""
	}
	return normalizeSessionID(value)
}

// rpmWait blocks until a request slot is available within the rpm limit.
// key should be "baseURL|modelID" to scope the counter per endpoint+model.
func rpmWait(key string, limit int, logger *zap.Logger) {
	if limit <= 0 {
		return
	}
	rpmMu.Lock()
	b, ok := rpmBuckets[key]
	if !ok {
		b = &rpmBucket{}
		rpmBuckets[key] = b
	}
	rpmMu.Unlock()

	for {
		b.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-60 * time.Second)
		var fresh []time.Time
		for _, t := range b.timestamps {
			if t.After(cutoff) {
				fresh = append(fresh, t)
			}
		}
		b.timestamps = fresh

		if len(b.timestamps) < limit {
			b.timestamps = append(b.timestamps, now)
			b.mu.Unlock()
			return
		}

		// Wait until the oldest timestamp exits the 60-second window.
		waitDur := b.timestamps[0].Add(60*time.Second).Sub(now) + 50*time.Millisecond
		b.mu.Unlock()

		if logger != nil {
			logger.Debug("RPM limit reached, waiting",
				zap.String("key", key),
				zap.Int("limit", limit),
				zap.Duration("wait", waitDur))
		}
		time.Sleep(waitDur)
	}
}

func NewAIGateway(profiles ProfileResolver, sessions *redis.Client, logger *zap.Logger) *AIGateway {
	return &AIGateway{profiles: profiles, sessions: sessions, logger: logger}
}

// normalizeAPIStyle converts legacy style names to path-based values.
func normalizeAPIStyle(s string) string {
	switch s {
	case "chat_completions":
		return "/chat/completions"
	case "claude":
		return "/messages"
	case "responses":
		return "/responses"
	}
	return s
}

// apiProtocol returns the dispatch protocol key based on api_style,
// falling back to provider when api_style is empty.
func apiProtocol(apiStyle, provider string) string {
	if apiStyle != "" {
		if strings.HasSuffix(apiStyle, "/messages") {
			return "anthropic"
		}
		if strings.HasSuffix(apiStyle, "/responses") {
			return "responses"
		}
		if apiStyle == "gemini" {
			return "gemini"
		}
		return "openai"
	}
	if provider == "anthropic" {
		return "anthropic"
	}
	return "openai"
}

// openaiSDKBase strips the /chat/completions suffix from (baseURL+apiStyle)
// so the OpenAI SDK targets the correct endpoint for both "/chat/completions"
// and "/v1/chat/completions" api_style values.
func openaiSDKBase(baseURL, apiStyle string) string {
	if apiStyle == "" {
		return baseURL
	}
	total := strings.TrimRight(baseURL, "/") + apiStyle
	if r := strings.TrimSuffix(total, "/chat/completions"); r != total {
		return r
	}
	return baseURL
}

// anthropicSDKBase strips /messages from (baseURL+apiStyle) so the Anthropic SDK
// targets the correct endpoint for both "/messages" and "/v1/messages" styles.
func anthropicSDKBase(baseURL, apiStyle string) string {
	if baseURL == "" {
		return "https://api.anthropic.com/v1"
	}
	if apiStyle == "" {
		return baseURL
	}
	total := strings.TrimRight(baseURL, "/") + apiStyle
	if r := strings.TrimSuffix(total, "/messages"); r != total {
		return r
	}
	return baseURL
}

// resolveModel returns the model parameters from the DB default profile.
func (gw *AIGateway) resolveModel(ctx context.Context, req ChatRequest) (resolvedModel, error) {
	profile, err := gw.profiles.GetDefault(ctx)
	if err != nil {
		return resolvedModel{}, fmt.Errorf("fetch default LLM profile: %w", err)
	}
	if profile == nil {
		return resolvedModel{}, fmt.Errorf(
			"no default AI model configured — please add a profile in Settings → AI 模型配置 and mark it as default")
	}
	resolved := resolvedModel{
		Provider:        profile.Provider,
		APIStyle:        normalizeAPIStyle(profile.APIStyle),
		APIKey:          profile.APIKey,
		ModelID:         profile.ModelName,
		BaseURL:         profile.BaseURL,
		MaxTokens:       profile.MaxTokens,
		Temperature:     profile.Temperature,
		OmitMaxTokens:   profile.OmitMaxTokens,
		OmitTemperature: profile.OmitTemperature,
		ProfileName:     profile.Name,
		RPMLimit:        profile.RPMLimit,
	}
	if req.MaxTokens > 0 {
		resolved.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		resolved.Temperature = req.Temperature
	}
	return resolved, nil
}

func (gw *AIGateway) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	resolved, err := gw.resolveModel(ctx, req)
	if err != nil {
		return nil, err
	}
	sessionID := normalizeSessionID(req.SessionID)
	if sessionID == "" {
		sessionID = SessionIDFromContext(ctx)
	}
	preparedMessages := gw.prepareSessionMessages(ctx, sessionID, req.Messages)
	gw.logger.Info("AI Chat request",
		zap.String("profile", resolved.ProfileName),
		zap.String("model", resolved.ModelID),
		zap.String("api_style", resolved.APIStyle),
		zap.String("task", req.Task),
		zap.String("session_id", sessionID),
		zap.Int("messages", len(preparedMessages)))

	response, err := gw.dispatchChat(ctx, resolved, preparedMessages)
	if err != nil {
		return nil, err
	}
	gw.persistSessionMessages(sessionID, req.Messages, response.Content)
	return response, nil
}

func (gw *AIGateway) dispatchChat(ctx context.Context, resolved resolvedModel, msgs []ChatMessage) (*ChatResponse, error) {
	switch apiProtocol(resolved.APIStyle, resolved.Provider) {
	case "openai":
		return gw.chatOpenAI(ctx, resolved, msgs)
	case "anthropic":
		return gw.chatAnthropic(ctx, resolved, msgs)
	case "responses":
		return gw.chatResponses(ctx, resolved, msgs)
	case "gemini":
		return gw.chatGemini(ctx, resolved, msgs)
	}
	return nil, fmt.Errorf("unsupported api_style: %q (provider: %s)", resolved.APIStyle, resolved.Provider)
}

// resolvedFromCfg builds a resolvedModel from an explicit credentials map.
func resolvedFromCfg(cfg map[string]interface{}, req ChatRequest) resolvedModel {
	apiKey, _ := cfg["api_key"].(string)
	model, _ := cfg["model"].(string)
	baseURL, _ := cfg["base_url"].(string)
	provider, _ := cfg["provider"].(string)
	rawStyle, _ := cfg["api_style"].(string)
	apiStyle := normalizeAPIStyle(rawStyle)
	omitMaxTokens, _ := cfg["omit_max_tokens"].(bool)
	omitTemperature, _ := cfg["omit_temperature"].(bool)

	if provider == "" {
		switch {
		case baseURL == "":
			provider = "openai"
		case strings.HasPrefix(baseURL, "https://api.anthropic.com"):
			provider = "anthropic"
		default:
			provider = "openai_compatible"
		}
	}

	maxTokens := 4096
	if v, ok := cfg["max_tokens"]; ok {
		switch mv := v.(type) {
		case int:
			maxTokens = mv
		case float64:
			maxTokens = int(mv)
		}
	}
	temperature := 0.7
	if v, ok := cfg["temperature"]; ok {
		if tv, ok := v.(float64); ok {
			temperature = tv
		}
	}
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		temperature = req.Temperature
	}
	rpmLimit := 0
	if v, ok := cfg["rpm_limit"]; ok {
		switch rv := v.(type) {
		case int:
			rpmLimit = rv
		case float64:
			rpmLimit = int(rv)
		}
	}

	return resolvedModel{
		Provider:        provider,
		APIStyle:        apiStyle,
		APIKey:          apiKey,
		ModelID:         model,
		BaseURL:         baseURL,
		MaxTokens:       maxTokens,
		Temperature:     temperature,
		OmitMaxTokens:   omitMaxTokens,
		OmitTemperature: omitTemperature,
		RPMLimit:        rpmLimit,
	}
}

// ChatWithConfig performs a chat using an explicitly provided credentials map.
// Falls back to Chat() if cfg is nil or lacks an api_key.
func (gw *AIGateway) ChatWithConfig(ctx context.Context, req ChatRequest, cfg map[string]interface{}) (*ChatResponse, error) {
	if cfg == nil {
		return gw.Chat(ctx, req)
	}
	apiKey, _ := cfg["api_key"].(string)
	if apiKey == "" {
		return gw.Chat(ctx, req)
	}
	resolved := resolvedFromCfg(cfg, req)
	sessionID := normalizeSessionID(sessionIDFromConfig(cfg))
	if sessionID == "" {
		sessionID = normalizeSessionID(req.SessionID)
	}
	if sessionID == "" {
		sessionID = SessionIDFromContext(ctx)
	}
	preparedMessages := gw.prepareSessionMessages(ctx, sessionID, req.Messages)
	response, err := gw.dispatchChat(ctx, resolved, preparedMessages)
	if err != nil {
		return nil, err
	}
	gw.persistSessionMessages(sessionID, req.Messages, response.Content)
	return response, nil
}

func normalizeSessionID(sessionID string) string {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" || trimmed == "<nil>" {
		return ""
	}
	return trimmed
}

func sessionIDFromConfig(cfg map[string]interface{}) string {
	if cfg == nil {
		return ""
	}
	return normalizeSessionID(fmt.Sprint(cfg["session_id"]))
}

func (gw *AIGateway) prepareSessionMessages(ctx context.Context, sessionID string, current []ChatMessage) []ChatMessage {
	if gw.sessions == nil || sessionID == "" {
		return current
	}

	state, err := gw.loadSessionState(ctx, sessionID)
	if err != nil {
		gw.logger.Debug("failed to load chat session state",
			zap.String("session_id", sessionID),
			zap.Error(err))
		return current
	}
	if state == nil {
		return current
	}

	gw.compactSessionState(state)
	if len(state.Messages) == 0 && strings.TrimSpace(state.Summary) == "" {
		return current
	}

	var systemMessages []ChatMessage
	var otherMessages []ChatMessage
	for _, msg := range current {
		if msg.Role == "system" {
			systemMessages = append(systemMessages, msg)
			continue
		}
		otherMessages = append(otherMessages, msg)
	}

	prepared := append([]ChatMessage{}, systemMessages...)
	if summary := strings.TrimSpace(state.Summary); summary != "" {
		prepared = append(prepared, ChatMessage{
			Role:    "system",
			Content: "以下是当前同一任务 session 的压缩历史摘要，请保持连续性并避免重复展开已完成内容：\n" + summary,
		})
	}
	prepared = append(prepared, state.Messages...)
	prepared = append(prepared, otherMessages...)
	return prepared
}

func (gw *AIGateway) persistSessionMessages(sessionID string, current []ChatMessage, assistantReply string) {
	if gw.sessions == nil || sessionID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := gw.chatSessionKey(sessionID)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		err := gw.sessions.Watch(ctx, func(tx *redis.Tx) error {
			state := &chatSessionState{}
			raw, err := tx.Get(ctx, key).Result()
			switch {
			case errors.Is(err, redis.Nil):
				// no prior state
			case err != nil:
				return err
			default:
				if err := json.Unmarshal([]byte(raw), state); err != nil {
					return err
				}
			}

			for _, msg := range current {
				if msg.Role == "system" {
					continue
				}
				state.Messages = append(state.Messages, ChatMessage{Role: msg.Role, Content: msg.Content})
			}
			if reply := strings.TrimSpace(assistantReply); reply != "" {
				state.Messages = append(state.Messages, ChatMessage{Role: "assistant", Content: reply})
			}

			gw.compactSessionState(state)
			payload, err := json.Marshal(state)
			if err != nil {
				return err
			}

			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, payload, chatSessionTTL)
				return nil
			})
			return err
		}, key)
		if err == nil {
			return
		}
		lastErr = err
		if err != redis.TxFailedErr {
			break
		}
	}

	gw.logger.Debug("failed to save chat session state",
		zap.String("session_id", sessionID),
		zap.Error(lastErr))
}

func (gw *AIGateway) loadSessionState(ctx context.Context, sessionID string) (*chatSessionState, error) {
	raw, err := gw.sessions.Get(ctx, gw.chatSessionKey(sessionID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state chatSessionState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (gw *AIGateway) saveSessionState(ctx context.Context, sessionID string, state *chatSessionState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return gw.sessions.Set(ctx, gw.chatSessionKey(sessionID), payload, chatSessionTTL).Err()
}

func (gw *AIGateway) chatSessionKey(sessionID string) string {
	return "ai_gateway_session:" + sessionID
}

func (gw *AIGateway) compactSessionState(state *chatSessionState) {
	for len(state.Messages) > chatSessionMaxMessages || totalMessageRunes(state.Messages)+len([]rune(state.Summary)) > chatSessionMaxRunes {
		if len(state.Messages) <= chatSessionKeepRecent {
			break
		}
		archiveCount := len(state.Messages) - chatSessionKeepRecent
		archived := append([]ChatMessage(nil), state.Messages[:archiveCount]...)
		state.Messages = append([]ChatMessage(nil), state.Messages[archiveCount:]...)
		state.Summary = extendSessionSummary(state.Summary, archived)
	}
	if len([]rune(state.Summary)) > chatSessionSummaryRunes {
		runes := []rune(state.Summary)
		state.Summary = string(runes[len(runes)-chatSessionSummaryRunes:])
	}
}

func extendSessionSummary(existing string, archived []ChatMessage) string {
	var builder strings.Builder
	if summary := strings.TrimSpace(existing); summary != "" {
		builder.WriteString(summary)
		builder.WriteString("\n")
	}
	for _, msg := range archived {
		builder.WriteString(msg.Role)
		builder.WriteString(": ")
		builder.WriteString(compactMessageContent(msg.Content))
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func compactMessageContent(content string) string {
	compact := strings.Join(strings.Fields(content), " ")
	runes := []rune(compact)
	if len(runes) > chatSessionExcerptRunes {
		return string(runes[:chatSessionExcerptRunes]) + "..."
	}
	return compact
}

func totalMessageRunes(messages []ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += len([]rune(msg.Content))
	}
	return total
}

// ── OpenAI / Chat Completions ──────────────────────────────────────────────

func (gw *AIGateway) openaiClient(r resolvedModel) *openai.Client {
	clientCfg := openai.DefaultConfig(r.APIKey)
	if r.BaseURL != "" {
		clientCfg.BaseURL = openaiSDKBase(r.BaseURL, r.APIStyle)
	}
	return openai.NewClientWithConfig(clientCfg)
}

func (gw *AIGateway) chatOpenAI(ctx context.Context, r resolvedModel, msgs []ChatMessage) (*ChatResponse, error) {
	rpmWait(r.BaseURL+"|"+r.ModelID, r.RPMLimit, gw.logger)
	oaiMsgs := make([]openai.ChatCompletionMessage, len(msgs))
	for i, m := range msgs {
		oaiMsgs[i] = openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
	}
	reqBody := openai.ChatCompletionRequest{Model: r.ModelID, Messages: oaiMsgs}
	if !r.OmitMaxTokens {
		reqBody.MaxTokens = r.MaxTokens
	}
	if !r.OmitTemperature {
		reqBody.Temperature = float32(r.Temperature)
	}
	resp, err := gw.openaiClient(r).CreateChatCompletion(ctx, reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("no choices in response")
	}
	return &ChatResponse{
		Content:      resp.Choices[0].Message.Content,
		Model:        r.ProfileName,
		TokensUsed:   resp.Usage.TotalTokens,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}, nil
}

// ── Anthropic / Messages ──────────────────────────────────────────────

func (gw *AIGateway) anthropicClient(r resolvedModel) *anthropic.Client {
	return anthropic.NewClient(r.APIKey, anthropic.WithBaseURL(anthropicSDKBase(r.BaseURL, r.APIStyle)))
}

func buildAnthropicRequest(r resolvedModel, msgs []ChatMessage) anthropic.MessagesRequest {
	var systemMsg string
	var anthMsgs []anthropic.Message
	for _, m := range msgs {
		switch m.Role {
		case "system":
			systemMsg = m.Content
		case "user":
			anthMsgs = append(anthMsgs, anthropic.NewUserTextMessage(m.Content))
		case "assistant":
			anthMsgs = append(anthMsgs, anthropic.NewAssistantTextMessage(m.Content))
		}
	}
	maxTokens := r.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	req := anthropic.MessagesRequest{
		Model:     r.ModelID,
		MaxTokens: maxTokens,
		Messages:  anthMsgs,
		System:    systemMsg,
	}
	if !r.OmitTemperature {
		t := float32(r.Temperature)
		req.Temperature = &t
	}
	return req
}

func (gw *AIGateway) chatAnthropic(ctx context.Context, r resolvedModel, msgs []ChatMessage) (*ChatResponse, error) {
	rpmWait(r.BaseURL+"|"+r.ModelID, r.RPMLimit, gw.logger)
	resp, err := gw.anthropicClient(r).CreateMessages(ctx, buildAnthropicRequest(r, msgs))
	if err != nil {
		return nil, fmt.Errorf("anthropic chat: %w", err)
	}
	return &ChatResponse{
		Content:      resp.GetFirstContentText(),
		Model:        r.ProfileName,
		TokensUsed:   resp.Usage.InputTokens + resp.Usage.OutputTokens,
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
	}, nil
}

// ── OpenAI Responses API ────────────────────────────────────────────

func buildResponsesBody(r resolvedModel, msgs []ChatMessage) map[string]interface{} {
	var instructions string
	var inputParts []map[string]interface{}
	for _, m := range msgs {
		if m.Role == "system" {
			instructions = m.Content
			continue
		}
		inputParts = append(inputParts, map[string]interface{}{"role": m.Role, "content": m.Content})
	}
	body := map[string]interface{}{"model": r.ModelID, "input": inputParts}
	if instructions != "" {
		body["instructions"] = instructions
	}
	if !r.OmitMaxTokens {
		body["max_output_tokens"] = r.MaxTokens
	}
	if !r.OmitTemperature {
		body["temperature"] = r.Temperature
	}
	return body
}

func (gw *AIGateway) chatResponses(ctx context.Context, r resolvedModel, msgs []ChatMessage) (*ChatResponse, error) {
	rpmWait(r.BaseURL+"|"+r.ModelID, r.RPMLimit, gw.logger)
	bodyBytes, err := json.Marshal(buildResponsesBody(r, msgs))
	if err != nil {
		return nil, fmt.Errorf("responses marshal: %w", err)
	}
	endpoint := strings.TrimRight(r.BaseURL, "/") + r.APIStyle
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+r.APIKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("responses api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("responses api error %d: %s", resp.StatusCode, string(b))
	}
	var result struct {
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("responses decode: %w", err)
	}
	var content string
	for _, o := range result.Output {
		for _, c := range o.Content {
			content += c.Text
		}
	}
	return &ChatResponse{
		Content:      content,
		Model:        r.ProfileName,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		TokensUsed:   result.Usage.InputTokens + result.Usage.OutputTokens,
	}, nil
}

// ── Google Gemini ───────────────────────────────────────────────

func buildGeminiBody(r resolvedModel, msgs []ChatMessage) map[string]interface{} {
	var sysInstruction string
	var contents []map[string]interface{}
	for _, m := range msgs {
		if m.Role == "system" {
			sysInstruction = m.Content
			continue
		}
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]interface{}{
			"role":  role,
			"parts": []map[string]string{{"text": m.Content}},
		})
	}
	body := map[string]interface{}{"contents": contents}
	if sysInstruction != "" {
		body["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]string{{"text": sysInstruction}},
		}
	}
	if !r.OmitMaxTokens || !r.OmitTemperature {
		genCfg := map[string]interface{}{}
		if !r.OmitMaxTokens {
			genCfg["maxOutputTokens"] = r.MaxTokens
		}
		if !r.OmitTemperature {
			genCfg["temperature"] = r.Temperature
		}
		body["generationConfig"] = genCfg
	}
	return body
}

func (gw *AIGateway) chatGemini(ctx context.Context, r resolvedModel, msgs []ChatMessage) (*ChatResponse, error) {
	rpmWait(r.BaseURL+"|"+r.ModelID, r.RPMLimit, gw.logger)
	bodyBytes, err := json.Marshal(buildGeminiBody(r, msgs))
	if err != nil {
		return nil, fmt.Errorf("gemini marshal: %w", err)
	}
	endpoint := strings.TrimRight(r.BaseURL, "/") + "/models/" + r.ModelID + ":generateContent?key=" + r.APIKey
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini error %d: %s", resp.StatusCode, string(b))
	}
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("gemini decode: %w", err)
	}
	var content string
	for _, c := range result.Candidates {
		for _, p := range c.Content.Parts {
			content += p.Text
		}
	}
	return &ChatResponse{
		Content:      content,
		Model:        r.ProfileName,
		InputTokens:  result.UsageMetadata.PromptTokenCount,
		OutputTokens: result.UsageMetadata.CandidatesTokenCount,
		TokensUsed:   result.UsageMetadata.PromptTokenCount + result.UsageMetadata.CandidatesTokenCount,
	}, nil
}
