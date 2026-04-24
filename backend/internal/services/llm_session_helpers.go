package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/novelbuilder/backend/internal/gateway"
)

func contextWithLLMSession(ctx context.Context, llmCfg map[string]interface{}, fallback string) context.Context {
	if gateway.SessionIDFromContext(ctx) != "" {
		return ctx
	}
	if sessionID := llmConfigSessionID(llmCfg); sessionID != "" {
		return gateway.WithSessionID(ctx, sessionID)
	}
	if strings.TrimSpace(fallback) != "" {
		return gateway.WithSessionID(ctx, fallback)
	}
	return ctx
}

func ensureContextSessionConfig(ctx context.Context, llmCfg map[string]interface{}, fallback string) map[string]interface{} {
	cloned := make(map[string]interface{}, len(llmCfg)+1)
	for key, value := range llmCfg {
		cloned[key] = value
	}
	if llmConfigSessionID(cloned) != "" {
		return cloned
	}
	if sessionID := gateway.SessionIDFromContext(ctx); sessionID != "" {
		cloned["session_id"] = sessionID
		return cloned
	}
	if strings.TrimSpace(fallback) != "" {
		cloned["session_id"] = strings.TrimSpace(fallback)
	}
	return cloned
}

func llmConfigSessionID(llmCfg map[string]interface{}) string {
	if llmCfg == nil {
		return ""
	}
	value := strings.TrimSpace(fmt.Sprint(llmCfg["session_id"]))
	if value == "" || value == "<nil>" {
		return ""
	}
	return value
}
