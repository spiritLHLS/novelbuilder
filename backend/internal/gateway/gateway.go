package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"

	anthropic "github.com/liushuangls/go-anthropic/v2"
	"github.com/novelbuilder/backend/internal/models"
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
	logger   *zap.Logger
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Task        string        `json:"task"`
	TaskType    string        `json:"task_type"`
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

type StreamChunk struct {
	Content string `json:"content"`
	Done    bool   `json:"done"`
}

// resolvedModel holds all parameters needed to make an API call.
type resolvedModel struct {
	Provider    string
	APIKey      string
	ModelID     string // the model string sent to the provider (e.g. "gpt-4o")
	BaseURL     string
	MaxTokens   int
	Temperature float64
	ProfileName string
}

func NewAIGateway(profiles ProfileResolver, logger *zap.Logger) *AIGateway {
	return &AIGateway{
		profiles: profiles,
		logger:   logger,
	}
}

// buildClientForProfile creates an API client for the given profile on-demand.
func buildClientForProfile(p *models.LLMProfileFull) interface{} {
	switch p.Provider {
	case "anthropic":
		return anthropic.NewClient(p.APIKey)
	default: // openai, openai_compatible, deepseek, etc.
		clientCfg := openai.DefaultConfig(p.APIKey)
		if p.BaseURL != "" {
			clientCfg.BaseURL = p.BaseURL
		}
		return openai.NewClientWithConfig(clientCfg)
	}
}

// resolveModel returns the model parameters from the DB default profile.
// Returns a clear error if no default profile has been configured yet.
func (gw *AIGateway) resolveModel(ctx context.Context, req ChatRequest) (resolvedModel, interface{}, error) {
	profile, err := gw.profiles.GetDefault(ctx)
	if err != nil {
		return resolvedModel{}, nil, fmt.Errorf("fetch default LLM profile: %w", err)
	}
	if profile == nil {
		return resolvedModel{}, nil, fmt.Errorf(
			"no default AI model configured — please add a profile in Settings → AI 模型配置 and mark it as default")
	}

	resolved := resolvedModel{
		Provider:    profile.Provider,
		APIKey:      profile.APIKey,
		ModelID:     profile.ModelName,
		BaseURL:     profile.BaseURL,
		MaxTokens:   profile.MaxTokens,
		Temperature: profile.Temperature,
		ProfileName: profile.Name,
	}
	if req.MaxTokens > 0 {
		resolved.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		resolved.Temperature = req.Temperature
	}
	return resolved, buildClientForProfile(profile), nil
}

func (gw *AIGateway) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	resolved, client, err := gw.resolveModel(ctx, req)
	if err != nil {
		return nil, err
	}

	gw.logger.Info("AI Chat request",
		zap.String("profile", resolved.ProfileName),
		zap.String("model", resolved.ModelID),
		zap.String("task", req.Task),
		zap.Int("messages", len(req.Messages)))

	switch resolved.Provider {
	case "openai", "deepseek", "openai_compatible":
		oaiClient := client.(*openai.Client)
		msgs := make([]openai.ChatCompletionMessage, len(req.Messages))
		for i, m := range req.Messages {
			msgs[i] = openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
		}
		resp, err := oaiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:       resolved.ModelID,
			Messages:    msgs,
			MaxTokens:   resolved.MaxTokens,
			Temperature: float32(resolved.Temperature),
		})
		if err != nil {
			return nil, fmt.Errorf("openai chat: %w", err)
		}
		if len(resp.Choices) == 0 {
			return nil, errors.New("no choices in response")
		}
		return &ChatResponse{
			Content:      resp.Choices[0].Message.Content,
			Model:        resolved.ProfileName,
			TokensUsed:   resp.Usage.TotalTokens,
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}, nil

	case "anthropic":
		anthClient := client.(*anthropic.Client)
		var systemMsg string
		var anthMsgs []anthropic.Message
		for _, m := range req.Messages {
			if m.Role == "system" {
				systemMsg = m.Content
				continue
			}
			if m.Role == "user" {
				anthMsgs = append(anthMsgs, anthropic.NewUserTextMessage(m.Content))
			} else if m.Role == "assistant" {
				anthMsgs = append(anthMsgs, anthropic.NewAssistantTextMessage(m.Content))
			}
		}
		resp, err := anthClient.CreateMessages(ctx, anthropic.MessagesRequest{
			Model:     resolved.ModelID,
			MaxTokens: resolved.MaxTokens,
			Messages:  anthMsgs,
			System:    systemMsg,
		})
		if err != nil {
			return nil, fmt.Errorf("anthropic chat: %w", err)
		}
		return &ChatResponse{
			Content:      resp.GetFirstContentText(),
			Model:        resolved.ProfileName,
			TokensUsed:   resp.Usage.InputTokens + resp.Usage.OutputTokens,
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		}, nil
	}

	return nil, fmt.Errorf("unsupported provider: %s", resolved.Provider)
}

func (gw *AIGateway) ChatStream(ctx context.Context, req ChatRequest, handler func(chunk string) error) error {
	resolved, client, err := gw.resolveModel(ctx, req)
	if err != nil {
		return err
	}

	switch resolved.Provider {
	case "openai", "deepseek", "openai_compatible":
		oaiClient := client.(*openai.Client)
		msgs := make([]openai.ChatCompletionMessage, len(req.Messages))
		for i, m := range req.Messages {
			msgs[i] = openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
		}
		stream, err := oaiClient.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
			Model:       resolved.ModelID,
			Messages:    msgs,
			MaxTokens:   resolved.MaxTokens,
			Temperature: float32(resolved.Temperature),
			Stream:      true,
		})
		if err != nil {
			return fmt.Errorf("openai stream: %w", err)
		}
		defer stream.Close()

		for {
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("stream recv: %w", err)
			}
			if len(resp.Choices) > 0 {
				delta := resp.Choices[0].Delta.Content
				if delta != "" {
					if err := handler(delta); err != nil {
						return err
					}
				}
			}
		}

	case "anthropic":
		anthClient := client.(*anthropic.Client)
		var systemMsg string
		var anthMsgs []anthropic.Message
		for _, m := range req.Messages {
			if m.Role == "system" {
				systemMsg = m.Content
				continue
			}
			if m.Role == "user" {
				anthMsgs = append(anthMsgs, anthropic.NewUserTextMessage(m.Content))
			} else if m.Role == "assistant" {
				anthMsgs = append(anthMsgs, anthropic.NewAssistantTextMessage(m.Content))
			}
		}

		var streamErr error
		_, err := anthClient.CreateMessagesStream(ctx, anthropic.MessagesStreamRequest{
			MessagesRequest: anthropic.MessagesRequest{
				Model:     resolved.ModelID,
				MaxTokens: resolved.MaxTokens,
				Messages:  anthMsgs,
				System:    systemMsg,
			},
			OnContentBlockDelta: func(data anthropic.MessagesEventContentBlockDeltaData) {
				text := data.Delta.GetText()
				if text != "" {
					if err := handler(text); err != nil {
						streamErr = err
					}
				}
			},
		})
		if err != nil {
			return fmt.Errorf("anthropic stream: %w", err)
		}
		if streamErr != nil {
			return streamErr
		}
		return nil
	}

	return fmt.Errorf("unsupported provider: %s", resolved.Provider)
}

// ChatWithConfig performs a chat using an explicitly provided credentials map
// (api_key, model, base_url, provider, max_tokens, temperature).
// If cfg is nil or lacks an api_key, it transparently falls back to Chat()
// with the database-configured default profile.
func (gw *AIGateway) ChatWithConfig(ctx context.Context, req ChatRequest, cfg map[string]interface{}) (*ChatResponse, error) {
	if cfg == nil {
		return gw.Chat(ctx, req)
	}
	apiKey, _ := cfg["api_key"].(string)
	if apiKey == "" {
		return gw.Chat(ctx, req)
	}

	model, _ := cfg["model"].(string)
	baseURL, _ := cfg["base_url"].(string)
	provider, _ := cfg["provider"].(string)
	if provider == "" {
		// Infer provider from base_url heuristic
		if baseURL == "" || len(baseURL) == 0 {
			provider = "openai"
		} else if len(baseURL) >= 25 && baseURL[:25] == "https://api.anthropic.com" {
			provider = "anthropic"
		} else {
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

	resolved := resolvedModel{
		Provider:    provider,
		APIKey:      apiKey,
		ModelID:     model,
		BaseURL:     baseURL,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}

	switch resolved.Provider {
	case "openai", "deepseek", "openai_compatible":
		clientCfg := openai.DefaultConfig(resolved.APIKey)
		if resolved.BaseURL != "" {
			clientCfg.BaseURL = resolved.BaseURL
		}
		oaiClient := openai.NewClientWithConfig(clientCfg)
		msgs := make([]openai.ChatCompletionMessage, len(req.Messages))
		for i, m := range req.Messages {
			msgs[i] = openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
		}
		resp, err := oaiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:       resolved.ModelID,
			Messages:    msgs,
			MaxTokens:   resolved.MaxTokens,
			Temperature: float32(resolved.Temperature),
		})
		if err != nil {
			return nil, fmt.Errorf("openai chat (custom config): %w", err)
		}
		if len(resp.Choices) == 0 {
			return nil, errors.New("no choices in response")
		}
		return &ChatResponse{
			Content:      resp.Choices[0].Message.Content,
			TokensUsed:   resp.Usage.TotalTokens,
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}, nil

	case "anthropic":
		anthClient := anthropic.NewClient(resolved.APIKey)
		var systemMsg string
		var anthMsgs []anthropic.Message
		for _, m := range req.Messages {
			if m.Role == "system" {
				systemMsg = m.Content
				continue
			}
			if m.Role == "user" {
				anthMsgs = append(anthMsgs, anthropic.NewUserTextMessage(m.Content))
			} else if m.Role == "assistant" {
				anthMsgs = append(anthMsgs, anthropic.NewAssistantTextMessage(m.Content))
			}
		}
		resp, err := anthClient.CreateMessages(ctx, anthropic.MessagesRequest{
			Model:     resolved.ModelID,
			MaxTokens: resolved.MaxTokens,
			Messages:  anthMsgs,
			System:    systemMsg,
		})
		if err != nil {
			return nil, fmt.Errorf("anthropic chat (custom config): %w", err)
		}
		return &ChatResponse{
			Content:      resp.GetFirstContentText(),
			TokensUsed:   resp.Usage.InputTokens + resp.Usage.OutputTokens,
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		}, nil
	}

	return nil, fmt.Errorf("unsupported provider: %s", resolved.Provider)
}

// ChatStreamWithConfig is the streaming variant of ChatWithConfig.
// For openai_compatible providers, if the streaming call fails it automatically
// falls back to a synchronous (non-streamed) completion to handle providers
// that do not support SSE.
func (gw *AIGateway) ChatStreamWithConfig(ctx context.Context, req ChatRequest, cfg map[string]interface{}, handler func(chunk string) error) error {
	if cfg == nil {
		return gw.ChatStream(ctx, req, handler)
	}
	apiKey, _ := cfg["api_key"].(string)
	if apiKey == "" {
		return gw.ChatStream(ctx, req, handler)
	}

	model, _ := cfg["model"].(string)
	baseURL, _ := cfg["base_url"].(string)
	provider, _ := cfg["provider"].(string)
	if provider == "" {
		if baseURL == "" {
			provider = "openai"
		} else if len(baseURL) >= 25 && baseURL[:25] == "https://api.anthropic.com" {
			provider = "anthropic"
		} else {
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

	resolved := resolvedModel{
		Provider:    provider,
		APIKey:      apiKey,
		ModelID:     model,
		BaseURL:     baseURL,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}

	switch resolved.Provider {
	case "openai", "deepseek", "openai_compatible":
		clientCfg := openai.DefaultConfig(resolved.APIKey)
		if resolved.BaseURL != "" {
			clientCfg.BaseURL = resolved.BaseURL
		}
		oaiClient := openai.NewClientWithConfig(clientCfg)
		msgs := make([]openai.ChatCompletionMessage, len(req.Messages))
		for i, m := range req.Messages {
			msgs[i] = openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
		}

		stream, streamErr := oaiClient.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
			Model:       resolved.ModelID,
			Messages:    msgs,
			MaxTokens:   resolved.MaxTokens,
			Temperature: float32(resolved.Temperature),
			Stream:      true,
		})
		if streamErr != nil {
			// SSE stream not supported by this provider — fall back to sync completion.
			gw.logger.Warn("stream not supported by provider, falling back to sync",
				zap.String("provider", resolved.Provider),
				zap.Error(streamErr))
			resp, syncErr := oaiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
				Model:       resolved.ModelID,
				Messages:    msgs,
				MaxTokens:   resolved.MaxTokens,
				Temperature: float32(resolved.Temperature),
			})
			if syncErr != nil {
				return fmt.Errorf("sync fallback failed: %w", syncErr)
			}
			if len(resp.Choices) == 0 {
				return errors.New("no choices in response")
			}
			return handler(resp.Choices[0].Message.Content)
		}
		defer stream.Close()

		for {
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("stream recv: %w", err)
			}
			if len(resp.Choices) > 0 {
				delta := resp.Choices[0].Delta.Content
				if delta != "" {
					if err := handler(delta); err != nil {
						return err
					}
				}
			}
		}

	case "anthropic":
		anthClient := anthropic.NewClient(resolved.APIKey)
		var systemMsg string
		var anthMsgs []anthropic.Message
		for _, m := range req.Messages {
			if m.Role == "system" {
				systemMsg = m.Content
				continue
			}
			if m.Role == "user" {
				anthMsgs = append(anthMsgs, anthropic.NewUserTextMessage(m.Content))
			} else if m.Role == "assistant" {
				anthMsgs = append(anthMsgs, anthropic.NewAssistantTextMessage(m.Content))
			}
		}
		var streamErr error
		_, err := anthClient.CreateMessagesStream(ctx, anthropic.MessagesStreamRequest{
			MessagesRequest: anthropic.MessagesRequest{
				Model:     resolved.ModelID,
				MaxTokens: resolved.MaxTokens,
				Messages:  anthMsgs,
				System:    systemMsg,
			},
			OnContentBlockDelta: func(data anthropic.MessagesEventContentBlockDeltaData) {
				text := data.Delta.GetText()
				if text != "" {
					if err := handler(text); err != nil {
						streamErr = err
					}
				}
			},
		})
		if err != nil {
			return fmt.Errorf("anthropic stream (custom config): %w", err)
		}
		return streamErr
	}

	return fmt.Errorf("unsupported provider: %s", resolved.Provider)
}
