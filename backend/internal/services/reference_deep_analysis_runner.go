package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/retry"
	"go.uber.org/zap"
)

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
		reason := "no content to analyze"
		if err != nil {
			reason = "no content to analyze: " + err.Error()
		}
		s.failJob(ctx, jobID, reason)
		if err != nil {
			return fmt.Errorf("no content: %w", err)
		}
		return fmt.Errorf("no content to analyze")
	}

	// Compute dynamic chunk size based on the model's context window so we
	// maximize coverage per API call while avoiding context overflow.
	chunkSz := chunkSize // default fallback (80_000 chars)
	if llmCfg != nil {
		if model, ok := llmCfg["model"].(string); ok && model != "" {
			// Resolve effective output budget:
			// 1. Start from the model's hard maximum (e.g. 64K for deepseek-reasoner).
			// 2. If the profile has a configured max_tokens, use whichever is larger
			//    so that reasoning models always get their full output capacity.
			maxOut := modelMaxOutputTokens(model)
			if mt, ok := llmCfg["max_tokens"].(int); ok && mt > maxOut {
				maxOut = mt
			}
			chunkSz = computeChunkChars(model, maxOut)
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
		if err := json.Unmarshal(existingResultsRaw, &chunkResults); err != nil {
			s.logger.Warn("runAnalysisTask: failed to load chunk_results checkpoint",
				zap.String("job_id", jobID), zap.Error(err))
		}
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

		result, err := s.analyzeChunk(ctx, jobID, projectID, chunk, i, totalChunks, llmCfg, buildPriorContext(chunkResults))
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
	priorContext map[string]interface{},
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
	if len(priorContext) > 0 {
		body["prior_context"] = priorContext
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

// buildPriorContext extracts a compact list of entity names already found in
// completed chunks.  ALL completed chunks are scanned so that major recurring
// characters (e.g. a protagonist who appears in every chapter) are always in
// the hint and the LLM doesn't re-extract them indefinitely.
// Per-category caps and a total byte budget keep the hint size bounded.
//
// Only names are included (no descriptions) to minimise prompt bloat.
func buildPriorContext(completed []chunkResult) map[string]interface{} {
	const (
		maxChars   = 150  // max character entries — big enough for long novels
		maxLocs    = 50   // max location entries
		maxSystems = 20   // max system/power entries
		maxGloss   = 80   // max glossary term entries
		maxTotalB  = 6000 // hard byte budget for the whole prior context payload
	)
	if len(completed) == 0 {
		return nil
	}

	charSet := map[string]bool{}
	locSet := map[string]bool{}
	sysSet := map[string]bool{}
	glossSet := map[string]bool{}

	for _, r := range completed {
		if r == nil {
			continue
		}
		if chars, ok := r["characters"].([]interface{}); ok {
			for _, c := range chars {
				if cmap, ok := c.(map[string]interface{}); ok {
					name, _ := cmap["name"].(string)
					role, _ := cmap["role"].(string)
					if name != "" {
						entry := name
						if role != "" {
							entry = name + "(" + role + ")"
						}
						charSet[entry] = true
					}
				}
			}
		}
		if world, ok := r["world"].(map[string]interface{}); ok {
			if locs, ok := world["locations"].([]interface{}); ok {
				for _, l := range locs {
					if s, ok := l.(string); ok && s != "" {
						locSet[s] = true
					}
				}
			}
			if systems, ok := world["systems"].([]interface{}); ok {
				for _, sys := range systems {
					if s, ok := sys.(string); ok && s != "" {
						sysSet[s] = true
					}
				}
			}
		}
		if gloss, ok := r["glossary"].([]interface{}); ok {
			for _, g := range gloss {
				if gmap, ok := g.(map[string]interface{}); ok {
					term, _ := gmap["term"].(string)
					if term != "" {
						glossSet[term] = true
					}
				}
			}
		}
	}

	capSlice := func(set map[string]bool, limit int) []string {
		s := make([]string, 0, len(set))
		for k := range set {
			s = append(s, k)
		}
		sort.Strings(s)
		if len(s) > limit {
			s = s[:limit]
		}
		return s
	}

	ctx := map[string]interface{}{}
	if len(charSet) > 0 {
		ctx["characters"] = capSlice(charSet, maxChars)
	}
	if len(locSet) > 0 {
		ctx["locations"] = capSlice(locSet, maxLocs)
	}
	if len(sysSet) > 0 {
		ctx["systems"] = capSlice(sysSet, maxSystems)
	}
	if len(glossSet) > 0 {
		ctx["glossary"] = capSlice(glossSet, maxGloss)
	}
	if len(ctx) == 0 {
		return nil
	}

	// Final byte-budget guard: if the serialized payload is too large, drop
	// lower-priority categories until it fits.
	encoded, _ := json.Marshal(ctx)
	if len(encoded) > maxTotalB {
		delete(ctx, "glossary")
		encoded, _ = json.Marshal(ctx)
	}
	if len(encoded) > maxTotalB {
		delete(ctx, "locations")
	}
	return ctx
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
	case strings.Contains(m, "deepseek-reasoner"):
		return 128_000
	case strings.Contains(m, "deepseek-r1"):
		return 65_536
	case strings.Contains(m, "deepseek-chat"):
		return 128_000
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

// modelMaxOutputTokens returns the hard maximum output token limit for a model.
// Used to clamp the configured max_tokens so it never exceeds what the API allows,
// and to set a sensible floor when the user hasn't explicitly configured a value.
func modelMaxOutputTokens(m string) int {
	m = strings.ToLower(m)
	switch {
	case strings.Contains(m, "deepseek-reasoner"):
		return 64_000 // DeepSeek-Reasoner max output
	case strings.Contains(m, "deepseek-chat"):
		return 8_000 // DeepSeek-V3 max output
	case strings.Contains(m, "deepseek-r1"):
		return 32_000
	default:
		return 8_192
	}
}

// computeChunkChars returns the number of characters per analysis chunk, sized to fit
// inside the model's input window minus the output budget and prompt overhead.
// Assumes ~1.5 chars/token for Chinese prose (conservative).
func computeChunkChars(modelName string, maxOutputTokens int) int {
	ctxTokens := modelContextTokens(modelName)
	// promptOverhead covers: system message + JSON schema template (~500 tokens)
	// + prior_context hint block (~600 tokens worst case) + extraction rules (~300 tokens)
	promptOverhead := 1400
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
