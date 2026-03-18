package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"

	anthropic "github.com/liushuangls/go-anthropic/v2"
	"github.com/novelbuilder/backend/internal/config"
	"github.com/novelbuilder/backend/internal/models"
	openai "github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
)

// ProfileResolver is a minimal interface for looking up the active LLM profile from the database.
// This avoids a circular import with the services package.
type ProfileResolver interface {
	GetDefault(ctx context.Context) (*models.LLMProfileFull, error)
}

type AIGateway struct {
	cfg      config.AIGatewayConfig
	clients  map[string]interface{}
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
	Content    string `json:"content"`
	Model      string `json:"model"`
	TokensUsed int    `json:"tokens_used"`
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

func NewAIGateway(cfg config.AIGatewayConfig, profiles ProfileResolver, logger *zap.Logger) *AIGateway {
	gw := &AIGateway{
		cfg:      cfg,
		clients:  make(map[string]interface{}),
		profiles: profiles,
		logger:   logger,
	}
	// Pre-build clients for config-file-defined models (fallback)
	for name, modelCfg := range cfg.Models {
		switch modelCfg.Provider {
		case "openai":
			clientCfg := openai.DefaultConfig(modelCfg.APIKey)
			if modelCfg.BaseURL != "" {
				clientCfg.BaseURL = modelCfg.BaseURL
			}
			gw.clients[name] = openai.NewClientWithConfig(clientCfg)
		case "anthropic":
			gw.clients[name] = anthropic.NewClient(modelCfg.APIKey)
		default: // deepseek, openai_compatible, etc.
			clientCfg := openai.DefaultConfig(modelCfg.APIKey)
			if modelCfg.BaseURL != "" {
				clientCfg.BaseURL = modelCfg.BaseURL
			}
			gw.clients[name] = openai.NewClientWithConfig(clientCfg)
		}
	}
	return gw
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

// resolveModel returns the model parameters to use for a request.
// Priority: DB default profile > config task_routing > config default_model.
func (gw *AIGateway) resolveModel(ctx context.Context, req ChatRequest) (resolvedModel, interface{}, error) {
	// 1. Try DB-configured default profile (highest priority)
	if gw.profiles != nil {
		profile, err := gw.profiles.GetDefault(ctx)
		if err == nil && profile != nil {
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
	}

	// 2. Fall back to config-file model (by name routing or default)
	modelName := req.ModelName
	if modelName == "" {
		task := req.Task
		if task == "" {
			task = req.TaskType
		}
		if routed, ok := gw.cfg.TaskRouting[task]; ok {
			modelName = routed
		} else {
			modelName = gw.cfg.DefaultModel
		}
	}

	if modelName == "" {
		return resolvedModel{}, nil, fmt.Errorf("no AI model configured: add a profile in Settings or set default_model in config")
	}

	modelCfg, ok := gw.cfg.Models[modelName]
	if !ok {
		return resolvedModel{}, nil, fmt.Errorf("model %q not found in config", modelName)
	}

	client, ok := gw.clients[modelName]
	if !ok {
		return resolvedModel{}, nil, fmt.Errorf("no client for model %q", modelName)
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = modelCfg.MaxTokens
	}
	temp := req.Temperature
	if temp == 0 {
		temp = 0.7
	}

	resolved := resolvedModel{
		Provider:    modelCfg.Provider,
		APIKey:      modelCfg.APIKey,
		ModelID:     modelCfg.Model,
		BaseURL:     modelCfg.BaseURL,
		MaxTokens:   maxTokens,
		Temperature: temp,
		ProfileName: modelName,
	}
	return resolved, client, nil
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
			Content:    resp.Choices[0].Message.Content,
			Model:      resolved.ProfileName,
			TokensUsed: resp.Usage.TotalTokens,
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
			Content:    resp.GetFirstContentText(),
			Model:      resolved.ProfileName,
			TokensUsed: resp.Usage.InputTokens + resp.Usage.OutputTokens,
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
