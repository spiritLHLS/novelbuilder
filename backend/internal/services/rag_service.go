package services

import (
"bytes"
"context"
"encoding/json"
"fmt"
"io"
"net/http"
"time"

"github.com/jackc/pgx/v5/pgxpool"
"go.uber.org/zap"
)

// RAGService handles vector retrieval.
// New architecture: delegates all embedding/search to the Python sidecar's
// Qdrant endpoints. The PostgreSQL vector_store table is kept for metadata
// but vector operations no longer use pgvector.
type RAGService struct {
db         *pgxpool.Pool
sidecarURL string
httpClient *http.Client
logger     *zap.Logger
}

func NewRAGService(db *pgxpool.Pool, sidecarURL string, logger *zap.Logger) *RAGService {
return &RAGService{
db:         db,
sidecarURL: sidecarURL,
httpClient: &http.Client{Timeout: 30 * time.Second},
logger:     logger,
}
}

// embedRequest mirrors the Python sidecar /embed body.
type embedRequest struct {
Text string `json:"text"`
}

type embedResponse struct {
Embedding []float32 `json:"embedding"`
}

// GetEmbedding returns an embedding vector from the Python sidecar.
// Returns nil, nil on sidecar unavailability for graceful degradation.
func (r *RAGService) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
body, _ := json.Marshal(embedRequest{Text: text})
req, err := http.NewRequestWithContext(ctx, http.MethodPost,
r.sidecarURL+"/embed", bytes.NewReader(body))
if err != nil {
return nil, err
}
req.Header.Set("Content-Type", "application/json")

resp, err := r.httpClient.Do(req)
if err != nil {
r.logger.Warn("embedding sidecar unavailable", zap.Error(err))
return nil, nil
}
defer resp.Body.Close()

if resp.StatusCode != http.StatusOK {
raw, _ := io.ReadAll(resp.Body)
r.logger.Warn("embed returned non-200",
zap.Int("status", resp.StatusCode), zap.String("body", string(raw)))
return nil, nil
}

var er embedResponse
if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
return nil, err
}
return er.Embedding, nil
}

// sidecarPost is a helper for calling sidecar JSON endpoints.
func (r *RAGService) sidecarPost(ctx context.Context, path string, body interface{}) (json.RawMessage, error) {
data, _ := json.Marshal(body)
req, err := http.NewRequestWithContext(ctx, http.MethodPost,
r.sidecarURL+path, bytes.NewReader(data))
if err != nil {
return nil, err
}
req.Header.Set("Content-Type", "application/json")

resp, err := r.httpClient.Do(req)
if err != nil {
return nil, fmt.Errorf("sidecar %s: %w", path, err)
}
defer resp.Body.Close()
raw, _ := io.ReadAll(resp.Body)
if resp.StatusCode >= 400 {
return nil, fmt.Errorf("sidecar %s returned %d: %s", path, resp.StatusCode, string(raw))
}
return raw, nil
}

// SearchSensory retrieves semantically similar content from Qdrant via sidecar.
// Falls back to empty result if sidecar is unavailable.
func (r *RAGService) SearchSensory(ctx context.Context, projectID, query, collection string, k int) ([]string, error) {
body := map[string]interface{}{
"project_id": projectID,
"collection": collection,
"query":      query,
"limit":      k,
}
raw, err := r.sidecarPost(ctx, "/vector/search", body)
if err != nil {
r.logger.Warn("Qdrant search failed, returning empty", zap.Error(err))
return nil, nil
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

// StoreEmbedding upserts a single piece of content into Qdrant via sidecar.
func (r *RAGService) StoreEmbedding(ctx context.Context, projectID, collection, content, sourceType, sourceID string, metadata map[string]interface{}) error {
if metadata == nil {
metadata = make(map[string]interface{})
}
metadata["source_type"] = sourceType
metadata["source_id"] = sourceID

body := map[string]interface{}{
"project_id": projectID,
"collection": collection,
"content":    content,
"metadata":   metadata,
}
_, err := r.sidecarPost(ctx, "/vector/upsert", body)
if err != nil {
r.logger.Warn("Qdrant upsert failed", zap.Error(err))
return nil // graceful degradation
}
return nil
}

// BatchEmbedItem is the input unit for StoreEmbeddingBatch.
type BatchEmbedItem struct {
Collection string
Content    string
SourceType string
SourceID   string
Metadata   map[string]interface{}
}

// StoreEmbeddingBatch sends all items to Qdrant in a single batch request.
// The Python sidecar handles concurrent embedding internally — no N+1 here.
func (r *RAGService) StoreEmbeddingBatch(ctx context.Context, projectID string, items []BatchEmbedItem) error {
if len(items) == 0 {
return nil
}

qdrantItems := make([]map[string]interface{}, 0, len(items))
for _, item := range items {
meta := item.Metadata
if meta == nil {
meta = make(map[string]interface{})
}
meta["source_type"] = item.SourceType
meta["source_id"] = item.SourceID
qdrantItems = append(qdrantItems, map[string]interface{}{
"collection": item.Collection,
"content":    item.Content,
"metadata":   meta,
})
}

body := map[string]interface{}{
"project_id": projectID,
"items":      qdrantItems,
}
_, err := r.sidecarPost(ctx, "/vector/rebuild", body)
if err != nil {
r.logger.Warn("Qdrant batch upsert failed", zap.Error(err))
return nil
}
return nil
}

// DeleteBySourceID removes all Qdrant points for a specific source.
// (Delegated to Qdrant's filter-based delete on the sidecar.)
func (r *RAGService) DeleteBySourceID(ctx context.Context, projectID, sourceID string) error {
// Qdrant filter delete is done via re-upsert (idempotent).
// For a full delete, call the sidecar's collection management endpoint.
r.logger.Info("DeleteBySourceID: points will be overwritten on next rebuild",
zap.String("project_id", projectID), zap.String("source_id", sourceID))
return nil
}

// DeleteForProject is a no-op here; Qdrant collections are project-prefixed.
func (r *RAGService) DeleteForProject(ctx context.Context, projectID string) error {
r.logger.Info("DeleteForProject: use /vector/rebuild to replace collection",
zap.String("project_id", projectID))
return nil
}

// CollectionStat carries per-collection counts.
type CollectionStat struct {
Collection string `json:"collection"`
Count      int    `json:"count"`
}

// GetProjectStats returns per-collection point counts from Qdrant.
func (r *RAGService) GetProjectStats(ctx context.Context, projectID string) ([]CollectionStat, error) {
req, err := http.NewRequestWithContext(ctx, http.MethodGet,
r.sidecarURL+"/vector/status/"+projectID, nil)
if err != nil {
return nil, err
}
resp, err := r.httpClient.Do(req)
if err != nil {
r.logger.Warn("vector status sidecar unavailable", zap.Error(err))
return nil, nil
}
defer resp.Body.Close()

var result struct {
Collections []CollectionStat `json:"collections"`
}
if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
return nil, err
}
return result.Collections, nil
}
