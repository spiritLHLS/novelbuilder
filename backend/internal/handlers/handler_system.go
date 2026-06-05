package handlers

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
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
		warnings = append(warnings, "Redis 不可用：即时承接与缓存能力降级")
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
	session := currentUser(c)
	var projectID *string
	if pid := c.Query("project_id"); pid != "" {
		if !isAdmin(session) {
			allowed, err := h.userCanAccessProject(c.Request.Context(), session.UserID, pid)
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			if !allowed {
				c.JSON(404, gin.H{"error": "project not found"})
				return
			}
		}
		projectID = &pid
	}
	if projectID == nil && !isAdmin(session) {
		c.JSON(403, gin.H{"error": "admin role required for global prompt presets"})
		return
	}
	if projectID != nil {
		req.IsGlobal = false
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
	if !h.canManagePromptPreset(c, c.Param("id")) {
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
	if !h.canManagePromptPreset(c, c.Param("id")) {
		return
	}
	if err := h.promptPresets.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

func (h *Handler) canManagePromptPreset(c *gin.Context, presetID string) bool {
	session := currentUser(c)
	if isAdmin(session) {
		return true
	}
	preset, err := h.promptPresets.Get(c.Request.Context(), presetID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return false
	}
	if preset == nil {
		c.JSON(404, gin.H{"error": "prompt preset not found"})
		return false
	}
	if preset.ProjectID == nil || *preset.ProjectID == "" || preset.IsGlobal {
		c.JSON(403, gin.H{"error": "admin role required for global prompt presets"})
		return false
	}
	allowed, err := h.userCanAccessProject(c.Request.Context(), session.UserID, *preset.ProjectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return false
	}
	if !allowed {
		c.JSON(404, gin.H{"error": "project not found"})
		return false
	}
	return true
}

func (h *Handler) OptimizePromptPreset(c *gin.Context) {
	var req struct {
		Content     string `json:"content" binding:"required"`
		TargetChars int    `json:"target_chars"`
		Language    string `json:"language"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		c.JSON(400, gin.H{"error": "content is required"})
		return
	}
	if req.TargetChars <= 0 {
		req.TargetChars = maxInt(280, utf8.RuneCountInString(content)*70/100)
	}
	if req.TargetChars > 4000 {
		req.TargetChars = 4000
	}
	system := `You optimize reusable writing rules for a long-form fiction generation system.
Return only the optimized rule text. Preserve hard constraints, factual requirements,
names, safety rules, genre boundaries, and ordering dependencies. Remove repetition,
soft filler, vague motivation, and duplicate wording. Do not introduce new story facts.`
	if strings.HasPrefix(strings.ToLower(req.Language), "zh") || req.Language == "" {
		system = `你是长篇小说生成系统的提示词规则编辑。只输出优化后的规则文本。
必须保留硬性约束、事实设定、名称、题材边界、安全规则和顺序依赖；删除重复、空泛动机、软性废话和同义累赘；不得新增故事事实。`
	}
	user := fmt.Sprintf("目标长度：不超过 %d 字符。\n\n待优化规则：\n%s", req.TargetChars, content)
	resp, err := h.ai.Chat(c.Request.Context(), gateway.ChatRequest{
		Task:        "prompt_rule_optimize",
		MaxTokens:   minInt(1600, maxInt(400, req.TargetChars*2)),
		Temperature: 0.2,
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	optimized := strings.TrimSpace(resp.Content)
	if optimized == "" {
		c.JSON(502, gin.H{"error": "optimizer returned empty content"})
		return
	}
	c.JSON(200, gin.H{"data": gin.H{
		"content":         optimized,
		"original_chars":  utf8.RuneCountInString(content),
		"optimized_chars": utf8.RuneCountInString(optimized),
		"tokens_used":     resp.TokensUsed,
	}})
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
