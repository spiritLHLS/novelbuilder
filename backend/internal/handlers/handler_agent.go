package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// ── Agent Review Handlers ─────────────────────────────────────────────────────

func (h *Handler) StartAgentReview(c *gin.Context) {
	projectID := c.Param("id")
	var req models.AgentReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Rounds <= 0 {
		req.Rounds = 3
	}

	session, err := h.agentReview.StreamReview(c.Request.Context(), projectID, req, func(msg models.AgentMessage) {})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, session)
}

func (h *Handler) StreamAgentReview(c *gin.Context) {
	projectID := c.Param("id")
	scope := c.Query("scope")
	if scope == "" {
		scope = "full"
	}
	targetID := c.Query("target_id")
	roundsStr := c.DefaultQuery("rounds", "3")
	rounds := 3
	if n, err := strconv.Atoi(roundsStr); err == nil && n > 0 {
		rounds = n
	}

	req := models.AgentReviewRequest{Scope: scope, TargetID: targetID, Rounds: rounds}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	_, err := h.agentReview.StreamReview(c.Request.Context(), projectID, req, func(msg models.AgentMessage) {
		data, e := json.Marshal(msg)
		if e != nil {
			return
		}
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
		c.Writer.Flush()
	})
	if err != nil {
		errData, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(c.Writer, "data: %s\n\n", errData)
		c.Writer.Flush()
		return
	}
	fmt.Fprintf(c.Writer, "data: {\"done\":true}\n\n")
	c.Writer.Flush()
}

func (h *Handler) GetAgentReview(c *gin.Context) {
	sessionID := c.Param("id")
	session, err := h.agentReview.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if session == nil {
		c.JSON(404, gin.H{"error": "session not found"})
		return
	}
	c.JSON(200, session)
}

func (h *Handler) ListAgentReviews(c *gin.Context) {
	projectID := c.Param("id")
	sessions, err := h.agentReview.ListSessions(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, sessions)
}

// ── LangGraph Agent Handlers ──────────────────────────────────────────────────

func (h *Handler) AgentRun(c *gin.Context) {
	projectID := c.Param("id")
	var req models.AgentRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.TaskType == "" {
		req.TaskType = "generate_chapter"
	}

	if req.LLMConfig == nil {
		req.LLMConfig = map[string]interface{}{}
	}

	_, hasAPIKey := req.LLMConfig["api_key"]
	_, hasModel := req.LLMConfig["model"]
	_, hasBaseURL := req.LLMConfig["base_url"]
	if !hasAPIKey || !hasModel || !hasBaseURL {
		profile, err := h.llmProfiles.GetDefault(c.Request.Context())
		if err != nil {
			h.logger.Error("resolve default llm profile failed", zap.Error(err))
			c.JSON(500, gin.H{"error": "failed to resolve default LLM profile"})
			return
		}
		if profile == nil {
			c.JSON(400, gin.H{"error": "no default AI model configured: please set one in 设置 → AI 模型配置"})
			return
		}

		if !hasAPIKey {
			req.LLMConfig["api_key"] = profile.APIKey
		}
		if !hasModel {
			req.LLMConfig["model"] = profile.ModelName
		}
		if !hasBaseURL {
			req.LLMConfig["base_url"] = profile.BaseURL
		}
		if _, ok := req.LLMConfig["max_tokens"]; !ok {
			req.LLMConfig["max_tokens"] = profile.MaxTokens
		}
		if _, ok := req.LLMConfig["temperature"]; !ok {
			req.LLMConfig["temperature"] = profile.Temperature
		}
		if _, ok := req.LLMConfig["graphiti_model"]; !ok {
			req.LLMConfig["graphiti_model"] = profile.ModelName
		}
		if _, ok := req.LLMConfig["graphiti_api_key"]; !ok {
			req.LLMConfig["graphiti_api_key"] = profile.APIKey
		}
		if _, ok := req.LLMConfig["graphiti_base_url"]; !ok {
			req.LLMConfig["graphiti_base_url"] = profile.BaseURL
		}
	}

	sessionID, err := h.sidecar.RunAgent(c.Request.Context(), projectID, req)
	if err != nil {
		h.logger.Error("agent run failed", zap.Error(err))
		c.JSON(502, gin.H{"error": "agent service unavailable: " + err.Error()})
		return
	}
	c.JSON(202, gin.H{"session_id": sessionID, "status": "running"})
}

func (h *Handler) AgentSessionStatus(c *gin.Context) {
	sid := c.Param("sid")
	status, err := h.sidecar.GetAgentStatus(c.Request.Context(), sid)
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, status)
}

func (h *Handler) AgentSessionStream(c *gin.Context) {
	// Proxy the SSE stream from Python sidecar
	sid := c.Param("sid")
	sidecarURL := h.sidecar.StreamURL(sid)

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, sidecarURL, nil)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(502, gin.H{"error": "sidecar stream unavailable"})
		return
	}
	defer resp.Body.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(200)

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			c.Writer.Write(buf[:n])
			c.Writer.Flush()
		}
		if err != nil {
			break
		}
	}
}

// ── Knowledge Graph (Neo4j) Handlers ─────────────────────────────────────────

func (h *Handler) GetGraphEntities(c *gin.Context) {
	data, err := h.sidecar.GetGraphEntities(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, data)
}

func (h *Handler) QueryGraph(c *gin.Context) {
	var req models.GraphQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	raw, err := h.sidecar.QueryGraph(c.Request.Context(), req)
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.Data(200, "application/json", raw)
}

func (h *Handler) UpsertGraphEntity(c *gin.Context) {
	var req models.GraphUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := h.sidecar.UpsertGraphEntity(c.Request.Context(), c.Param("id"), req); err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) SyncProjectGraph(c *gin.Context) {
	if err := h.sidecar.SyncProjectGraph(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "graph sync triggered"})
}

// ── Vector Store (Qdrant) Handlers ───────────────────────────────────────────

func (h *Handler) GetVectorStatus(c *gin.Context) {
	status, err := h.sidecar.GetVectorStatus(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, status)
}

func (h *Handler) RebuildVectorIndex(c *gin.Context) {
	var req models.VectorRebuildRequest
	// Body is optional — ignore bind errors (empty body is valid)
	_ = c.ShouldBindJSON(&req)
	if err := h.sidecar.RebuildVectorIndex(c.Request.Context(), c.Param("id"), req.Items); err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) SearchVector(c *gin.Context) {
	var req models.VectorSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Limit == 0 {
		req.Limit = 5
	}
	raw, err := h.sidecar.SearchVector(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.Data(200, "application/json", raw)
}

// ── Per-Agent Model Routing ───────────────────────────────────────────────────

func (h *Handler) ListAgentRoutes(c *gin.Context) {
	routes, err := h.agentRouting.List(c.Request.Context(), nil)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": routes})
}

func (h *Handler) UpsertAgentRoute(c *gin.Context) {
	agentType := c.Param("agent_type")
	var req models.UpsertAgentRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	req.AgentType = agentType
	req.ProjectID = nil
	route, err := h.agentRouting.Upsert(c.Request.Context(), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": route})
}

func (h *Handler) DeleteAgentRoute(c *gin.Context) {
	if err := h.agentRouting.Delete(c.Request.Context(), c.Param("agent_type"), nil); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) ListProjectAgentRoutes(c *gin.Context) {
	pid := c.Param("id")
	routes, err := h.agentRouting.List(c.Request.Context(), &pid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": routes})
}

func (h *Handler) UpsertProjectAgentRoute(c *gin.Context) {
	projectID := c.Param("id")
	agentType := c.Param("agent_type")
	var req models.UpsertAgentRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	req.AgentType = agentType
	req.ProjectID = &projectID
	route, err := h.agentRouting.Upsert(c.Request.Context(), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": route})
}

func (h *Handler) DeleteProjectAgentRoute(c *gin.Context) {
	pid := c.Param("id")
	if err := h.agentRouting.Delete(c.Request.Context(), c.Param("agent_type"), &pid); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ── Batch Agent (Volume-based sequential generation) ─────────────────────────

// BatchAgentRunHTTPRequest is the HTTP body for POST /projects/:id/agent/batch-run.
type BatchAgentRunHTTPRequest struct {
	ChapterNums  []int             `json:"chapter_nums" binding:"required"`
	OutlineHints map[string]string `json:"outline_hints"`
	LLMProfileID string            `json:"llm_profile_id"`
	MaxRetries   int               `json:"max_retries"`
}

// AgentBatchRun starts a sequential multi-chapter generation session via the Python sidecar.
func (h *Handler) AgentBatchRun(c *gin.Context) {
	projectID := c.Param("id")
	var req BatchAgentRunHTTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if len(req.ChapterNums) == 0 {
		c.JSON(400, gin.H{"error": "chapter_nums must not be empty"})
		return
	}
	if len(req.ChapterNums) > 200 {
		c.JSON(400, gin.H{"error": "chapter_nums must not exceed 200 entries"})
		return
	}
	if req.MaxRetries <= 0 {
		req.MaxRetries = 1
	}

	llmCfg, err := h.resolveAgentLLMConfig(c.Request.Context(), "writer", projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to resolve LLM config: " + err.Error()})
		return
	}

	if req.OutlineHints == nil {
		req.OutlineHints = map[string]string{}
	}

	batchReq := models.BatchAgentRunRequest{
		ChapterNums:  req.ChapterNums,
		OutlineHints: req.OutlineHints,
		LLMConfig:    llmCfg,
		MaxRetries:   req.MaxRetries,
	}

	batchID, err := h.sidecar.RunBatchAgent(c.Request.Context(), projectID, batchReq)
	if err != nil {
		h.logger.Error("batch agent run failed", zap.Error(err))
		c.JSON(502, gin.H{"error": "agent service unavailable: " + err.Error()})
		return
	}
	c.JSON(202, gin.H{"batch_id": batchID, "status": "running", "total": len(req.ChapterNums)})
}

func (h *Handler) AgentBatchStatus(c *gin.Context) {
	raw, err := h.sidecar.GetBatchAgentStatus(c.Request.Context(), c.Param("bid"))
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.Data(200, "application/json", raw)
}

func (h *Handler) AgentBatchStream(c *gin.Context) {
	bid := c.Param("bid")
	streamURL := h.sidecar.BatchStreamURL(bid)

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, streamURL, nil)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(502, gin.H{"error": "sidecar batch stream unavailable"})
		return
	}
	defer resp.Body.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(200)

	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			c.Writer.Write(buf[:n]) //nolint:errcheck
			c.Writer.Flush()
		}
		if readErr != nil {
			break
		}
	}
}
