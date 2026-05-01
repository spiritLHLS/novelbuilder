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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/models"
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

type constitutionRuleEntry struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}

func rawJSONToStringSlice(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	items := make([]string, 0)
	appendValue := func(value string) {
		text := strings.TrimSpace(value)
		if text != "" {
			items = append(items, text)
		}
	}
	switch v := decoded.(type) {
	case []interface{}:
		for _, item := range v {
			appendValue(fmt.Sprint(item))
		}
	case string:
		normalized := strings.NewReplacer("；", ";", "，", ",", "、", ",").Replace(v)
		for _, part := range strings.FieldsFunc(normalized, func(r rune) bool { return r == ';' || r == ',' || r == '\n' }) {
			appendValue(part)
		}
	}
	return items
}

func mergeConstitutionRuleJSON(existingRaw, incomingRaw json.RawMessage) json.RawMessage {
	merged := make([]constitutionRuleEntry, 0)
	indexByRule := map[string]int{}
	appendRule := func(rule, reason string) {
		normalizedRule := strings.TrimSpace(rule)
		if normalizedRule == "" {
			return
		}
		normalizedReason := strings.TrimSpace(reason)
		key := strings.ToLower(normalizedRule)
		if idx, ok := indexByRule[key]; ok {
			if merged[idx].Reason == "" && normalizedReason != "" {
				merged[idx].Reason = normalizedReason
			}
			return
		}
		indexByRule[key] = len(merged)
		merged = append(merged, constitutionRuleEntry{Rule: normalizedRule, Reason: normalizedReason})
	}
	mergeRaw := func(raw json.RawMessage) {
		if len(raw) == 0 {
			return
		}
		var decoded []interface{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return
		}
		for _, item := range decoded {
			switch typed := item.(type) {
			case map[string]interface{}:
				appendRule(fmt.Sprint(typed["rule"]), fmt.Sprint(typed["reason"]))
			case string:
				appendRule(typed, "")
			default:
				appendRule(fmt.Sprint(typed), "")
			}
		}
	}
	mergeRaw(existingRaw)
	mergeRaw(incomingRaw)
	if len(merged) == 0 {
		return json.RawMessage(`[]`)
	}
	out, _ := json.Marshal(merged)
	return out
}

func mergeStringListJSON(existingRaw, incomingRaw json.RawMessage) json.RawMessage {
	merged := make([]string, 0)
	seen := map[string]bool{}
	appendList := func(items []string) {
		for _, item := range items {
			text := strings.TrimSpace(item)
			if text == "" {
				continue
			}
			key := strings.ToLower(text)
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, text)
		}
	}
	appendList(rawJSONToStringSlice(existingRaw))
	appendList(rawJSONToStringSlice(incomingRaw))
	if len(merged) == 0 {
		return json.RawMessage(`[]`)
	}
	out, _ := json.Marshal(merged)
	return out
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

	toStringSlice := func(raw any) []string {
		items := make([]string, 0)
		switch v := raw.(type) {
		case []interface{}:
			for _, item := range v {
				text := strings.TrimSpace(fmt.Sprint(item))
				if text != "" {
					items = append(items, text)
				}
			}
		case []string:
			for _, item := range v {
				text := strings.TrimSpace(item)
				if text != "" {
					items = append(items, text)
				}
			}
		case string:
			normalized := strings.NewReplacer("；", ";", "，", ",", "、", ",").Replace(v)
			for _, part := range strings.FieldsFunc(normalized, func(r rune) bool { return r == ';' || r == ',' || r == '\n' }) {
				text := strings.TrimSpace(part)
				if text != "" {
					items = append(items, text)
				}
			}
		}
		return items
	}

	parseConstitutions := func(raw any) (json.RawMessage, json.RawMessage) {
		immutable := make([]constitutionRuleEntry, 0)
		mutable := make([]constitutionRuleEntry, 0)
		seen := map[string]bool{}
		if rules, ok := raw.([]interface{}); ok {
			for _, item := range rules {
				ruleMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				rule := strings.TrimSpace(fmt.Sprint(ruleMap["rule"]))
				if rule == "" {
					continue
				}
				ruleType := strings.ToLower(strings.TrimSpace(fmt.Sprint(ruleMap["type"])))
				if ruleType != "mutable" {
					ruleType = "immutable"
				}
				key := ruleType + ":" + strings.ToLower(rule)
				if seen[key] {
					continue
				}
				seen[key] = true
				entry := constitutionRuleEntry{
					Rule:   rule,
					Reason: strings.TrimSpace(fmt.Sprint(ruleMap["reason"])),
				}
				if ruleType == "mutable" {
					mutable = append(mutable, entry)
				} else {
					immutable = append(immutable, entry)
				}
			}
		}
		immutableJSON, _ := json.Marshal(immutable)
		mutableJSON, _ := json.Marshal(mutable)
		return immutableJSON, mutableJSON
	}

	var outlineNodes []map[string]interface{}
	firstAppearanceByName := map[string]int{}
	if len(job.ExtractedOutline) > 2 && json.Unmarshal(job.ExtractedOutline, &outlineNodes) == nil {
		chapterOrder := 0
		for _, node := range outlineNodes {
			level, _ := node["level"].(string)
			if level == "meso" {
				chapterOrder++
			}
			if chapterOrder == 0 {
				continue
			}
			for _, name := range toStringSlice(node["involved_characters"]) {
				if existing, ok := firstAppearanceByName[name]; !ok || chapterOrder < existing {
					firstAppearanceByName[name] = chapterOrder
				}
			}
		}
		if len(firstAppearanceByName) == 0 {
			order := 0
			for _, node := range outlineNodes {
				order++
				for _, name := range toStringSlice(node["involved_characters"]) {
					if existing, ok := firstAppearanceByName[name]; !ok || order < existing {
						firstAppearanceByName[name] = order
					}
				}
			}
		}
	}

	var chars []map[string]interface{}
	if len(job.ExtractedCharacters) > 2 {
		_ = json.Unmarshal(job.ExtractedCharacters, &chars)
	}

	if len(chars) > 0 {
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
			traitStrs := toStringSlice(ch["traits"])
			relMap := map[string]string{}
			if relList, ok := ch["relationships"].([]interface{}); ok {
				for _, relItem := range relList {
					if rel, ok := relItem.(map[string]interface{}); ok {
						targetName := strings.TrimSpace(fmt.Sprint(rel["name"]))
						relText := strings.TrimSpace(fmt.Sprint(rel["description"]))
						if targetName != "" {
							relMap[targetName] = relText
						}
					}
				}
			}
			profileData := map[string]interface{}{
				"backstory":                desc,
				"personality_traits":       traitStrs,
				"motivation":               motivation,
				"growth_arc":               growthArc,
				"relationships":            relMap,
				"first_appearance_chapter": firstAppearanceByName[name],
				"source_ref_id":            job.RefID,
				"imported_from":            "reference_analysis",
			}
			profileJSON, _ := json.Marshal(profileData)
			b.Queue(
				`INSERT INTO characters (project_id, name, role_type, profile)
				 VALUES ($1, $2, $3, $4)
				 ON CONFLICT (project_id, name) DO UPDATE
				 SET role_type = EXCLUDED.role_type,
				     profile = characters.profile || EXCLUDED.profile,
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
			if closeErr := br.Close(); closeErr != nil {
				s.logger.Warn("character batch close failed", zap.Error(closeErr))
			}
		}
	}

	if len(chars) > 0 {
		charRows, listErr := s.characters.List(ctx, projectID)
		if listErr != nil {
			s.logger.Warn("list characters after reference import failed", zap.Error(listErr))
		} else {
			nameToID := make(map[string]string, len(charRows))
			for _, ch := range charRows {
				nameToID[strings.TrimSpace(ch.Name)] = ch.ID
			}

			type edgeSeed struct {
				charA string
				charB string
				rel   string
				first *int
			}
			relationSeeds := map[string]edgeSeed{}
			for _, ch := range chars {
				sourceName, _ := ch["name"].(string)
				sourceID := nameToID[strings.TrimSpace(sourceName)]
				if sourceID == "" {
					continue
				}
				rels, _ := ch["relationships"].([]interface{})
				for _, relItem := range rels {
					relMap, ok := relItem.(map[string]interface{})
					if !ok {
						continue
					}
					targetName := strings.TrimSpace(fmt.Sprint(relMap["name"]))
					relText := strings.TrimSpace(fmt.Sprint(relMap["description"]))
					targetID := nameToID[targetName]
					if targetID == "" || targetID == sourceID {
						continue
					}
					charA, charB := sourceID, targetID
					if charA > charB {
						charA, charB = charB, charA
					}
					key := charA + ":" + charB
					firstChapter := 1
					if sourceFirst, ok := firstAppearanceByName[sourceName]; ok && sourceFirst > 0 {
						firstChapter = sourceFirst
					}
					if targetFirst, ok := firstAppearanceByName[targetName]; ok && targetFirst > 0 && targetFirst < firstChapter {
						firstChapter = targetFirst
					}
					seed := relationSeeds[key]
					if seed.charA == "" {
						seed = edgeSeed{charA: charA, charB: charB, rel: relText, first: &firstChapter}
					} else {
						if relText != "" && !strings.Contains(seed.rel, relText) {
							if seed.rel == "" {
								seed.rel = relText
							} else {
								seed.rel = seed.rel + " / " + relText
							}
						}
						if seed.first == nil || firstChapter < *seed.first {
							seed.first = &firstChapter
						}
					}
					relationSeeds[key] = seed
				}
			}

			if len(relationSeeds) > 0 {
				b := &pgx.Batch{}
				emptyInfo := json.RawMessage(`[]`)
				for _, seed := range relationSeeds {
					b.Queue(
						`INSERT INTO character_interactions
						    (project_id, char_a_id, char_b_id, first_meet_chapter, relationship, info_known_by_a, info_known_by_b, notes)
						 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
						 ON CONFLICT (project_id, char_a_id, char_b_id) DO UPDATE SET
						   first_meet_chapter = COALESCE(LEAST(character_interactions.first_meet_chapter, EXCLUDED.first_meet_chapter), character_interactions.first_meet_chapter, EXCLUDED.first_meet_chapter),
						   relationship = CASE
						       WHEN character_interactions.relationship = '' THEN EXCLUDED.relationship
						       WHEN EXCLUDED.relationship = '' OR position(EXCLUDED.relationship in character_interactions.relationship) > 0 THEN character_interactions.relationship
						       ELSE character_interactions.relationship || ' / ' || EXCLUDED.relationship
						   END,
						   notes = CASE WHEN character_interactions.notes = '' THEN EXCLUDED.notes ELSE character_interactions.notes END,
						   updated_at = NOW()`,
						projectID, seed.charA, seed.charB, seed.first, seed.rel, emptyInfo, emptyInfo, "reference_analysis")
				}
				br := s.db.SendBatch(ctx, b)
				for i := 0; i < b.Len(); i++ {
					if _, dbErr := br.Exec(); dbErr != nil {
						s.logger.Warn("import character interaction batch failed", zap.Int("idx", i), zap.Error(dbErr))
					}
				}
				if closeErr := br.Close(); closeErr != nil {
					s.logger.Warn("character interaction batch close failed", zap.Error(closeErr))
				}
			}
		}
	}

	if len(job.ExtractedWorld) > 2 {
		var worldData map[string]interface{}
		if json.Unmarshal(job.ExtractedWorld, &worldData) == nil {
			setting, _ := worldData["setting"].(string)
			timePeriod, _ := worldData["time_period"].(string)
			socialStructure, _ := worldData["social_structure"].(string)
			coreConflict, _ := worldData["core_conflict"].(string)
			locStrs := toStringSlice(worldData["locations"])
			sysStrs := toStringSlice(worldData["systems"])
			factionStrs := toStringSlice(worldData["factions"])
			forbiddenAnchors := toStringSlice(worldData["forbidden_anchors"])

			mappedWorld := map[string]interface{}{
				"world_view":       setting,
				"era_background":   timePeriod,
				"geography":        strings.Join(locStrs, "、"),
				"social_structure": socialStructure,
				"power_system":     strings.Join(sysStrs, "、"),
				"core_conflict":    coreConflict,
				"factions":         factionStrs,
				"source_ref_id":    job.RefID,
				"imported_from":    "reference_analysis",
			}
			worldJSON, _ := json.Marshal(mappedWorld)
			tag, dbErr := s.db.Exec(ctx,
				`UPDATE world_bibles SET content = COALESCE(content, '{}') || $1::jsonb, version = version + 1
				 WHERE project_id = $2`,
				worldJSON, projectID)
			if dbErr != nil {
				s.logger.Warn("world bible update failed", zap.String("project_id", projectID), zap.Error(dbErr))
			} else if tag.RowsAffected() == 0 {
				if _, dbErr2 := s.db.Exec(ctx,
					`INSERT INTO world_bibles (project_id, content, version)
					 VALUES ($1, $2, 1) ON CONFLICT (project_id) DO NOTHING`,
					projectID, worldJSON); dbErr2 != nil {
					s.logger.Warn("world bible insert failed", zap.String("project_id", projectID), zap.Error(dbErr2))
				}
			}

			immutableRules, mutableRules := parseConstitutions(worldData["constitutions"])
			forbiddenJSON, _ := json.Marshal(forbiddenAnchors)
			if len(immutableRules) > 2 || len(mutableRules) > 2 || len(forbiddenAnchors) > 0 {
				mergedImmutable := immutableRules
				mergedMutable := mutableRules
				mergedForbidden := forbiddenJSON
				if existingConst, getErr := s.worldBible.GetConstitution(ctx, projectID); getErr != nil {
					s.logger.Warn("load existing world constitution failed", zap.String("project_id", projectID), zap.Error(getErr))
				} else if existingConst != nil {
					mergedImmutable = mergeConstitutionRuleJSON(existingConst.ImmutableRules, immutableRules)
					mergedMutable = mergeConstitutionRuleJSON(existingConst.MutableRules, mutableRules)
					mergedForbidden = mergeStringListJSON(existingConst.ForbiddenAnchors, forbiddenJSON)
				}
				if _, constErr := s.worldBible.UpdateConstitution(ctx, projectID, mergedImmutable, mergedMutable, mergedForbidden); constErr != nil {
					s.logger.Warn("world constitution import failed", zap.String("project_id", projectID), zap.Error(constErr))
				}
			}
		}
	}

	if len(outlineNodes) > 0 {
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
			contentData := map[string]interface{}{
				"content":             summary,
				"key_events":          summary,
				"involved_characters": toStringSlice(node["involved_characters"]),
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
			if closeErr := br.Close(); closeErr != nil {
				s.logger.Warn("outline batch close failed", zap.Error(closeErr))
			}
		}
	}

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
				if closeErr := br.Close(); closeErr != nil {
					s.logger.Warn("glossary batch close failed", zap.Error(closeErr))
				}
			}
		}
	}

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
				b.Queue(
					`INSERT INTO foreshadowings (project_id, content, embed_method, priority, tags, status)
					 VALUES ($1, $2, 'reference_import', $3, $4, 'planned')`,
					projectID, content, priority, toStringSlice(f["related_characters"]))
			}
			if b.Len() > 0 {
				br := s.db.SendBatch(ctx, b)
				for i := 0; i < b.Len(); i++ {
					if _, dbErr := br.Exec(); dbErr != nil {
						s.logger.Warn("import foreshadowing batch failed", zap.Int("idx", i), zap.Error(dbErr))
					}
				}
				if closeErr := br.Close(); closeErr != nil {
					s.logger.Warn("foreshadowing batch close failed", zap.Error(closeErr))
				}
			}
		}
	}

	if _, dbErr := s.db.Exec(ctx,
		`UPDATE reference_materials SET status = 'completed' WHERE id = $1`,
		job.RefID); dbErr != nil {
		s.logger.Warn("could not mark reference as completed after import",
			zap.String("ref_id", job.RefID), zap.Error(dbErr))
	}

	return nil
}

// ── TaskQueue handler (runs as goroutine) ─────────────────────────────────────
