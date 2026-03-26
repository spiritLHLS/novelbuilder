package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/crypto"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// AgentRoutingService manages the agent_model_routes table.
// It allows different agents to use different LLM profiles.
type AgentRoutingService struct {
	db            *pgxpool.Pool
	encryptionKey string
	logger        *zap.Logger
}

func NewAgentRoutingService(db *pgxpool.Pool, encryptionKey string, logger *zap.Logger) *AgentRoutingService {
	return &AgentRoutingService{db: db, encryptionKey: encryptionKey, logger: logger}
}

// List returns all agent routes for a given project (plus global routes if project routes are absent).
func (s *AgentRoutingService) List(ctx context.Context, projectID *string) ([]models.AgentModelRoute, error) {
	var rows []models.AgentModelRoute
	var query string
	var args []interface{}
	if projectID != nil && *projectID != "" {
		query = `SELECT r.id, r.agent_type, r.llm_profile_id, r.project_id,
		                p.name AS profile_name, p.provider AS profile_provider, p.model_name AS profile_model,
		                r.created_at, r.updated_at
		         FROM agent_model_routes r
		         LEFT JOIN llm_profiles p ON p.id = r.llm_profile_id
		         WHERE r.project_id = $1 OR r.project_id IS NULL
		         ORDER BY CASE WHEN r.project_id IS NOT NULL THEN 0 ELSE 1 END, r.agent_type`
		args = []interface{}{*projectID}
	} else {
		query = `SELECT r.id, r.agent_type, r.llm_profile_id, r.project_id,
		                p.name AS profile_name, p.provider AS profile_provider, p.model_name AS profile_model,
		                r.created_at, r.updated_at
		         FROM agent_model_routes r
		         LEFT JOIN llm_profiles p ON p.id = r.llm_profile_id
		         WHERE r.project_id IS NULL
		         ORDER BY r.agent_type`
	}
	dbRows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer dbRows.Close()
	for dbRows.Next() {
		var r models.AgentModelRoute
		if err := dbRows.Scan(
			&r.ID, &r.AgentType, &r.LLMProfileID, &r.ProjectID,
			&r.ProfileName, &r.ProfileProvider, &r.ProfileModel,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// Upsert inserts or updates an agent route record.
func (s *AgentRoutingService) Upsert(ctx context.Context, req models.UpsertAgentRouteRequest) (*models.AgentModelRoute, error) {
	now := time.Now()
	var r models.AgentModelRoute
	var err error
	if req.ProjectID != nil && *req.ProjectID != "" {
		// Project-level route: use partial index on (agent_type, project_id) WHERE project_id IS NOT NULL
		err = s.db.QueryRow(ctx,
			`INSERT INTO agent_model_routes (agent_type, llm_profile_id, project_id, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $4)
			 ON CONFLICT (agent_type, project_id) WHERE project_id IS NOT NULL
			 DO UPDATE SET llm_profile_id = EXCLUDED.llm_profile_id, updated_at = EXCLUDED.updated_at
			 RETURNING id, agent_type, llm_profile_id, project_id, created_at, updated_at`,
			req.AgentType, req.LLMProfileID, req.ProjectID, now,
		).Scan(&r.ID, &r.AgentType, &r.LLMProfileID, &r.ProjectID, &r.CreatedAt, &r.UpdatedAt)
	} else {
		// Global route: use partial index on (agent_type) WHERE project_id IS NULL
		err = s.db.QueryRow(ctx,
			`INSERT INTO agent_model_routes (agent_type, llm_profile_id, project_id, created_at, updated_at)
			 VALUES ($1, $2, NULL, $3, $3)
			 ON CONFLICT (agent_type) WHERE project_id IS NULL
			 DO UPDATE SET llm_profile_id = EXCLUDED.llm_profile_id, updated_at = EXCLUDED.updated_at
			 RETURNING id, agent_type, llm_profile_id, project_id, created_at, updated_at`,
			req.AgentType, req.LLMProfileID, now,
		).Scan(&r.ID, &r.AgentType, &r.LLMProfileID, &r.ProjectID, &r.CreatedAt, &r.UpdatedAt)
	}
	if err != nil {
		return nil, err
	}
	// Fetch profile metadata
	if r.LLMProfileID != nil {
		s.db.QueryRow(ctx, `SELECT name, provider, model_name FROM llm_profiles WHERE id = $1`, r.LLMProfileID).
			Scan(&r.ProfileName, &r.ProfileProvider, &r.ProfileModel)
	}
	return &r, nil
}

// Delete removes an agent route.
func (s *AgentRoutingService) Delete(ctx context.Context, agentType string, projectID *string) error {
	var q string
	var args []interface{}
	if projectID != nil && *projectID != "" {
		q = `DELETE FROM agent_model_routes WHERE agent_type = $1 AND project_id = $2`
		args = []interface{}{agentType, *projectID}
	} else {
		q = `DELETE FROM agent_model_routes WHERE agent_type = $1 AND project_id IS NULL`
		args = []interface{}{agentType}
	}
	_, err := s.db.Exec(ctx, q, args...)
	return err
}

// ResolveForAgent resolves the effective LLM config for a given agent + project,
// preferring project-level route over global route, then falling back to the global default profile.
func (s *AgentRoutingService) ResolveForAgent(ctx context.Context, agentType string, projectID string) (map[string]interface{}, error) {
	var profileID *string
	// Project-level first
	err := s.db.QueryRow(ctx,
		`SELECT llm_profile_id FROM agent_model_routes WHERE agent_type = $1 AND project_id = $2`,
		agentType, projectID,
	).Scan(&profileID)
	if err == pgx.ErrNoRows {
		// Try global
		err = s.db.QueryRow(ctx,
			`SELECT llm_profile_id FROM agent_model_routes WHERE agent_type = $1 AND project_id IS NULL`,
			agentType,
		).Scan(&profileID)
	}

	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	var apiKey, model, baseURL, provider *string
	var maxTokens, rpmLimit *int
	var omitMaxTokens, omitTemperature *bool
	var apiStyle *string
	if profileID != nil {
		s.db.QueryRow(ctx, `SELECT api_key, model_name, base_url, provider, max_tokens, rpm_limit, omit_max_tokens, omit_temperature, api_style FROM llm_profiles WHERE id = $1`, profileID).
			Scan(&apiKey, &model, &baseURL, &provider, &maxTokens, &rpmLimit, &omitMaxTokens, &omitTemperature, &apiStyle)
	}
	if apiKey == nil {
		// Fall back to default profile
		err2 := s.db.QueryRow(ctx,
			`SELECT api_key, model_name, base_url, provider, max_tokens, rpm_limit, omit_max_tokens, omit_temperature, api_style FROM llm_profiles WHERE is_default = TRUE LIMIT 1`,
		).Scan(&apiKey, &model, &baseURL, &provider, &maxTokens, &rpmLimit, &omitMaxTokens, &omitTemperature, &apiStyle)
		if err2 != nil {
			return nil, nil // No profile at all; caller handles
		}
	}

	cfg := map[string]interface{}{}
	if apiKey != nil {
		decrypted, err := crypto.Decrypt(*apiKey, s.encryptionKey)
		if err != nil {
			s.logger.Warn("ResolveForAgent: failed to decrypt api key", zap.Error(err))
		} else {
			cfg["api_key"] = decrypted
		}
	}
	if model != nil {
		cfg["model"] = *model
	}
	if baseURL != nil {
		cfg["base_url"] = *baseURL
	}
	if provider != nil {
		cfg["provider"] = *provider
	}
	if maxTokens != nil {
		cfg["max_tokens"] = *maxTokens
	}
	// For reasoning models (deepseek-reasoner, o1, o3 etc.) the default DB value of
	// 8192 is far below the model's actual maximum.  Override upward so callers
	// automatically get the full output capacity without requiring manual UI edits.
	if model != nil {
		m := strings.ToLower(*model)
		modelCap := 0
		switch {
		case strings.Contains(m, "deepseek-reasoner"):
			modelCap = 64_000
		case strings.Contains(m, "deepseek-r1"):
			modelCap = 32_000
		case strings.Contains(m, "o1"), strings.Contains(m, "o3"):
			modelCap = 100_000
		}
		if modelCap > 0 {
			current, _ := cfg["max_tokens"].(int)
			if current < modelCap {
				cfg["max_tokens"] = modelCap
			}
		}
	}
	if rpmLimit != nil {
		cfg["rpm_limit"] = *rpmLimit
	}
	if omitMaxTokens != nil {
		cfg["omit_max_tokens"] = *omitMaxTokens
	}
	if omitTemperature != nil {
		cfg["omit_temperature"] = *omitTemperature
	}
	if apiStyle != nil {
		cfg["api_style"] = *apiStyle
	}
	// json_mode: enable JSON-structured output for extraction tasks.
	// Disabled automatically for reasoning/thinking models that reject response_format.
	jsonMode := true
	if model != nil {
		m := strings.ToLower(*model)
		if strings.Contains(m, "reasoner") || strings.Contains(m, "o1") || strings.Contains(m, "o3") {
			jsonMode = false
		}
	}
	cfg["json_mode"] = jsonMode
	return cfg, nil
}

// ImportService manages chapter_imports: parse source text, call sidecar, insert chapters.
type ImportService struct {
	db         *pgxpool.Pool
	sidecarURL string
	httpClient *http.Client
	logger     *zap.Logger
}

func NewImportService(db *pgxpool.Pool, sidecarURL string, logger *zap.Logger) *ImportService {
	return &ImportService{
		db:         db,
		sidecarURL: sidecarURL,
		httpClient: &http.Client{Timeout: 300 * time.Second},
		logger:     logger,
	}
}

func (s *ImportService) Create(ctx context.Context, projectID string, req models.CreateImportRequest) (*models.ChapterImport, error) {
	pattern := req.SplitPattern
	if pattern == "" {
		pattern = `第.{1,4}章`
	}
	var imp models.ChapterImport
	now := time.Now()
	err := s.db.QueryRow(ctx,
		`INSERT INTO chapter_imports (project_id, source_text, split_pattern, fanfic_mode, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'pending', $5, $5)
		 RETURNING id, project_id, source_text, split_pattern, fanfic_mode, status,
		           total_chapters, processed_chapters, error_message, reverse_engineered, created_at, updated_at`,
		projectID, req.SourceText, pattern, req.FanficMode, now).
		Scan(&imp.ID, &imp.ProjectID, &imp.SourceText, &imp.SplitPattern, &imp.FanficMode,
			&imp.Status, &imp.TotalChapters, &imp.ProcessedChapters, &imp.ErrorMessage,
			&imp.ReverseEngineered, &imp.CreatedAt, &imp.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &imp, nil
}

func (s *ImportService) Get(ctx context.Context, importID string) (*models.ChapterImport, error) {
	var imp models.ChapterImport
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, source_text, split_pattern, fanfic_mode, status,
		        total_chapters, processed_chapters, error_message, reverse_engineered, created_at, updated_at
		 FROM chapter_imports WHERE id = $1`, importID).
		Scan(&imp.ID, &imp.ProjectID, &imp.SourceText, &imp.SplitPattern, &imp.FanficMode,
			&imp.Status, &imp.TotalChapters, &imp.ProcessedChapters, &imp.ErrorMessage,
			&imp.ReverseEngineered, &imp.CreatedAt, &imp.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &imp, nil
}

func (s *ImportService) List(ctx context.Context, projectID string) ([]models.ChapterImport, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, split_pattern, fanfic_mode, status,
		        total_chapters, processed_chapters, error_message, created_at, updated_at
		 FROM chapter_imports WHERE project_id = $1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var imports []models.ChapterImport
	for rows.Next() {
		var imp models.ChapterImport
		if err := rows.Scan(
			&imp.ID, &imp.ProjectID, &imp.SplitPattern, &imp.FanficMode, &imp.Status,
			&imp.TotalChapters, &imp.ProcessedChapters, &imp.ErrorMessage,
			&imp.CreatedAt, &imp.UpdatedAt,
		); err != nil {
			return nil, err
		}
		imports = append(imports, imp)
	}
	return imports, nil
}

// Process calls the sidecar, then bulk-inserts the resulting chapters.
func (s *ImportService) Process(ctx context.Context, importID string, llmCfg map[string]interface{}) error {
	imp, err := s.Get(ctx, importID)
	if err != nil {
		return fmt.Errorf("get import: %w", err)
	}

	// Mark as processing
	s.db.Exec(ctx,
		`UPDATE chapter_imports SET status = 'processing', updated_at = NOW() WHERE id = $1`, importID)

	sidecarBody := map[string]interface{}{
		"import_id":     imp.ID,
		"source_text":   imp.SourceText,
		"split_pattern": imp.SplitPattern,
		"project_id":    imp.ProjectID,
		"fanfic_mode":   imp.FanficMode,
		"llm_config":    llmCfg,
	}
	data, _ := json.Marshal(sidecarBody)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.sidecarURL+"/import-chapters/analyze", bytes.NewReader(data))
	if err != nil {
		s.markFailed(importID, err.Error())
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		s.markFailed(importID, err.Error())
		return fmt.Errorf("sidecar: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		msg := fmt.Sprintf("sidecar %d: %s", resp.StatusCode, string(raw))
		s.markFailed(importID, msg)
		return fmt.Errorf("%s", msg)
	}

	var result struct {
		Chapters          []map[string]interface{} `json:"chapters"`
		TotalChapters     int                      `json:"total_chapters"`
		ReverseEngineered json.RawMessage          `json:"reverse_engineered"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		s.markFailed(importID, "parse error: "+err.Error())
		return fmt.Errorf("parse sidecar result: %w", err)
	}

	// Bulk-insert chapters (no trigger context needed since we're just inserting raw content)
	if len(result.Chapters) > 0 {
		if err := s.bulkInsertChapters(ctx, imp.ProjectID, result.Chapters); err != nil {
			s.markFailed(importID, "insert chapters: "+err.Error())
			return fmt.Errorf("bulk insert: %w", err)
		}
	}

	reJSON := result.ReverseEngineered
	if reJSON == nil {
		reJSON = json.RawMessage(`{}`)
	}

	_, err = s.db.Exec(ctx,
		`UPDATE chapter_imports
		 SET status = 'completed', total_chapters = $2, processed_chapters = $2,
		     reverse_engineered = $3, updated_at = NOW()
		 WHERE id = $1`,
		importID, result.TotalChapters, reJSON,
	)
	if err != nil {
		s.logger.Error("update import status", zap.Error(err))
	}

	// Ingest reverse-engineered entities (characters, world notes, foreshadowings, etc.)
	// into the project's entity tables so the writer agent can reference them immediately.
	if len(reJSON) > 2 { // not just "{}"
		if ingErr := s.ingestReverseEngineered(ctx, imp.ProjectID, reJSON); ingErr != nil {
			s.logger.Warn("ingestReverseEngineered partial failure", zap.Error(ingErr))
			// Non-fatal: chapters were already saved successfully.
		}
	}

	return nil
}

// ingestReverseEngineered parses the LLM-extracted entity JSON returned by the
// sidecar /import-chapters/analyze endpoint and batch-inserts any discovered
// characters, glossary terms, foreshadowings, and resources into the relevant
// project tables.  Duplicate handling uses ON CONFLICT DO NOTHING to stay safe.
func (s *ImportService) ingestReverseEngineered(ctx context.Context, projectID string, reJSON json.RawMessage) error {
	var re struct {
		Characters []struct {
			Name     string                 `json:"name"`
			RoleType string                 `json:"role_type"`
			Profile  map[string]interface{} `json:"profile"`
		} `json:"characters"`
		Foreshadowings []struct {
			Content     string `json:"content"`
			EmbedMethod string `json:"embed_method"`
			Priority    int    `json:"priority"`
		} `json:"foreshadowings"`
		Glossary []struct {
			Term       string `json:"term"`
			Definition string `json:"definition"`
			Category   string `json:"category"`
		} `json:"glossary"`
	}

	if err := json.Unmarshal(reJSON, &re); err != nil {
		return fmt.Errorf("parse reverse_engineered: %w", err)
	}

	now := time.Now()

	// Characters
	if len(re.Characters) > 0 {
		b := &pgx.Batch{}
		for _, ch := range re.Characters {
			roleType := ch.RoleType
			if roleType == "" {
				roleType = "supporting"
			}
			profileJSON, _ := json.Marshal(ch.Profile)
			if profileJSON == nil {
				profileJSON = []byte(`{}`)
			}
			b.Queue(
				`INSERT INTO characters (id, project_id, name, role_type, profile, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $6)
				 ON CONFLICT (project_id, name) DO NOTHING`,
				uuid.New().String(), projectID, ch.Name, roleType, profileJSON, now,
			)
		}
		br := s.db.SendBatch(ctx, b)
		for range re.Characters {
			if _, err := br.Exec(); err != nil {
				s.logger.Warn("insert re character", zap.Error(err))
			}
		}
		br.Close()
	}

	// Foreshadowings
	if len(re.Foreshadowings) > 0 {
		b := &pgx.Batch{}
		for _, fs := range re.Foreshadowings {
			method := fs.EmbedMethod
			if method == "" {
				method = "implicit"
			}
			priority := fs.Priority
			if priority == 0 {
				priority = 5
			}
			b.Queue(
				`INSERT INTO foreshadowings (id, project_id, content, embed_method, priority, status, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, 'planted', $6, $6)`,
				uuid.New().String(), projectID, fs.Content, method, priority, now,
			)
		}
		br := s.db.SendBatch(ctx, b)
		for range re.Foreshadowings {
			if _, err := br.Exec(); err != nil {
				s.logger.Warn("insert re foreshadowing", zap.Error(err))
			}
		}
		br.Close()
	}

	// Glossary
	if len(re.Glossary) > 0 {
		b := &pgx.Batch{}
		for _, g := range re.Glossary {
			cat := g.Category
			if cat == "" {
				cat = "general"
			}
			b.Queue(
				`INSERT INTO glossary_terms (id, project_id, term, definition, category, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $6)
				 ON CONFLICT (project_id, term) DO NOTHING`,
				uuid.New().String(), projectID, g.Term, g.Definition, cat, now,
			)
		}
		br := s.db.SendBatch(ctx, b)
		for range re.Glossary {
			if _, err := br.Exec(); err != nil {
				s.logger.Warn("insert re glossary", zap.Error(err))
			}
		}
		br.Close()
	}

	return nil
}

func (s *ImportService) bulkInsertChapters(ctx context.Context, projectID string, chapters []map[string]interface{}) error {
	// Run inside a transaction so SET LOCAL is scoped to this operation only.
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Bypass the guard_chapter_sequence trigger: imported chapters skip the
	// blueprint-approval / previous-chapter-approved invariants since the
	// workflow hasn't been run for imported content.
	if _, err := tx.Exec(ctx, "SET LOCAL app.bypass_sequence_guard = 'true'"); err != nil {
		return fmt.Errorf("set bypass: %w", err)
	}

	// Get current max chapter number (inside the transaction for consistency)
	var maxSeq int
	tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(chapter_num), 0) FROM chapters WHERE project_id = $1`, projectID).
		Scan(&maxSeq)

	// Build batch insert
	b := &pgx.Batch{}
	for i, ch := range chapters {
		title, _ := ch["title"].(string)
		content, _ := ch["content"].(string)
		if title == "" {
			title = fmt.Sprintf("第%d章", maxSeq+i+1)
		}
		b.Queue(
			`INSERT INTO chapters (project_id, title, content, chapter_num, word_count, status, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, 'draft', NOW(), NOW())
			 ON CONFLICT (project_id, chapter_num) DO NOTHING`,
			projectID, title, content, maxSeq+i+1, len([]rune(content)),
		)
	}
	br := tx.SendBatch(ctx, b)
	for i := 0; i < len(chapters); i++ {
		if _, err := br.Exec(); err != nil {
			s.logger.Warn("insert chapter batch row", zap.Error(err))
		}
	}
	br.Close()

	return tx.Commit(ctx)
}

func (s *ImportService) markFailed(importID, errMsg string) {
	s.db.Exec(context.Background(),
		`UPDATE chapter_imports SET status = 'failed', error_message = $2, updated_at = NOW() WHERE id = $1`,
		importID, errMsg)
}
