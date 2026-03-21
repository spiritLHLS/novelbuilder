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
	"github.com/jackc/pgx/v5"
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
	ID                      string          `json:"id"`
	RefID                   string          `json:"ref_id"`
	ProjectID               string          `json:"project_id"`
	Status                  string          `json:"status"` // pending|running|completed|failed|cancelled
	TotalChunks             int             `json:"total_chunks"`
	DoneChunks              int             `json:"done_chunks"`
	ErrorMessage            string          `json:"error_message,omitempty"`
	ExtractedCharacters     json.RawMessage `json:"extracted_characters,omitempty"`
	ExtractedWorld          json.RawMessage `json:"extracted_world,omitempty"`
	ExtractedOutline        json.RawMessage `json:"extracted_outline,omitempty"`
	ExtractedGlossary       json.RawMessage `json:"extracted_glossary,omitempty"`
	ExtractedForeshadowings json.RawMessage `json:"extracted_foreshadowings,omitempty"`
	CreatedAt               time.Time       `json:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at"`
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
		        COALESCE(extracted_characters, '[]'::jsonb),
		        COALESCE(extracted_world, '{}'::jsonb),
		        COALESCE(extracted_outline, '[]'::jsonb),
		        COALESCE(extracted_glossary, '[]'::jsonb),
		        COALESCE(extracted_foreshadowings, '[]'::jsonb),
		        created_at, updated_at
		 FROM reference_analysis_jobs WHERE id = $1`, jobID).Scan(
		&job.ID, &job.RefID, &job.ProjectID, &job.Status,
		&job.TotalChunks, &job.DoneChunks, &job.ErrorMessage,
		&job.ExtractedCharacters, &job.ExtractedWorld, &job.ExtractedOutline,
		&job.ExtractedGlossary, &job.ExtractedForeshadowings,
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
		        COALESCE(extracted_characters, '[]'::jsonb),
		        COALESCE(extracted_world, '{}'::jsonb),
		        COALESCE(extracted_outline, '[]'::jsonb),
		        COALESCE(extracted_glossary, '[]'::jsonb),
		        COALESCE(extracted_foreshadowings, '[]'::jsonb),
		        created_at, updated_at
		 FROM reference_analysis_jobs WHERE ref_id = $1
		 ORDER BY created_at DESC LIMIT 1`, refID).Scan(
		&job.ID, &job.RefID, &job.ProjectID, &job.Status,
		&job.TotalChunks, &job.DoneChunks, &job.ErrorMessage,
		&job.ExtractedCharacters, &job.ExtractedWorld, &job.ExtractedOutline,
		&job.ExtractedGlossary, &job.ExtractedForeshadowings,
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

// ResetAnalysis cancels any running/pending job for the reference, then deletes all
// analysis job records so the next StartDeepAnalysis begins completely from scratch.
func (s *ReferenceDeepAnalysisService) ResetAnalysis(ctx context.Context, refID string) error {
	// First mark any live jobs as cancelled so background goroutines stop at the
	// next isJobCancelled check.
	if _, err := s.db.Exec(ctx,
		`UPDATE reference_analysis_jobs SET status='cancelled', updated_at=NOW()
		 WHERE ref_id=$1 AND status IN ('pending','running')`, refID); err != nil {
		s.logger.Warn("reset: could not cancel live jobs", zap.String("ref_id", refID), zap.Error(err))
	}
	// Delete all jobs for this reference (cascade nulls analysis_job_id on reference_materials).
	if _, err := s.db.Exec(ctx,
		`DELETE FROM reference_analysis_jobs WHERE ref_id=$1`, refID); err != nil {
		return fmt.Errorf("reset analysis jobs: %w", err)
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

	// Import characters — batch insert to avoid N+1 queries.
	if len(job.ExtractedCharacters) > 2 {
		var chars []map[string]interface{}
		if json.Unmarshal(job.ExtractedCharacters, &chars) == nil && len(chars) > 0 {
			b := &pgx.Batch{}
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
				motivation, _ := ch["motivation"].(string)
				growthArc, _ := ch["growth_arc"].(string)
				traits, _ := ch["traits"].([]interface{})
				var traitStrs []string
				for _, t := range traits {
					if ts, ok := t.(string); ok {
						traitStrs = append(traitStrs, ts)
					}
				}
				// Convert relationships array [{name, description}] → map {name: description}
				relMap := map[string]string{}
				if relList, ok := ch["relationships"].([]interface{}); ok {
					for _, r := range relList {
						if rm, ok := r.(map[string]interface{}); ok {
							rName, _ := rm["name"].(string)
							rDesc, _ := rm["description"].(string)
							if rName != "" {
								relMap[rName] = rDesc
							}
						}
					}
				}
				profileData := map[string]interface{}{
					"backstory":          desc,
					"personality_traits": traitStrs,
					"motivation":         motivation,
					"growth_arc":         growthArc,
					"relationships":      relMap,
					"source_ref_id":      job.RefID,
					"imported_from":      "reference_analysis",
				}
				profileJSON, _ := json.Marshal(profileData)
				b.Queue(
					`INSERT INTO characters (project_id, name, role_type, profile)
					 VALUES ($1, $2, $3, $4)
					 ON CONFLICT (project_id, name) DO UPDATE
					 SET role_type = EXCLUDED.role_type,
					     profile   = characters.profile || EXCLUDED.profile,
					     updated_at = NOW()`,
					projectID, name, normalizeRole(roleType), profileJSON)
			}
			if b.Len() > 0 {
				br := s.db.SendBatch(ctx, b)
				for i := 0; i < b.Len(); i++ {
					if _, dbErr := br.Exec(); dbErr != nil {
						s.logger.Warn("import character batch failed", zap.Int("idx", i), zap.Error(dbErr))
					}
				}
				br.Close()
			}
		}
	}

	// Import world bible (merge into existing or create)
	if len(job.ExtractedWorld) > 2 {
		var worldData map[string]interface{}
		if json.Unmarshal(job.ExtractedWorld, &worldData) == nil {
			// Map Python sidecar keys to the keys the frontend expects
			setting, _ := worldData["setting"].(string)
			timePeriod, _ := worldData["time_period"].(string)
			locations, _ := worldData["locations"].([]interface{})
			systems, _ := worldData["systems"].([]interface{})
			var locStrs, sysStrs []string
			for _, l := range locations {
				if ls, ok := l.(string); ok {
					locStrs = append(locStrs, ls)
				}
			}
			for _, sys := range systems {
				if ss, ok := sys.(string); ok {
					sysStrs = append(sysStrs, ss)
				}
			}
			mappedWorld := map[string]interface{}{
				"world_view":     setting,
				"era_background": timePeriod,
				"geography":      strings.Join(locStrs, "、"),
				"power_system":   strings.Join(sysStrs, "、"),
				"source_ref_id":  job.RefID,
				"imported_from":  "reference_analysis",
			}
			worldJSON, _ := json.Marshal(mappedWorld)
			// Try update first; if no row create one
			tag, dbErr := s.db.Exec(ctx,
				`UPDATE world_bibles SET content = COALESCE(content, '{}') || $1::jsonb, version = version + 1
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

	// Import outline nodes — batch insert to avoid N+1 queries.
	if len(job.ExtractedOutline) > 2 {
		var outlineNodes []map[string]interface{}
		if json.Unmarshal(job.ExtractedOutline, &outlineNodes) == nil && len(outlineNodes) > 0 {
			b := &pgx.Batch{}
			orderNum := 0
			for _, node := range outlineNodes {
				title, _ := node["title"].(string)
				if title == "" {
					continue
				}
				orderNum++
				summary, _ := node["summary"].(string)
				levelStr := "meso"
				if ls, ok := node["level"].(string); ok && ls != "" {
					valid := map[string]bool{"macro": true, "meso": true, "micro": true}
					if valid[ls] {
						levelStr = ls
					}
				} else if lf, ok := node["level"].(float64); ok {
					switch int(lf) {
					case 1:
						levelStr = "macro"
					case 3:
						levelStr = "micro"
					default:
						levelStr = "meso"
					}
				}
				// Extract involved_characters array
				var involvedChars []string
				if icRaw, ok := node["involved_characters"].([]interface{}); ok {
					for _, ic := range icRaw {
						if ics, ok := ic.(string); ok && ics != "" {
							involvedChars = append(involvedChars, ics)
						}
					}
				}
				contentData := map[string]interface{}{
					"content":             summary,
					"key_events":          summary,
					"involved_characters": involvedChars,
					"source":              "reference_analysis",
					"ref_id":              job.RefID,
				}
				contentJSON, _ := json.Marshal(contentData)
				b.Queue(
					`INSERT INTO outlines (project_id, level, order_num, title, content)
					 VALUES ($1, $2, $3, $4, $5)`,
					projectID, levelStr, orderNum, title, contentJSON)
			}
			if b.Len() > 0 {
				br := s.db.SendBatch(ctx, b)
				for i := 0; i < b.Len(); i++ {
					if _, dbErr := br.Exec(); dbErr != nil {
						s.logger.Warn("import outline batch failed", zap.Int("idx", i), zap.Error(dbErr))
					}
				}
				br.Close()
			}
		}
	}

	// Import glossary terms — batch insert to avoid N+1 queries.
	if len(job.ExtractedGlossary) > 2 {
		var terms []map[string]interface{}
		if json.Unmarshal(job.ExtractedGlossary, &terms) == nil && len(terms) > 0 {
			b := &pgx.Batch{}
			for _, t := range terms {
				term, _ := t["term"].(string)
				if term == "" {
					continue
				}
				definition, _ := t["definition"].(string)
				category, _ := t["category"].(string)
				if category == "" {
					category = "concept"
				}
				b.Queue(
					`INSERT INTO glossary_terms (project_id, term, definition, category)
					 VALUES ($1, $2, $3, $4)
					 ON CONFLICT (project_id, term) DO UPDATE SET definition = EXCLUDED.definition`,
					projectID, term, definition, category)
			}
			if b.Len() > 0 {
				br := s.db.SendBatch(ctx, b)
				for i := 0; i < b.Len(); i++ {
					if _, dbErr := br.Exec(); dbErr != nil {
						s.logger.Warn("import glossary batch failed", zap.Int("idx", i), zap.Error(dbErr))
					}
				}
				br.Close()
			}
		}
	}

	// Import foreshadowings — batch insert to avoid N+1 queries.
	if len(job.ExtractedForeshadowings) > 2 {
		var foreshadowings []map[string]interface{}
		if json.Unmarshal(job.ExtractedForeshadowings, &foreshadowings) == nil && len(foreshadowings) > 0 {
			b := &pgx.Batch{}
			for _, f := range foreshadowings {
				content, _ := f["content"].(string)
				if content == "" {
					continue
				}
				priority := 3
				if pf, ok := f["priority"].(float64); ok {
					priority = int(pf)
				}
				relChars, _ := f["related_characters"].([]interface{})
				var tags []string
				for _, rc := range relChars {
					if rs, ok := rc.(string); ok {
						tags = append(tags, rs)
					}
				}
				b.Queue(
					`INSERT INTO foreshadowings (project_id, content, embed_method, priority, tags, status)
					 VALUES ($1, $2, 'reference_import', $3, $4, 'planned')
					 ON CONFLICT DO NOTHING`,
					projectID, content, priority, tags)
			}
			if b.Len() > 0 {
				br := s.db.SendBatch(ctx, b)
				for i := 0; i < b.Len(); i++ {
					if _, dbErr := br.Exec(); dbErr != nil {
						s.logger.Warn("import foreshadowing batch failed", zap.Int("idx", i), zap.Error(dbErr))
					}
				}
				br.Close()
			}
		}
	}

	// Mark the reference material as completed so the RAG rebuild can find it.
	if _, dbErr := s.db.Exec(ctx,
		`UPDATE reference_materials SET status = 'completed', updated_at = NOW() WHERE id = $1`,
		job.RefID); dbErr != nil {
		s.logger.Warn("could not mark reference as completed after import",
			zap.String("ref_id", job.RefID), zap.Error(dbErr))
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

	// Compute dynamic chunk size based on the model's context window so we
	// maximize coverage per API call while avoiding context overflow.
	chunkSz := chunkSize // default fallback (80_000 chars)
	if llmCfg != nil {
		if model, ok := llmCfg["model"].(string); ok && model != "" {
			chunkSz = computeChunkChars(model, 4096)
		}
	}

	// Split into chunks
	chunks := splitIntoChunks(text, chunkSz)
	totalChunks := len(chunks)

	if _, dbErr := s.db.Exec(ctx,
		`UPDATE reference_analysis_jobs SET total_chunks=$1, updated_at=NOW() WHERE id=$2`,
		totalChunks, jobID); dbErr != nil {
		s.logger.Warn("could not update total_chunks", zap.String("job_id", jobID), zap.Error(dbErr))
	}

	// Load previously saved chunk results for checkpoint resume.
	// A nil entry (JSON null) means that chunk failed last time and must be retried.
	// An empty map {} means the LLM returned no data but the call succeeded.
	var existingResultsRaw []byte
	_ = s.db.QueryRow(ctx,
		`SELECT COALESCE(chunk_results, '[]'::jsonb) FROM reference_analysis_jobs WHERE id=$1`, jobID,
	).Scan(&existingResultsRaw)
	var chunkResults []chunkResult
	if len(existingResultsRaw) > 2 { // more than empty array []
		_ = json.Unmarshal(existingResultsRaw, &chunkResults)
	}

	// Count consecutive non-nil entries from the start — these are the successfully
	// completed chunks we can skip.  The first nil marks where the run broke.
	skipChunks := 0
	for _, r := range chunkResults {
		if r == nil {
			break
		}
		skipChunks++
	}
	if skipChunks > totalChunks {
		skipChunks = 0 // guard: text changed between runs
	}
	// Discard the failed tail so we only keep the verified-successful prefix.
	chunkResults = chunkResults[:skipChunks]

	// Sync done_chunks to the actual checkpoint so the UI shows correct progress.
	if _, dbErr := s.db.Exec(ctx,
		`UPDATE reference_analysis_jobs SET done_chunks=$1, updated_at=NOW() WHERE id=$2`,
		skipChunks, jobID); dbErr != nil {
		s.logger.Warn("could not sync done_chunks", zap.String("job_id", jobID), zap.Error(dbErr))
	}

	s.logger.Info("deep analysis started",
		zap.String("job_id", jobID),
		zap.String("ref_id", refID),
		zap.Int("total_chunks", totalChunks),
		zap.Int("chunk_chars", chunkSz),
		zap.Int("resuming_from_chunk", skipChunks),
		zap.Int("text_length", len(text)))

	for i, chunk := range chunks {
		// Skip chunks already successfully completed in a previous run.
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
			// Non-fatal: store nil (serialises as JSON null) so this chunk is retried on resume.
			s.logger.Warn("chunk analysis failed, will retry on resume",
				zap.Int("chunk", i), zap.Error(err))
			chunkResults = append(chunkResults, nil)
		} else {
			chunkResults = append(chunkResults, result)
		}

		// Persist progress: done_chunks + checkpoint snapshot.
		resultsJSON, _ := json.Marshal(chunkResults)
		if _, dbErr := s.db.Exec(ctx,
			`UPDATE reference_analysis_jobs
			 SET done_chunks=$1, chunk_results=$2, updated_at=NOW()
			 WHERE id=$3`,
			i+1, resultsJSON, jobID); dbErr != nil {
			s.logger.Warn("could not update progress", zap.String("job_id", jobID), zap.Error(dbErr))
		}
	}

	// Filter out nil (failed) chunks before merging — they carry no usable data.
	validChunks := make([]chunkResult, 0, len(chunkResults))
	for _, r := range chunkResults {
		if r != nil {
			validChunks = append(validChunks, r)
		}
	}

	// Merge all chunk results
	merged, err := s.mergeChunks(ctx, jobID, projectID, validChunks, llmCfg)
	if err != nil {
		s.failJob(ctx, jobID, "merge failed: "+err.Error())
		return fmt.Errorf("merge: %w", err)
	}

	charsJSON := mustMarshalRaw(merged["characters"])
	worldJSON := mustMarshalRaw(merged["world"])
	outlineJSON := mustMarshalRaw(merged["outline"])
	glossaryJSON := mustMarshalRaw(merged["glossary"])
	foreshadowingsJSON := mustMarshalRaw(merged["foreshadowings"])

	_, err = s.db.Exec(ctx,
		`UPDATE reference_analysis_jobs
		 SET status='completed', extracted_characters=$1, extracted_world=$2, extracted_outline=$3,
		     extracted_glossary=$4, extracted_foreshadowings=$5, updated_at=NOW()
		 WHERE id=$6`,
		charsJSON, worldJSON, outlineJSON, glossaryJSON, foreshadowingsJSON, jobID)
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
		MaxAttempts: 10,
		BaseDelay:   10 * time.Second,
		MaxDelay:    120 * time.Second,
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
		MaxAttempts: 10,
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

// modelContextTokens returns the approximate input context window (in tokens) for a model.
func modelContextTokens(m string) int {
	m = strings.ToLower(m)
	switch {
	case strings.Contains(m, "gpt-4o"):
		return 128_000
	case strings.Contains(m, "gpt-4-turbo"):
		return 128_000
	case strings.Contains(m, "gpt-4"):
		return 8_192
	case strings.Contains(m, "gpt-3.5"):
		return 16_385
	case strings.Contains(m, "deepseek-r1"):
		return 65_536
	case strings.Contains(m, "deepseek"):
		return 65_536
	case strings.Contains(m, "claude-3"):
		return 200_000
	case strings.Contains(m, "qwen"):
		return 131_072
	case strings.Contains(m, "doubao"):
		return 131_072
	default:
		return 32_768
	}
}

// computeChunkChars returns the number of characters per analysis chunk, sized to fit
// inside the model's input window minus the output budget and prompt overhead.
// Assumes ~1.5 chars/token for Chinese prose (conservative).
func computeChunkChars(modelName string, maxOutputTokens int) int {
	ctxTokens := modelContextTokens(modelName)
	promptOverhead := 800 // tokens for system prompt + JSON formatting
	available := ctxTokens - maxOutputTokens - promptOverhead
	if available < 2000 {
		available = 2000
	}
	chars := available * 3 / 2
	if chars > 400_000 {
		chars = 400_000
	}
	return chars
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
