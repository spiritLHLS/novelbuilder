import sys

helpers = """
// ── Shared LLM Config Helpers ─────────────────────────────────────────────────

func (h *Handler) resolveLLMConfig(ctx context.Context) (map[string]interface{}, error) {
\tprofile, err := h.llmProfiles.GetDefault(ctx)
\tif err != nil {
\t\treturn nil, err
\t}
\tif profile == nil {
\t\treturn nil, errors.New("no default AI model configured: please set one in \\u8bbe\\u7f6e \\u2192 AI \\u6a21\\u578b\\u914d\\u7f6e")
\t}
\treturn map[string]interface{}{
\t\t"api_key":     profile.APIKey,
\t\t"model":       profile.ModelName,
\t\t"base_url":    profile.BaseURL,
\t\t"provider":    profile.Provider,
\t\t"max_tokens":  profile.MaxTokens,
\t\t"temperature": profile.Temperature,
\t}, nil
}

// resolveAgentLLMConfig returns an LLM config map for a given agent type,
// respecting project-level -> global-level -> default-profile priority order.
func (h *Handler) resolveAgentLLMConfig(ctx context.Context, agentType, projectID string) (map[string]interface{}, error) {
\tcfg, err := h.agentRouting.ResolveForAgent(ctx, agentType, projectID)
\tif err != nil {
\t\treturn h.resolveLLMConfig(ctx)
\t}
\tif cfg == nil {
\t\treturn h.resolveLLMConfig(ctx)
\t}
\tapiKey, _ := cfg["api_key"].(string)
\tif apiKey == "" {
\t\treturn h.resolveLLMConfig(ctx)
\t}
\tif _, hasTemp := cfg["temperature"]; !hasTemp {
\t\tif defCfg, defErr := h.resolveLLMConfig(ctx); defErr == nil && defCfg != nil {
\t\t\tcfg["temperature"] = defCfg["temperature"]
\t\t\tcfg["max_tokens"] = defCfg["max_tokens"]
\t\t}
\t}
\treturn cfg, nil
}

// containsStr reports whether sub appears in s (case-sensitive byte search).
func containsStr(s, sub string) bool {
\tfor i := 0; i <= len(s)-len(sub); i++ {
\t\tif s[i:i+len(sub)] == sub {
\t\t\treturn true
\t\t}
\t}
\treturn false
}
"""

with open('handlers.go', 'r') as f:
    lines = f.readlines()

# Keep only the first 387 lines (package + imports + struct + NewHandler + RegisterRoutes)
keep = lines[:387]

with open('handlers.go', 'w') as f:
    f.writelines(keep)
    f.write(helpers)

with open('handlers.go') as f:
    total = len(f.readlines())
print(f"handlers.go trimmed to {total} lines")
