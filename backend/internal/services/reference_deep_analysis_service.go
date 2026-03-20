package services

// reference_deep_analysis_service.go — chunked, background analysis of large
// reference novels.  Dispatched as a TaskQueueItem (type "reference_analysis")
// so progress is persisted in the DB and survives restarts.
//
// Flow
// ────
//  1. Handler calls StartDeepAnalysis → inserts reference_analysis_jobs row + task_queue row.
//  2. TaskQueueService worker picks it up and calls the registered handler here.
//  3. For each text chunk the handler calls the Python sidecar /deep-analyze/chunk
//     with exponential back-off.  Progress is written after every chunk.
//  4. After all chunks are done the handler calls /deep-analyze/merge and writes
//     the aggregated result back to reference_analysis_jobs.
//  5. Handler optionally imports extracted entities into world_bibles / characters /
//     outlines tables when ImportResult is called.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/retry"
	"go.uber.org/zap"
)

const (
	// chunkSize is the maximum number of UTF-8 runes per analysis chunk.
	// At ~500 chars/page this is roughly 160 pages, well within a 16k-token
	// context window after prompt overhead.
	chunkSize = 80_000
	// taskTypeRefAnalysis is the task_queue.task_type value for deep analysis.
	taskTypeRefAnalysis = "reference_analysis"
)

// AnalysisJob mirrors the reference_analysis_jobs DB row.
type AnalysisJob struct {
	ID                  string          `json:"id"`
	RefID               string          `json:"ref_id"`
	ProjectID           string          `json:"project_id"`
	Status              string          `json:"status"` // pending|running|completed|failed|cancelled
	TotalChunks         int             `json:"total_chunks"`
	DoneChunks          int             `json:"done_chunks"`
	ErrorMessage        string          `json:"error_message,omitempty"`
	ExtractedCharacters json.RawMessage `json:"extracted_characters,omitempty"`
	ExtractedWorld      json.RawMessage `json:"extracted_world,omitempty"`
	ExtractedOutline    json.RawMessage `json:"extracted_outline,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// ── Deep Analysis Service ─────────────────────────────────────────────────────

type ReferenceDeepAnalysisService struct {
	db         *pgxpool.Pool
	sidecarURL string
	references *ReferenceService
	characters *CharacterService
	outlines   *OutlineService
	worldBible *WorldBibleService
	taskQueue  *TaskQueueService
	agentRoute *AgentRoutingService
	logger     *zap.Logger

	// http client reused for all sidecar calls (no overall timeout — chunks can be slow)
	httpClient *http.Client
}

func NewReferenceDeepAnalysisService(
	db *pgxpool.Pool,
	sidecarURL string,
	references *ReferenceService,
	characters *CharacterService,
	outlines *OutlineService,
	worldBible *WorldBibleService,
	taskQueue *TaskQueueService,
	agentRoute *AgentRoutingService,
	logger *zap.Logger,
) *ReferenceDeepAnalysisService {
	s := &ReferenceDeepAnalysisService{
		db:         db,
		sidecarURL: sidecarURL,
		references: references,
		characters: characters,
		outlines:   outlines,
		worldBible: worldBible,
		taskQueue:  taskQueue,
		agentRoute: agentRoute,
		logger:     logger,
		httpClient: &http.Client{}, // no global timeout; per-request context used
	}
	taskQueue.RegisterHandler(taskTypeRefAnalysis, s.runAnalysisTask)
	return s
}

// ── Public API ────────────────────────────────────────────────────────────────

// StartDeepAnalysis creates (or resumes) an analysis job + task-queue entry.
// If there is a cancelled or failed job for this reference that has partial
// chunk results, it is resumed from its last checkpoint instead of starting
// from scratch.  Returns 202 immediately; poll GetDeepAnalysisJob for progress.
func (s *ReferenceDeepAnalysisService) StartDeepAnalysis(ctx context.Context, refID, projectID string) (*AnalysisJob, error) {
	// Look for a resumable job (cancelled or failed) that has partial work.
	var resumableID string
	var resumableDone int
	var resumableTotal int
	_ = s.db.QueryRow(ctx,
		`SELECT id, done_chunks, total_chunks
		 FROM reference_analysis_jobs
		 WHERE ref_id = $1
		   AND status IN ('cancelled','failed')
		   AND done_chunks > 0
		 ORDER BY created_at DESC LIMIT 1`, refID,
	).Scan(&resumableID, &resumableDone, &resumableTotal)

	var jobID string
	var job AnalysisJob

	if resumableID != "" {
		// Resume the existing job: mark it pending again, preserve chunk_results.
		s.logger.Info("resuming deep analysis from checkpoint",
			zap.String("job_id", resumableID),
			zap.Int("done_chunks", resumableDone),
			zap.Int("total_chunks", resumableTotal))
		err := s.db.QueryRow(ctx,
			`UPDATE reference_analysis_jobs
			 SET status='pending', error_message=NULL, updated_at=NOW()
			 WHERE id=$1
			 RETURNING id, ref_id, project_id, status, total_chunks, done_chunks,
			           COALESCE(error_message,''), created_at, updated_at`,
			resumableID).Scan(
			&job.ID, &job.RefID, &job.ProjectID, &job.Status,
			&job.TotalChunks, &job.DoneChunks, &job.ErrorMessage,
			&job.CreatedAt, &job.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("resume analysis job: %w", err)
		}
		jobID = resumableID
	} else {
		// Cancel any orphaned running/pending jobs first.
		if _, dbErr := s.db.Exec(ctx,
			`UPDATE reference_analysis_jobs SET status='cancelled', updated_at=NOW()
			 WHERE ref_id = $1 AND status IN ('pending','running')`, refID); dbErr != nil {
			s.logger.Warn("could not cancel previous analysis jobs", zap.String("ref_id", refID), zap.Error(dbErr))
		}
		// Create a fresh job.
		jobID = uuid.New().String()
		err := s.db.QueryRow(ctx,
			`INSERT INTO reference_analysis_jobs (id, ref_id, project_id, status)
			 VALUES ($1, $2, $3, 'pending')
			 RETURNING id, ref_id, project_id, status, total_chunks, done_chunks,
			           COALESCE(error_message,''), created_at, updated_at`,
			jobID, refID, projectID).Scan(
			&job.ID, &job.RefID, &job.ProjectID, &job.Status,
			&job.TotalChunks, &job.DoneChunks, &job.ErrorMessage,
			&job.CreatedAt, &job.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("create analysis job: %w", err)
		}
	}

	// Link the job to the reference row.
	if _, dbErr := s.db.Exec(ctx, `UPDATE reference_materials SET analysis_job_id=$1 WHERE id=$2`, jobID, refID); dbErr != nil {
		s.logger.Warn("could not link analysis job to reference", zap.String("ref_id", refID), zap.String("job_id", jobID), zap.Error(dbErr))
	}

	payload, _ := json.Marshal(map[string]string{
		"job_id":     jobID,
		"ref_id":     refID,
		"project_id": projectID,
	})
	if _, err := s.taskQueue.Enqueue(ctx, models.CreateTaskRequest{
		ProjectID:   projectID,
		TaskType:    taskTypeRefAnalysis,
		Payload:     payload,
		Priority:    1,
		MaxAttempts: 1, // outer retries handled inside the handler
	}); err != nil {
		return nil, fmt.Errorf("enqueue analysis task: %w", err)
	}

	return &job, nil
}

// GetJob returns the current state of an analysis job.
func (s *ReferenceDeepAnalysisService) GetJob(ctx context.Context, jobID string) (*AnalysisJob, error) {
	var job AnalysisJob
	err := s.db.QueryRow(ctx,
		`SELECT id, ref_id, project_id, status, total_chunks, done_chunks,
		        COALESCE(error_message,''),
		        extracted_characters, extracted_world, extracted_outline,
		        created_at, updated_at
		 FROM reference_analysis_jobs WHERE id = $1`, jobID).Scan(
		&job.ID, &job.RefID, &job.ProjectID, &job.Status,
		&job.TotalChunks, &job.DoneChunks, &job.ErrorMessage,
		&job.ExtractedCharacters, &job.ExtractedWorld, &job.ExtractedOutline,
		&job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get analysis job: %w", err)
	}
	return &job, nil
}

// GetJobByRef returns the latest analysis job for a reference material (may be nil).
func (s *ReferenceDeepAnalysisService) GetJobByRef(ctx context.Context, refID string) (*AnalysisJob, error) {
	var job AnalysisJob
	err := s.db.QueryRow(ctx,
		`SELECT id, ref_id, project_id, status, total_chunks, done_chunks,
		        COALESCE(error_message,''),
		        extracted_characters, extracted_world, extracted_outline,
		        created_at, updated_at
		 FROM reference_analysis_jobs WHERE ref_id = $1
		 ORDER BY created_at DESC LIMIT 1`, refID).Scan(
		&job.ID, &job.RefID, &job.ProjectID, &job.Status,
		&job.TotalChunks, &job.DoneChunks, &job.ErrorMessage,
		&job.ExtractedCharacters, &job.ExtractedWorld, &job.ExtractedOutline,
		&job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return nil, nil // no job yet is not an error
	}
	return &job, nil
}

// CancelJob cancels a pending or running job.
func (s *ReferenceDeepAnalysisService) CancelJob(ctx context.Context, jobID string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE reference_analysis_jobs SET status='cancelled', updated_at=NOW()
		 WHERE id=$1 AND status IN ('pending','running')`, jobID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("job %s not found or not cancellable", jobID)
	}
	return nil
}

// ImportResult writes the extracted entities from a completed job into the project's
// world_bibles / characters / outlines tables.  Only call after job is 'completed'.
func (s *ReferenceDeepAnalysisService) ImportResult(ctx context.Context, jobID, projectID string) error {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status != "completed" {
		return fmt.Errorf("job %s is not completed (status: %s)", jobID, job.Status)
	}

	// Import characters
	if len(job.ExtractedCharacters) > 2 {
		var chars []map[string]interface{}
		if json.Unmarshal(job.ExtractedCharacters, &chars) == nil {
			for _, ch := range chars {
				name, _ := ch["name"].(string)
				if name == "" {
					continue
				}
				roleType, _ := ch["role"].(string)
				if roleType == "" {
					roleType = "other"
				}
				desc, _ := ch["description"].(string)
				traits, _ := ch["traits"].([]interface{})
				var traitStrs []string
				for _, t := range traits {
					if ts, ok := t.(string); ok {
						traitStrs = append(traitStrs, ts)
					}
				}
				profileData := map[string]interface{}{
					"description":   desc,
					"traits":        traitStrs,
					"source_ref_id": job.RefID,
					"imported_from": "reference_analysis",
				}
				profileJSON, _ := json.Marshal(profileData)
				// Insert; ignore conflict (duplicate name+project)
				if _, dbErr := s.db.Exec(ctx,
					`INSERT INTO characters (project_id, name, role_type, profile)
					 VALUES ($1, $2, $3, $4)
					 ON CONFLICT DO NOTHING`,
					projectID, name, normalizeRole(roleType), profileJSON); dbErr != nil {
					s.logger.Warn("import character failed", zap.String("name", name), zap.Error(dbErr))
				}
			}
		}
	}

	// Import world bible (merge into existing or create)
	if len(job.ExtractedWorld) > 2 {
		var worldData map[string]interface{}
		if json.Unmarshal(job.ExtractedWorld, &worldData) == nil {
			worldData["source_ref_id"] = job.RefID
			worldData["imported_from"] = "reference_analysis"
			worldJSON, _ := json.Marshal(worldData)
			// Try update first; if no row create one
			tag, dbErr := s.db.Exec(ctx,
				`UPDATE world_bibles SET content = content || $1::jsonb, version = version + 1
				 WHERE project_id = $2`,
				worldJSON, projectID)
			if dbErr != nil {
				s.logger.Warn("world bible update failed", zap.String("project_id", projectID), zap.Error(dbErr))
			} else if tag.RowsAffected() == 0 {
				if _, dbErr2 := s.db.Exec(ctx,
					`INSERT INTO world_bibles (project_id, content, version)
					 VALUES ($1, $2, 1) ON CONFLICT DO NOTHING`,
					projectID, worldJSON); dbErr2 != nil {
					s.logger.Warn("world bible insert failed", zap.String("project_id", projectID), zap.Error(dbErr2))
				}
			}
		}
	}

	// Import outline nodes
	if len(job.ExtractedOutline) > 2 {
		var outlineNodes []map[string]interface{}
		if json.Unmarshal(job.ExtractedOutline, &outlineNodes) == nil {
			for i, node := range outlineNodes {
				title, _ := node["title"].(string)
				if title == "" {
					continue
				}
				summary, _ := node["summary"].(string)
				levelF, _ := node["level"].(float64)
				level := int(levelF)
				if level < 1 {
					level = 1
				}
				contentData := map[string]interface{}{
					"summary": summary,
					"source":  "reference_analysis",
					"ref_id":  job.RefID,
				}
				contentJSON, _ := json.Marshal(contentData)
				if _, dbErr := s.db.Exec(ctx,
					`INSERT INTO outlines (project_id, level, order_num, title, content)
					 VALUES ($1, $2, $3, $4, $5)`,
					projectID, level, i+1, title, contentJSON); dbErr != nil {
					s.logger.Warn("import outline node failed", zap.String("title", title), zap.Error(dbErr))
				}
			}
		}
	}

	return nil
}

// ── TaskQueue handler (runs as goroutine) ─────────────────────────────────────

func (s *ReferenceDeepAnalysisService) runAnalysisTask(ctx context.Context, task models.TaskQueueItem) error {
	var payload struct {
		JobID     string `json:"job_id"`
		RefID     string `json:"ref_id"`
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return fmt.Errorf("bad payload: %w", err)
	}

	jobID := payload.JobID
	refID := payload.RefID
	projectID := payload.ProjectID

	// Mark job as running
	if _, err := s.db.Exec(ctx,
		`UPDATE reference_analysis_jobs SET status='running', updated_at=NOW() WHERE id=$1`, jobID); err != nil {
		return err
	}

	// Check for cancellation
	if cancelled, _ := s.isJobCancelled(ctx, jobID); cancelled {
		return nil
	}

	// Resolve LLM config for the reference_analyzer agent type
	llmCfg, err := s.resolveLLMConfig(ctx, projectID)
	if err != nil {
		s.logger.Warn("could not resolve reference_analyzer LLM config, using defaults", zap.Error(err))
		llmCfg = nil
	}

	// Get full text (file or chapters)
	text, err := s.getFullText(ctx, refID)
	if err != nil || text == "" {
		s.failJob(ctx, jobID, "no content to analyze: "+err.Error())
		return fmt.Errorf("no content: %w", err)
	}

	// Split into chunks
	chunks := splitIntoChunks(text, chunkSize)
	totalChunks := len(chunks)

	if _, dbErr := s.db.Exec(ctx,
		`UPDATE reference_analysis_jobs SET total_chunks=$1, updated_at=NOW() WHERE id=$2`,
		totalChunks, jobID); dbErr != nil {
		s.logger.Warn("could not update total_chunks", zap.String("job_id", jobID), zap.Error(dbErr))
	}

	// Load any previously completed chunk results (checkpoint resume).
	var existingResultsRaw []byte
	_ = s.db.QueryRow(ctx,
		`SELECT COALESCE(chunk_results, '[]'::jsonb) FROM reference_analysis_jobs WHERE id=$1`, jobID,
	).Scan(&existingResultsRaw)
	var chunkResults []chunkResult
	if len(existingResultsRaw) > 2 { // more than empty array []
		_ = json.Unmarshal(existingResultsRaw, &chunkResults)
	}
	skipChunks := len(chunkResults) // skip chunks already saved
	if skipChunks > totalChunks {
		skipChunks = 0 // guard: text may have changed, restart
		chunkResults = nil
	}

	s.logger.Info("deep analysis started",
		zap.String("job_id", jobID),
		zap.String("ref_id", refID),
		zap.Int("total_chunks", totalChunks),
		zap.Int("resuming_from_chunk", skipChunks),
		zap.Int("text_length", len(text)))

	for i, chunk := range chunks {
		// Skip chunks already completed in a previous run.
		if i < skipChunks {
			continue
		}

		// Check cancellation between chunks
		if cancelled, _ := s.isJobCancelled(ctx, jobID); cancelled {
			s.logger.Info("deep analysis cancelled", zap.String("job_id", jobID))
			return nil
		}

		result, err := s.analyzeChunk(ctx, jobID, projectID, chunk, i, totalChunks, llmCfg)
		if err != nil {
			// Non-fatal: record empty result for this chunk and continue.
			s.logger.Warn("chunk analysis failed, skipping",
				zap.Int("chunk", i), zap.Error(err))
			chunkResults = append(chunkResults, chunkResult{})
		} else {
			chunkResults = append(chunkResults, result)
		}

		// Persist progress: done_chunks + the latest chunk_results snapshot.
		resultsJSON, _ := json.Marshal(chunkResults)
		if _, dbErr := s.db.Exec(ctx,
			`UPDATE reference_analysis_jobs
			 SET done_chunks=$1, chunk_results=$2, updated_at=NOW()
			 WHERE id=$3`,
			i+1, resultsJSON, jobID); dbErr != nil {
			s.logger.Warn("could not update progress", zap.String("job_id", jobID), zap.Error(dbErr))
		}
	}

	// Merge all chunk results
	merged, err := s.mergeChunks(ctx, jobID, projectID, chunkResults, llmCfg)
	if err != nil {
		s.failJob(ctx, jobID, "merge failed: "+err.Error())
		return fmt.Errorf("merge: %w", err)
	}

	charsJSON := mustMarshalRaw(merged["characters"])
	worldJSON := mustMarshalRaw(merged["world"])
	outlineJSON := mustMarshalRaw(merged["outline"])

	_, err = s.db.Exec(ctx,
		`UPDATE reference_analysis_jobs
		 SET status='completed', extracted_characters=$1, extracted_world=$2, extracted_outline=$3, updated_at=NOW()
		 WHERE id=$4`,
		charsJSON, worldJSON, outlineJSON, jobID)
	if err != nil {
		return fmt.Errorf("save job result: %w", err)
	}

	// Also update the legacy style/narrative/atmosphere layers so existing code still works
	styleLayer, _ := json.Marshal(map[string]interface{}{"source": "deep_analysis", "job_id": jobID})
	s.references.UpdateAnalysis(ctx, refID,
		json.RawMessage(styleLayer),
		json.RawMessage(`{}`),
		json.RawMessage(`{}`))

	s.logger.Info("deep analysis completed", zap.String("job_id", jobID))
	return nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (s *ReferenceDeepAnalysisService) getFullText(ctx context.Context, refID string) (string, error) {
	ref, err := s.references.Get(ctx, refID)
	if err != nil {
		return "", err
	}
	if ref == nil {
		return "", fmt.Errorf("reference %s not found", refID)
	}
	if ref.FilePath != "" {
		data, err := os.ReadFile(ref.FilePath)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", ref.FilePath, err)
		}
		return string(data), nil
	}
	return s.references.GetChaptersContent(ctx, refID)
}

func (s *ReferenceDeepAnalysisService) isJobCancelled(ctx context.Context, jobID string) (bool, error) {
	var status string
	err := s.db.QueryRow(ctx,
		`SELECT status FROM reference_analysis_jobs WHERE id=$1`, jobID).Scan(&status)
	if err != nil {
		return false, err
	}
	return status == "cancelled", nil
}

func (s *ReferenceDeepAnalysisService) failJob(ctx context.Context, jobID, msg string) {
	if _, dbErr := s.db.Exec(ctx,
		`UPDATE reference_analysis_jobs SET status='failed', error_message=$1, updated_at=NOW() WHERE id=$2`,
		msg, jobID); dbErr != nil {
		s.logger.Error("could not mark job as failed", zap.String("job_id", jobID), zap.String("msg", msg), zap.Error(dbErr))
	}
}

type chunkResult = map[string]interface{}

func (s *ReferenceDeepAnalysisService) analyzeChunk(
	ctx context.Context,
	jobID, projectID, chunk string,
	chunkIndex, totalChunks int,
	llmCfg interface{},
) (chunkResult, error) {
	body := map[string]interface{}{
		"job_id":       jobID,
		"project_id":   projectID,
		"chunk_text":   chunk,
		"chunk_index":  chunkIndex,
		"total_chunks": totalChunks,
	}
	if llmCfg != nil {
		body["llm_config"] = llmCfg
	}
	bodyJSON, _ := json.Marshal(body)

	retryConfig := retry.Config{
		MaxAttempts: 5,
		BaseDelay:   3 * time.Second,
		MaxDelay:    60 * time.Second,
		Jitter:      0.25,
	}

	var result map[string]interface{}
	err := retry.Do(ctx, retryConfig, func(attempt int) (bool, error) {
		// Each retry re-creates the request so the body isn't consumed
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			s.sidecarURL+"/deep-analyze/chunk", bytes.NewReader(bodyJSON))
		if err != nil {
			return false, err // bad req: permanent
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			s.logger.Warn("sidecar chunk call failed (attempt %d)", zap.Int("attempt", attempt), zap.Error(err))
			return true, err // network error: retry
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode >= 500 {
			s.logger.Warn("sidecar returned retryable status",
				zap.Int("status", resp.StatusCode), zap.Int("attempt", attempt))
			return true, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("sidecar permanent error HTTP %d", resp.StatusCode) // 4xx: permanent
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return false, fmt.Errorf("decode response: %w", err)
		}
		return false, nil
	})
	return result, err
}

func (s *ReferenceDeepAnalysisService) mergeChunks(
	ctx context.Context,
	jobID, projectID string,
	results []chunkResult,
	llmCfg interface{},
) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"job_id":     jobID,
		"project_id": projectID,
		"chunks":     results,
	}
	if llmCfg != nil {
		body["llm_config"] = llmCfg
	}
	bodyJSON, _ := json.Marshal(body)

	retryConfig := retry.Config{
		MaxAttempts: 5,
		BaseDelay:   4 * time.Second,
		MaxDelay:    90 * time.Second,
		Jitter:      0.2,
	}

	var merged map[string]interface{}
	err := retry.Do(ctx, retryConfig, func(attempt int) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			s.sidecarURL+"/deep-analyze/merge", bytes.NewReader(bodyJSON))
		if err != nil {
			return false, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return true, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return true, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("merge permanent error HTTP %d", resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(&merged); err != nil {
			return false, err
		}
		return false, nil
	})
	return merged, err
}

func (s *ReferenceDeepAnalysisService) resolveLLMConfig(ctx context.Context, projectID string) (map[string]interface{}, error) {
	if s.agentRoute == nil {
		return nil, nil
	}
	return s.agentRoute.ResolveForAgent(ctx, "reference_analyzer", projectID)
}

// splitIntoChunks splits a large string into chunks of at most maxRunes runes,
// cutting only at paragraph (newline) boundaries when possible.
func splitIntoChunks(text string, maxRunes int) []string {
	if utf8.RuneCountInString(text) <= maxRunes {
		return []string{text}
	}

	var chunks []string
	runes := []rune(text)
	start := 0
	for start < len(runes) {
		end := start + maxRunes
		if end >= len(runes) {
			chunks = append(chunks, string(runes[start:]))
			break
		}
		// Walk backwards to find a paragraph break
		cut := end
		for cut > start+maxRunes/2 {
			if runes[cut] == '\n' {
				break
			}
			cut--
		}
		if cut == start+maxRunes/2 {
			cut = end // no paragraph break found; cut hard
		}
		chunks = append(chunks, string(runes[start:cut]))
		start = cut
	}
	return chunks
}

func mustMarshal(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil || v == nil {
		return json.RawMessage("null")
	}
	return b
}

func mustMarshalRaw(v interface{}) []byte {
	if v == nil {
		return []byte("null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

func normalizeRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch {
	case strings.Contains(role, "主角") || role == "protagonist" || role == "main":
		return "protagonist"
	case strings.Contains(role, "反派") || role == "antagonist":
		return "antagonist"
	case strings.Contains(role, "配角") || role == "supporting":
		return "supporting"
	default:
		return "other"
	}
}
