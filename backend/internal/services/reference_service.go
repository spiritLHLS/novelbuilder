package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

func validatePublicHTTPURL(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http and https URLs are allowed")
	}
	hostname := u.Hostname()
	ips, err := net.LookupHost(hostname)
	if err != nil {
		return fmt.Errorf("cannot resolve host %q: %w", hostname, err)
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if !isPublicIP(ip) {
			return fmt.Errorf("URL resolves to a private/reserved address and is not allowed")
		}
	}
	return nil
}

// isPublicIP returns true only for globally routable unicast addresses.
func isPublicIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
		"169.254.0.0/16",
		"100.64.0.0/10",
		"192.0.0.0/24",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"240.0.0.0/4",
		"0.0.0.0/8",
	}
	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

// isTextContentType returns true for text-based MIME types.
func isTextContentType(ct string) bool {
	ct = strings.ToLower(ct)
	allowedPrefixes := []string{"text/", "application/json", "application/xml", "application/atom", "application/rss"}
	for _, prefix := range allowedPrefixes {
		if strings.Contains(ct, prefix) {
			return true
		}
	}
	return false
}

// ============================================================
// Reference Material Service
// ============================================================

type ReferenceService struct {
	db         *pgxpool.Pool
	sidecarURL string
	rag        *RAGService
	logger     *zap.Logger
}

func NewReferenceService(db *pgxpool.Pool, sidecarURL string, rag *RAGService, logger *zap.Logger) *ReferenceService {
	return &ReferenceService{db: db, sidecarURL: sidecarURL, rag: rag, logger: logger}
}

func (s *ReferenceService) Create(ctx context.Context, projectID, title, author, genre, filePath, sourceURL string) (*models.ReferenceMaterial, error) {
	var ref models.ReferenceMaterial
	err := s.db.QueryRow(ctx,
		`INSERT INTO reference_materials (project_id, title, author, genre, file_path, source_url, status)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending')
		 RETURNING id, project_id, title, author, genre, file_path, COALESCE(source_url,''), status, created_at`,
		projectID, title, author, genre, filePath, sourceURL).Scan(
		&ref.ID, &ref.ProjectID, &ref.Title, &ref.Author, &ref.Genre, &ref.FilePath,
		&ref.SourceURL, &ref.Status, &ref.CreatedAt)
	return &ref, err
}

func (s *ReferenceService) Get(ctx context.Context, id string) (*models.ReferenceMaterial, error) {
	var ref models.ReferenceMaterial
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, title, author, genre, COALESCE(file_path, ''),
		        COALESCE(source_url, ''),
		        COALESCE(style_layer, '{}'), COALESCE(narrative_layer, '{}'), COALESCE(atmosphere_layer, '{}'),
		        COALESCE(migration_config, '{}'), COALESCE(style_collection, ''), status, created_at,
		        sample_texts,
		        COALESCE(fetch_status,'none'), COALESCE(fetch_done,0), COALESCE(fetch_total,0),
		        COALESCE(fetch_error,''), COALESCE(fetch_site,''), COALESCE(fetch_book_id,''),
		        COALESCE(fetch_chapter_ids,'[]'::jsonb)
		 FROM reference_materials WHERE id = $1`, id).Scan(
		&ref.ID, &ref.ProjectID, &ref.Title, &ref.Author, &ref.Genre, &ref.FilePath,
		&ref.SourceURL,
		&ref.StyleLayer, &ref.NarrativeLayer, &ref.AtmosphereLayer,
		&ref.MigrationConfig, &ref.StyleCollection, &ref.Status, &ref.CreatedAt,
		&ref.SampleTexts,
		&ref.FetchStatus, &ref.FetchDone, &ref.FetchTotal,
		&ref.FetchError, &ref.FetchSite, &ref.FetchBookID, &ref.FetchChapterIDs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &ref, nil
}

func (s *ReferenceService) List(ctx context.Context, projectID string) ([]models.ReferenceMaterial, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, title, author, genre, COALESCE(file_path, ''),
		        COALESCE(source_url, ''),
		        COALESCE(style_layer, '{}'), COALESCE(narrative_layer, '{}'), COALESCE(atmosphere_layer, '{}'),
		        COALESCE(migration_config, '{}'), COALESCE(style_collection, ''), status, created_at,
		        sample_texts,
		        COALESCE(fetch_status,'none'), COALESCE(fetch_done,0), COALESCE(fetch_total,0),
		        COALESCE(fetch_error,''), COALESCE(fetch_site,''), COALESCE(fetch_book_id,''),
		        COALESCE(fetch_chapter_ids,'[]'::jsonb)
		 FROM reference_materials WHERE project_id = $1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []models.ReferenceMaterial
	for rows.Next() {
		var ref models.ReferenceMaterial
		if err := rows.Scan(&ref.ID, &ref.ProjectID, &ref.Title, &ref.Author, &ref.Genre, &ref.FilePath,
			&ref.SourceURL,
			&ref.StyleLayer, &ref.NarrativeLayer, &ref.AtmosphereLayer,
			&ref.MigrationConfig, &ref.StyleCollection, &ref.Status, &ref.CreatedAt,
			&ref.SampleTexts,
			&ref.FetchStatus, &ref.FetchDone, &ref.FetchTotal,
			&ref.FetchError, &ref.FetchSite, &ref.FetchBookID, &ref.FetchChapterIDs); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *ReferenceService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM reference_materials WHERE id = $1`, id)
	return err
}

// CreateFromURL downloads the content at rawURL, saves it to the upload directory, and
// creates a reference_materials record. It enforces SSRF-safe URL validation.
func (s *ReferenceService) CreateFromURL(ctx context.Context, projectID, rawURL, title, author, genre string) (*models.ReferenceMaterial, error) {
	if err := validatePublicHTTPURL(rawURL); err != nil {
		return nil, err
	}

	const maxBytes = 20 * 1024 * 1024 // 20 MB
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "NovelBuilder/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("remote server returned HTTP %d", resp.StatusCode)
	}

	// Enforce content-type: must be text-based
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !isTextContentType(ct) {
		return nil, fmt.Errorf("unsupported content type %q; only text types are allowed", ct)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxBytes {
		return nil, fmt.Errorf("remote content exceeds 20 MB limit")
	}

	uploadDir := "/data/uploads"
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}

	ext := ".txt"
	if idx := strings.LastIndex(rawURL, "."); idx >= 0 {
		candidate := strings.ToLower(rawURL[idx:])
		switch candidate {
		case ".txt", ".md", ".html", ".htm":
			ext = candidate
		}
	}

	fileName := uuid.New().String() + ext
	filePath := filepath.Join(uploadDir, fileName)
	if err := os.WriteFile(filePath, body, 0o644); err != nil {
		return nil, fmt.Errorf("save file: %w", err)
	}

	return s.Create(ctx, projectID, title, author, genre, filePath, rawURL)
}

func (s *ReferenceService) UpdateMigrationConfig(ctx context.Context, id string, config json.RawMessage) error {
	_, err := s.db.Exec(ctx,
		`UPDATE reference_materials SET migration_config = $1 WHERE id = $2`,
		config, id)
	return err
}

func (s *ReferenceService) UpdateAnalysis(ctx context.Context, id string, style, narrative, atmosphere json.RawMessage) error {
	_, err := s.db.Exec(ctx,
		`UPDATE reference_materials SET style_layer = $1, narrative_layer = $2, atmosphere_layer = $3, status = 'completed'
		 WHERE id = $4`,
		style, narrative, atmosphere, id)
	return err
}

// IngestSamples vectorises style and sensory text samples extracted from a reference material
// and stores them in the vector_store. It first clears any existing vectors for that source,
// then caches the raw sample texts in reference_materials.sample_texts for future rebuilds.
func (s *ReferenceService) IngestSamples(ctx context.Context, projectID, refID string, styleSamples, sensorySamples []string) error {
	if s.rag == nil {
		return nil
	}

	// Persist sample texts so rebuild doesn't require re-reading files
	type sampleCache struct {
		Style   []string `json:"style"`
		Sensory []string `json:"sensory"`
	}
	cacheJSON, _ := json.Marshal(sampleCache{Style: styleSamples, Sensory: sensorySamples})
	s.db.Exec(ctx, `UPDATE reference_materials SET sample_texts = $1 WHERE id = $2`, cacheJSON, refID)

	// Delete old vectors for this reference before re-inserting
	if err := s.rag.DeleteBySourceID(ctx, projectID, refID); err != nil {
		s.logger.Warn("failed to clear old vectors for reference", zap.String("ref_id", refID), zap.Error(err))
	}

	meta := map[string]interface{}{"ref_id": refID}

	// Build batch items for all samples (one DB round-trip + bounded concurrent embedding calls)
	items := make([]BatchEmbedItem, 0, len(styleSamples)+len(sensorySamples))
	for _, sample := range styleSamples {
		items = append(items, BatchEmbedItem{
			Collection: "style_samples",
			Content:    sample,
			SourceType: "reference",
			SourceID:   refID,
			Metadata:   meta,
		})
	}
	for _, sample := range sensorySamples {
		items = append(items, BatchEmbedItem{
			Collection: "sensory_samples",
			Content:    sample,
			SourceType: "reference",
			SourceID:   refID,
			Metadata:   meta,
		})
	}
	return s.rag.StoreEmbeddingBatch(ctx, projectID, items)
}

// RebuildProject re-ingests all cached samples for every completed reference in a project.
// The number of style samples per reference scales with the project's chapter count so that
// each chapter has at least one style sample to draw from during generation.
// Returns the number of reference materials processed.
func (s *ReferenceService) RebuildProject(ctx context.Context, projectID string) (int, error) {
	if s.rag == nil {
		return 0, fmt.Errorf("RAG service not configured")
	}

	// Clear ALL vectors for the project so we start fresh
	if err := s.rag.DeleteForProject(ctx, projectID); err != nil {
		return 0, fmt.Errorf("clear project vectors: %w", err)
	}

	// Determine how many project chapters exist so we can scale the sample count.
	// Each chapter should have at least one representative style sample to look up,
	// with a floor of 20 and no hard ceiling (handles 1000+ chapter web novels).
	var projectChapterCount int
	if err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM chapters WHERE project_id = $1`, projectID,
	).Scan(&projectChapterCount); err != nil {
		projectChapterCount = 0
	}
	const minSamples = 20
	targetSamples := projectChapterCount
	if targetSamples < minSamples {
		targetSamples = minSamples
	}

	// Fetch all completed references.
	rows, err := s.db.Query(ctx,
		`SELECT id, sample_texts FROM reference_materials
		 WHERE project_id = $1 AND status = 'completed'`,
		projectID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type sampleCache struct {
		Style   []string `json:"style"`
		Sensory []string `json:"sensory"`
	}

	// Collect all batch items across all references.
	var allItems []BatchEmbedItem

	// For each reference: prefer reading from reference_book_chapters (always fresh,
	// properly sized). Fall back to cached sample_texts only when no chapters exist.
	type refEntry struct {
		id         string
		cachedJSON []byte
	}
	var refs []refEntry
	for rows.Next() {
		var refID string
		var samplesRaw []byte
		if err := rows.Scan(&refID, &samplesRaw); err != nil {
			continue
		}
		refs = append(refs, refEntry{id: refID, cachedJSON: samplesRaw})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rebuild project rows: %w", err)
	}

	rebuilt := 0
	const chapterBatchSize = 200
	for _, ref := range refs {
		meta := map[string]interface{}{"ref_id": ref.id}

		// ── Attempt 1: rebuild from reference_book_chapters ──────────────────
		var allText strings.Builder
		offset := 0
		for {
			chapRows, chErr := s.db.Query(ctx,
				`SELECT content FROM reference_book_chapters
				 WHERE ref_id = $1 AND NOT is_deleted AND content <> ''
				 ORDER BY chapter_no LIMIT $2 OFFSET $3`,
				ref.id, chapterBatchSize, offset)
			if chErr != nil {
				s.logger.Warn("could not read chapters for RAG rebuild",
					zap.String("ref_id", ref.id), zap.Error(chErr))
				break
			}
			rowCount := 0
			for chapRows.Next() {
				var content string
				if scanErr := chapRows.Scan(&content); scanErr == nil {
					allText.WriteString(content)
					allText.WriteByte('\n')
				}
				rowCount++
			}
			chapRows.Close()
			if rowCount < chapterBatchSize {
				break
			}
			offset += chapterBatchSize
		}

		if allText.Len() > 0 {
			// Scale sample count to project chapter count.
			sentences := sampleSentences(allText.String(), targetSamples)
			for _, sentence := range sentences {
				allItems = append(allItems, BatchEmbedItem{
					Collection: "style_samples",
					Content:    sentence,
					SourceType: "reference",
					SourceID:   ref.id,
					Metadata:   meta,
				})
			}
			rebuilt++
			continue
		}

		// ── Attempt 2: fall back to cached sample_texts ───────────────────────
		if len(ref.cachedJSON) > 2 {
			var cache sampleCache
			if err := json.Unmarshal(ref.cachedJSON, &cache); err == nil &&
				(len(cache.Style) > 0 || len(cache.Sensory) > 0) {
				for _, sample := range cache.Style {
					allItems = append(allItems, BatchEmbedItem{
						Collection: "style_samples",
						Content:    sample,
						SourceType: "reference",
						SourceID:   ref.id,
						Metadata:   meta,
					})
				}
				for _, sample := range cache.Sensory {
					allItems = append(allItems, BatchEmbedItem{
						Collection: "sensory_samples",
						Content:    sample,
						SourceType: "reference",
						SourceID:   ref.id,
						Metadata:   meta,
					})
				}
				rebuilt++
			}
		}
	}

	// ── world_knowledge: chunk world-bible content into paragraphs ─────────────
	wbRows, wbErr := s.db.Query(ctx,
		`SELECT id::text, content::text FROM world_bibles
		 WHERE project_id = $1
		 ORDER BY created_at`,
		projectID)
	if wbErr == nil {
		for wbRows.Next() {
			var wbID, content string
			if scanErr := wbRows.Scan(&wbID, &content); scanErr != nil || content == "" {
				continue
			}
			chunks := splitParagraphs(content, 300)
			for _, chunk := range chunks {
				allItems = append(allItems, BatchEmbedItem{
					Collection: "world_knowledge",
					Content:    chunk,
					SourceType: "world_bible",
					SourceID:   wbID,
					Metadata:   map[string]interface{}{"project_id": projectID},
				})
			}
		}
		wbRows.Close()
	} else {
		s.logger.Warn("could not read world_bibles for RAG rebuild", zap.String("project_id", projectID), zap.Error(wbErr))
	}

	// ── character_voices: embed character profiles ───────────────────────────
	charRows, charErr := s.db.Query(ctx,
		`SELECT id::text, name, COALESCE(profile::text, '{}') FROM characters
		 WHERE project_id = $1
		 ORDER BY created_at`,
		projectID)
	if charErr == nil {
		for charRows.Next() {
			var charID, name, profile string
			if scanErr := charRows.Scan(&charID, &name, &profile); scanErr != nil {
				continue
			}
			text := name
			if profile != "" && profile != "null" {
				text = name + "\n" + profile
			}
			if len([]rune(text)) < 10 {
				continue
			}
			for _, chunk := range splitParagraphs(text, 300) {
				allItems = append(allItems, BatchEmbedItem{
					Collection: "character_voices",
					Content:    chunk,
					SourceType: "character",
					SourceID:   charID,
					Metadata:   map[string]interface{}{"project_id": projectID, "name": name},
				})
			}
		}
		charRows.Close()
	} else {
		s.logger.Warn("could not read characters for RAG rebuild", zap.String("project_id", projectID), zap.Error(charErr))
	}

	// ── chapter_summaries: embed AI-generated summaries from novel chapters ──
	sumRows, sumErr := s.db.Query(ctx,
		`SELECT id::text, chapter_num, summary FROM chapters
		 WHERE project_id = $1 AND summary IS NOT NULL AND summary <> ''
		 ORDER BY chapter_num`,
		projectID)
	if sumErr == nil {
		for sumRows.Next() {
			var chapID, summary string
			var chapNum int
			if scanErr := sumRows.Scan(&chapID, &chapNum, &summary); scanErr != nil || len([]rune(summary)) < 10 {
				continue
			}
			allItems = append(allItems, BatchEmbedItem{
				Collection: "chapter_summaries",
				Content:    summary,
				SourceType: "chapter",
				SourceID:   chapID,
				Metadata:   map[string]interface{}{"project_id": projectID, "chapter_number": chapNum},
			})
		}
		sumRows.Close()
	} else {
		s.logger.Warn("could not read chapter summaries for RAG rebuild", zap.String("project_id", projectID), zap.Error(sumErr))
	}

	if len(allItems) == 0 {
		return rebuilt, nil
	}
	if err := s.rag.StoreEmbeddingBatch(ctx, projectID, allItems); err != nil {
		return rebuilt, err
	}
	return rebuilt, nil
}

// splitParagraphs splits text into chunks of at most maxRunes runes, breaking on paragraph
// boundaries where possible. Used when chunking world-bible and character-profile content.
