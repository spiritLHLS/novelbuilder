package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"

	anthropic "github.com/liushuangls/go-anthropic/v2"
	"github.com/novelbuilder/backend/internal/config"
	openai "github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
)

type AIGateway struct {
	cfg     config.AIGatewayConfig
	clients map[string]interface{}
	logger  *zap.Logger
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

func NewAIGateway(cfg config.AIGatewayConfig, logger *zap.Logger) *AIGateway {
	gw := &AIGateway{
		cfg:     cfg,
		clients: make(map[string]interface{}),
		logger:  logger,
	}

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
		case "deepseek":
			clientCfg := openai.DefaultConfig(modelCfg.APIKey)
			clientCfg.BaseURL = modelCfg.BaseURL
			gw.clients[name] = openai.NewClientWithConfig(clientCfg)
		}
	}

	return gw
}

func (gw *AIGateway) resolveModel(req ChatRequest) (string, config.AIModelConfig) {
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
	return modelName, gw.cfg.Models[modelName]
}

func (gw *AIGateway) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	modelName, modelCfg := gw.resolveModel(req)
	client, ok := gw.clients[modelName]
	if !ok {
		return nil, fmt.Errorf("model %s not configured", modelName)
	}

	gw.logger.Info("AI Chat request",
		zap.String("model", modelName),
		zap.String("task", req.Task),
		zap.Int("messages", len(req.Messages)))

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = modelCfg.MaxTokens
	}

	switch modelCfg.Provider {
	case "openai", "deepseek":
		oaiClient := client.(*openai.Client)
		msgs := make([]openai.ChatCompletionMessage, len(req.Messages))
		for i, m := range req.Messages {
			msgs[i] = openai.ChatCompletionMessage{
				Role:    m.Role,
				Content: m.Content,
			}
		}
		resp, err := oaiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:     modelCfg.Model,
			Messages:  msgs,
			MaxTokens: maxTokens,
		})
		if err != nil {
			return nil, fmt.Errorf("openai chat: %w", err)
		}
		if len(resp.Choices) == 0 {
			return nil, errors.New("no choices in response")
		}
		return &ChatResponse{
			Content:    resp.Choices[0].Message.Content,
			Model:      modelName,
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
		anthReq := anthropic.MessagesRequest{
			Model:     modelCfg.Model,
			MaxTokens: maxTokens,
			Messages:  anthMsgs,
			System:    systemMsg,
		}
		resp, err := anthClient.CreateMessages(ctx, anthReq)
		if err != nil {
			return nil, fmt.Errorf("anthropic chat: %w", err)
		}
		content := resp.GetFirstContentText()
		return &ChatResponse{
			Content:    content,
			Model:      modelName,
			TokensUsed: resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}, nil
	}

	return nil, fmt.Errorf("unsupported provider: %s", modelCfg.Provider)
}

func (gw *AIGateway) ChatStream(ctx context.Context, req ChatRequest, handler func(chunk string) error) error {
	modelName, modelCfg := gw.resolveModel(req)
	client, ok := gw.clients[modelName]
	if !ok {
		return fmt.Errorf("model %s not configured", modelName)
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = modelCfg.MaxTokens
	}

	switch modelCfg.Provider {
	case "openai", "deepseek":
		oaiClient := client.(*openai.Client)
		msgs := make([]openai.ChatCompletionMessage, len(req.Messages))
		for i, m := range req.Messages {
			msgs[i] = openai.ChatCompletionMessage{
				Role:    m.Role,
				Content: m.Content,
			}
		}
		stream, err := oaiClient.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
			Model:     modelCfg.Model,
			Messages:  msgs,
			MaxTokens: maxTokens,
			Stream:    true,
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
				Model:     modelCfg.Model,
				MaxTokens: maxTokens,
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

	return fmt.Errorf("unsupported provider: %s", modelCfg.Provider)
}
