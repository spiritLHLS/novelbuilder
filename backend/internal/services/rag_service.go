package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// RAGService handles vector retrieval from the vector_store table.
// It calls the Python sidecar to obtain embeddings and uses pgvector's
// cosine-distance operator (<=>)  for similarity search.
type RAGService struct {
	db         *pgxpool.Pool
	sidecarURL string
	logger     *zap.Logger
}

func NewRAGService(db *pgxpool.Pool, sidecarURL string, logger *zap.Logger) *RAGService {
	return &RAGService{db: db, sidecarURL: sidecarURL, logger: logger}
}

// embedRequest mirrors the Python sidecar /embed endpoint body.
type embedRequest struct {
	Text string `json:"text"`
}

type embedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// GetEmbedding returns a 1024-dim embedding vector for the given text.
// If the sidecar is unavailable the function returns nil, nil so callers
// can degrade gracefully.
func (r *RAGService) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(embedRequest{Text: text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.sidecarURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.logger.Warn("embedding sidecar unavailable", zap.Error(err))
		return nil, nil // graceful degradation
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		r.logger.Warn("embed endpoint returned non-200", zap.Int("status", resp.StatusCode), zap.String("body", string(raw)))
		return nil, nil
	}

	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	return er.Embedding, nil
}

// float32SliceToPGVec converts a Go []float32 to the pgvector literal format: '[0.1,0.2,...]'
func float32SliceToPGVec(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	buf := bytes.NewBufferString("[")
	for i, f := range v {
		if i > 0 {
			buf.WriteByte(',')
		}
		fmt.Fprintf(buf, "%g", f)
	}
	buf.WriteByte(']')
	return buf.String()
}

// SearchSensory retrieves the top-k most relevant style/sensory samples from
// the vector_store for the given project. collection should be 'style_samples'
// or 'sensory_samples'. Falls back to keyword scan when sidecar is unavailable.
func (r *RAGService) SearchSensory(ctx context.Context, projectID, query, collection string, k int) ([]string, error) {
	embedding, err := r.GetEmbedding(ctx, query)
	if err != nil {
		return nil, err
	}

	var rows interface{ Close() }
	var queryErr error

	if len(embedding) > 0 {
		// Vector similarity search using pgvector cosine distance
		pgVec := float32SliceToPGVec(embedding)
		sqlRows, e := r.db.Query(ctx,
			`SELECT content FROM vector_store
			 WHERE project_id = $1 AND collection = $2 AND embedding IS NOT NULL
			 ORDER BY embedding <=> $3::vector
			 LIMIT $4`,
			projectID, collection, pgVec, k)
		rows, queryErr = sqlRows, e
	} else {
		// Fallback: keyword match via ILIKE, ordered by recency
		likeQuery := "%" + query + "%"
		sqlRows, e := r.db.Query(ctx,
			`SELECT content FROM vector_store
			 WHERE project_id = $1 AND collection = $2
			   AND (content ILIKE $3 OR $3 = '%%')
			 ORDER BY created_at DESC
			 LIMIT $4`,
			projectID, collection, likeQuery, k)
		rows, queryErr = sqlRows, e
	}

	if queryErr != nil {
		return nil, fmt.Errorf("vector search: %w", queryErr)
	}

	// Type-assert to concrete pgx rows type so we can iterate
	type pgxRows interface {
		Next() bool
		Scan(...interface{}) error
		Close()
	}
	pgRows := rows.(pgxRows)
	defer pgRows.Close()

	var results []string
	for pgRows.Next() {
		var content string
		if err := pgRows.Scan(&content); err != nil {
			return nil, err
		}
		results = append(results, content)
	}
	return results, nil
}

// StoreEmbedding embeds and stores a piece of text in the vector_store.
// sourceType / sourceID are used for selective deletion during rebuild (see migration 004).
// If the sidecar is unavailable the entry is stored with a NULL embedding.
func (r *RAGService) StoreEmbedding(ctx context.Context, projectID, collection, content, sourceType, sourceID string, metadata map[string]interface{}) error {
	embedding, _ := r.GetEmbedding(ctx, content)

	id := uuid.New().String()
	metaBytes, _ := json.Marshal(metadata)

	if len(embedding) > 0 {
		pgVec := float32SliceToPGVec(embedding)
		_, err := r.db.Exec(ctx,
			`INSERT INTO vector_store (id, project_id, collection, content, metadata, embedding, source_type, source_id, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6::vector, $7, $8, NOW())`,
			id, projectID, collection, content, metaBytes, pgVec, sourceType, sourceID)
		return err
	}

	// Store without embedding (column is nullable)
	_, err := r.db.Exec(ctx,
		`INSERT INTO vector_store (id, project_id, collection, content, metadata, source_type, source_id, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		id, projectID, collection, content, metaBytes, sourceType, sourceID)
	return err
}

// DeleteBySourceID removes all vector_store rows for a specific source (e.g., one reference material).
func (r *RAGService) DeleteBySourceID(ctx context.Context, projectID, sourceID string) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM vector_store WHERE project_id = $1 AND source_id = $2`,
		projectID, sourceID)
	return err
}

// DeleteForProject removes ALL vector_store rows for a project (used for full rebuild).
func (r *RAGService) DeleteForProject(ctx context.Context, projectID string) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM vector_store WHERE project_id = $1`, projectID)
	return err
}

// CollectionStat carries per-collection row counts for a project.
type CollectionStat struct {
	Collection string `json:"collection"`
	Count      int    `json:"count"`
}

// GetProjectStats returns per-collection vector counts for a project.
func (r *RAGService) GetProjectStats(ctx context.Context, projectID string) ([]CollectionStat, error) {
	rows, err := r.db.Query(ctx,
		`SELECT collection, COUNT(*) FROM vector_store WHERE project_id = $1 GROUP BY collection ORDER BY collection`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []CollectionStat
	for rows.Next() {
		var s CollectionStat
		if err := rows.Scan(&s.Collection, &s.Count); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, nil
}
