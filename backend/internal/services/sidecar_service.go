package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// SidecarService proxies requests to the Python agent service.
// It handles agent sessions, graph (Neo4j), and vector (Qdrant) operations.
type SidecarService struct {
	baseURL string
	client  *http.Client
	logger  *zap.Logger
}

func NewSidecarService(baseURL string, logger *zap.Logger) *SidecarService {
	return &SidecarService{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 600 * time.Second, // generous for agent runs
		},
		logger: logger,
	}
}

// BaseURL returns the configured base URL of the Python sidecar.
func (s *SidecarService) BaseURL() string {
	return s.baseURL
}

// Post exposes the internal HTTP POST helper for handler layers that need
// to call arbitrary sidecar endpoints (e.g. Fanqie upload routes).
func (s *SidecarService) Post(ctx context.Context, path string, body interface{}) (json.RawMessage, error) {
	return s.post(ctx, path, body)
}

func (s *SidecarService) post(ctx context.Context, path string, body interface{}) (json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sidecar POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("sidecar %s returned %d: %s", path, resp.StatusCode, string(raw))
	}
	return raw, nil
}

func (s *SidecarService) get(ctx context.Context, path string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sidecar GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("sidecar %s returned %d: %s", path, resp.StatusCode, string(raw))
	}
	return raw, nil
}

// ── Agent ─────────────────────────────────────────────────────────────────────

// RunAgent starts an agent session and returns the session ID immediately.
func (s *SidecarService) RunAgent(ctx context.Context, projectID string, req models.AgentRunRequest) (string, error) {
	// Merge project_id into request body
	body := map[string]interface{}{
		"project_id":   projectID,
		"task_type":    req.TaskType,
		"user_prompt":  req.UserPrompt,
		"outline_hint": req.OutlineHint,
		"llm_config":   req.LLMConfig,
		"max_retries":  req.MaxRetries,
	}
	if req.ChapterNum != nil {
		body["chapter_num"] = *req.ChapterNum
	}
	if req.StyleProfile != nil {
		body["style_profile"] = req.StyleProfile
	}

	raw, err := s.post(ctx, "/agent/run", body)
	if err != nil {
		return "", err
	}
	var result struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("parse agent/run response: %w", err)
	}
	return result.SessionID, nil
}

// GetAgentStatus fetches the current session state.
func (s *SidecarService) GetAgentStatus(ctx context.Context, sessionID string) (*models.AgentSessionStatus, error) {
	raw, err := s.get(ctx, "/agent/status/"+sessionID)
	if err != nil {
		return nil, err
	}
	var status models.AgentSessionStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return nil, err
	}
	status.SessionID = sessionID
	return &status, nil
}

// SyncProjectGraph triggers a full sync of PostgreSQL project data → Neo4j.
// Called after project creation, character updates, or constitution changes.
func (s *SidecarService) SyncProjectGraph(ctx context.Context, projectID string) error {
	_, err := s.post(ctx, "/graph/sync-project/"+projectID, nil)
	return err
}

// ── Graph ─────────────────────────────────────────────────────────────────────

// GetGraphEntities returns the full knowledge graph for a project.
func (s *SidecarService) GetGraphEntities(ctx context.Context, projectID string) (*models.GraphData, error) {
	raw, err := s.get(ctx, "/graph/entities/"+projectID)
	if err != nil {
		return nil, err
	}
	var data models.GraphData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// UpsertGraphEntity upserts a single entity in Neo4j.
func (s *SidecarService) UpsertGraphEntity(ctx context.Context, projectID string, req models.GraphUpsertRequest) error {
	body := map[string]interface{}{
		"project_id":  projectID,
		"entity_type": req.EntityType,
		"entity_id":   req.EntityID,
		"name":        req.Name,
		"properties":  req.Properties,
		"relations":   req.Relations,
	}
	_, err := s.post(ctx, "/graph/upsert", body)
	return err
}

// QueryGraph executes a read Cypher query.
func (s *SidecarService) QueryGraph(ctx context.Context, req models.GraphQueryRequest) (json.RawMessage, error) {
	return s.post(ctx, "/graph/query", req)
}

// ── Vector ────────────────────────────────────────────────────────────────────

// GetVectorStatus returns Qdrant collection stats for a project.
func (s *SidecarService) GetVectorStatus(ctx context.Context, projectID string) (*models.VectorStatus, error) {
	raw, err := s.get(ctx, "/vector/status/"+projectID)
	if err != nil {
		return nil, err
	}
	var status models.VectorStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// RebuildVectorIndex rebuilds Qdrant collections from the provided items.
// Items are batched on the Python side — no N+1 embedding calls.
func (s *SidecarService) RebuildVectorIndex(ctx context.Context, projectID string, items []map[string]interface{}) error {
	body := map[string]interface{}{
		"project_id": projectID,
		"items":      items,
	}
	_, err := s.post(ctx, "/vector/rebuild", body)
	return err
}

// SearchVector performs a semantic search against a Qdrant collection.
func (s *SidecarService) SearchVector(ctx context.Context, projectID string, req models.VectorSearchRequest) (json.RawMessage, error) {
	body := map[string]interface{}{
		"project_id": projectID,
		"collection": req.Collection,
		"query":      req.Query,
		"limit":      req.Limit,
	}
	return s.post(ctx, "/vector/search", body)
}

// ── RAG (backward-compat wrapper used by legacy chapter service) ──────────────

// StreamURL returns the SSE stream URL for the agent session.
func (s *SidecarService) StreamURL(sessionID string) string {
	return s.baseURL + "/agent/stream/" + sessionID
}

// BatchStreamURL returns the SSE URL for a batch generation session.
func (s *SidecarService) BatchStreamURL(batchID string) string {
	return s.baseURL + "/agent/batch-stream/" + batchID
}

// RunBatchAgent starts a sequential multi-chapter agent batch and returns the batch ID.
func (s *SidecarService) RunBatchAgent(ctx context.Context, projectID string, req models.BatchAgentRunRequest) (string, error) {
	body := map[string]interface{}{
		"project_id":    projectID,
		"chapter_nums":  req.ChapterNums,
		"outline_hints": req.OutlineHints,
		"llm_config":    req.LLMConfig,
		"max_retries":   req.MaxRetries,
	}
	if req.StyleProfile != nil {
		body["style_profile"] = req.StyleProfile
	}
	raw, err := s.post(ctx, "/agent/batch-run", body)
	if err != nil {
		return "", err
	}
	var result struct {
		BatchID string `json:"batch_id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("parse agent/batch-run response: %w", err)
	}
	return result.BatchID, nil
}

// GetBatchAgentStatus fetches the current state of a batch generation session.
func (s *SidecarService) GetBatchAgentStatus(ctx context.Context, batchID string) (json.RawMessage, error) {
	return s.get(ctx, "/agent/batch-status/"+batchID)
}

// SearchSensoryViaQdrant replaces the old pgvector-based SearchSensory.
// Calls the Python sidecar's Qdrant-backed vector search.
func (s *SidecarService) SearchSensoryViaQdrant(ctx context.Context, projectID, query, collection string, k int) ([]string, error) {
	body := map[string]interface{}{
		"project_id": projectID,
		"collection": collection,
		"query":      query,
		"limit":      k,
	}
	raw, err := s.post(ctx, "/vector/search", body)
	if err != nil {
		s.logger.Warn("Qdrant search failed, returning empty", zap.Error(err))
		return nil, nil // graceful degradation
	}
	var result struct {
		Hits []struct {
			Content string `json:"content"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(result.Hits))
	for _, h := range result.Hits {
		out = append(out, h.Content)
	}
	return out, nil
}
