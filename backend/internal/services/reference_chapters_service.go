package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/models"
)

func splitParagraphs(text string, maxRunes int) []string {
	var chunks []string
	paragraphs := strings.Split(text, "\n")
	var buf strings.Builder
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if buf.Len() > 0 && len([]rune(buf.String()))+len([]rune(p))+1 > maxRunes {
			chunks = append(chunks, strings.TrimSpace(buf.String()))
			buf.Reset()
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(p)
	}
	if buf.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(buf.String()))
	}
	// If a single paragraph exceeds maxRunes, split it by runes directly.
	var result []string
	runes := []rune(text)
	for _, chunk := range chunks {
		cr := []rune(chunk)
		if len(cr) <= maxRunes {
			result = append(result, chunk)
			continue
		}
		for i := 0; i < len(cr); i += maxRunes {
			end := i + maxRunes
			if end > len(cr) {
				end = len(cr)
			}
			result = append(result, string(cr[i:end]))
		}
	}
	// Fallback: if original text produced no chunks
	if len(result) == 0 {
		for i := 0; i < len(runes); i += maxRunes {
			end := i + maxRunes
			if end > len(runes) {
				end = len(runes)
			}
			result = append(result, string(runes[i:end]))
		}
	}
	return result
}

// sampleSentences extracts up to maxSamples evenly-spaced sentences from text for RAG indexing.
func sampleSentences(text string, maxSamples int) []string {
	// Split on common Chinese/English sentence terminators
	var sentences []string
	start := 0
	for i, r := range text {
		if r == '。' || r == '！' || r == '？' || r == '\n' || r == '.' || r == '!' || r == '?' {
			chunk := strings.TrimSpace(text[start : i+len(string(r))])
			if len([]rune(chunk)) >= 15 && len([]rune(chunk)) <= 150 {
				sentences = append(sentences, chunk)
			}
			start = i + len(string(r))
		}
	}
	if len(sentences) <= maxSamples {
		return sentences
	}
	// Evenly sample
	step := len(sentences) / maxSamples
	result := make([]string, 0, maxSamples)
	for i := 0; i < len(sentences) && len(result) < maxSamples; i += step {
		result = append(result, sentences[i])
	}
	return result
}

// ── Download task management (migration 014) ─────────────────────────────────

// CreateDownloadTask inserts a reference_materials record immediately at the start of a download
// so the task is persisted even if the browser disconnects.
func (s *ReferenceService) CreateDownloadTask(ctx context.Context,
	projectID, title, author, genre, site, bookID string, chapterIDs []string,
) (*models.ReferenceMaterial, error) {
	chapterIDsJSON, _ := json.Marshal(chapterIDs)
	var ref models.ReferenceMaterial
	err := s.db.QueryRow(ctx,
		`INSERT INTO reference_materials
		   (project_id, title, author, genre, fetch_status, fetch_total, fetch_done,
		    fetch_site, fetch_book_id, fetch_chapter_ids, status)
		 VALUES ($1,$2,$3,$4,'downloading',$5,0,$6,$7,$8,'pending')
		 RETURNING id, project_id, title, author, genre,
		           COALESCE(file_path,''), COALESCE(source_url,''), status,
		           fetch_status, fetch_done, fetch_total,
		           COALESCE(fetch_error,''), COALESCE(fetch_site,''), COALESCE(fetch_book_id,''),
		           COALESCE(fetch_chapter_ids,'[]'::jsonb), created_at`,
		projectID, title, author, genre,
		len(chapterIDs), site, bookID, chapterIDsJSON,
	).Scan(
		&ref.ID, &ref.ProjectID, &ref.Title, &ref.Author, &ref.Genre,
		&ref.FilePath, &ref.SourceURL, &ref.Status,
		&ref.FetchStatus, &ref.FetchDone, &ref.FetchTotal,
		&ref.FetchError, &ref.FetchSite, &ref.FetchBookID,
		&ref.FetchChapterIDs, &ref.CreatedAt,
	)
	return &ref, err
}

// UpdateFetchProgress updates the download progress counters.
func (s *ReferenceService) UpdateFetchProgress(ctx context.Context, id string, done int) error {
	_, err := s.db.Exec(ctx,
		`UPDATE reference_materials SET fetch_done = $1 WHERE id = $2`,
		done, id)
	return err
}

// MarkFetchComplete marks the download as successfully finished and records the file path.
func (s *ReferenceService) MarkFetchComplete(ctx context.Context, id, filePath string, totalDownloaded int) error {
	_, err := s.db.Exec(ctx,
		`UPDATE reference_materials
		 SET fetch_status='completed', fetch_done=$2, file_path=$3
		 WHERE id = $1`,
		id, totalDownloaded, filePath)
	return err
}

// MarkFetchFailed marks the download as failed and stores the error message.
func (s *ReferenceService) MarkFetchFailed(ctx context.Context, id, errMsg string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE reference_materials SET fetch_status='failed', fetch_error=$2 WHERE id=$1`,
		id, errMsg)
	return err
}

// SetFetchStatus updates fetch_status to the given value (e.g. 'downloading').
func (s *ReferenceService) SetFetchStatus(ctx context.Context, id, status string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE reference_materials SET fetch_status=$2 WHERE id=$1`,
		id, status)
	return err
}

// SetStatus updates the main status field of a reference material (e.g. 'analyzing', 'completed', 'failed').
func (s *ReferenceService) SetStatus(ctx context.Context, id, status string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE reference_materials SET status=$2 WHERE id=$1`,
		id, status)
	return err
}

// SaveChapter inserts a single downloaded chapter into reference_book_chapters.
func (s *ReferenceService) SaveChapter(ctx context.Context, refID, chapterID, title, content string, chapterNo int) error {
	wordCount := len([]rune(content))
	_, err := s.db.Exec(ctx,
		`INSERT INTO reference_book_chapters (ref_id, chapter_no, chapter_id, title, content, word_count)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		refID, chapterNo, chapterID, title, content, wordCount)
	return err
}

// GetChaptersContent returns the full text content of all non-deleted chapters of a
// reference book, concatenated in chapter order. Used by AnalyzeReference when the
// reference has no file on disk (i.e. was imported via the fetch-download flow).
func (s *ReferenceService) GetChaptersContent(ctx context.Context, refID string) (string, error) {
	rows, err := s.db.Query(ctx,
		`SELECT title, content FROM reference_book_chapters
		 WHERE ref_id = $1 AND NOT is_deleted
		 ORDER BY chapter_no`, refID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var sb strings.Builder
	for rows.Next() {
		var title, content string
		if err := rows.Scan(&title, &content); err != nil {
			return "", err
		}
		sb.WriteString(title)
		sb.WriteRune('\n')
		sb.WriteString(content)
		sb.WriteString("\n\n")
	}
	return sb.String(), rows.Err()
}

// ListChapters returns non-deleted chapters of a reference book, ordered by chapter_no.
// Content is excluded to keep the payload small; use GetChapter for full content.
func (s *ReferenceService) ListChapters(ctx context.Context, refID string) ([]models.ReferenceChapter, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, ref_id, chapter_no, chapter_id, title, word_count, is_deleted, created_at
		 FROM reference_book_chapters
		 WHERE ref_id = $1 AND NOT is_deleted
		 ORDER BY chapter_no`, refID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chapters []models.ReferenceChapter
	for rows.Next() {
		var ch models.ReferenceChapter
		if err := rows.Scan(&ch.ID, &ch.RefID, &ch.ChapterNo, &ch.ChapterID,
			&ch.Title, &ch.WordCount, &ch.IsDeleted, &ch.CreatedAt); err != nil {
			return nil, err
		}
		chapters = append(chapters, ch)
	}
	return chapters, rows.Err()
}

// SoftDeleteChapter soft-deletes a chapter by ID.
func (s *ReferenceService) SoftDeleteChapter(ctx context.Context, chapterID string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE reference_book_chapters SET is_deleted=TRUE WHERE id=$1`,
		chapterID)
	return err
}

// BatchSoftDeleteChapters soft-deletes all specified chapter IDs that belong to refID.
func (s *ReferenceService) BatchSoftDeleteChapters(ctx context.Context, refID string, chapterIDs []string) error {
	if len(chapterIDs) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx,
		`UPDATE reference_book_chapters SET is_deleted=TRUE
		 WHERE ref_id=$1 AND id = ANY($2::uuid[])`,
		refID, chapterIDs)
	return err
}

// GetChapterFull returns a single chapter with full content.
func (s *ReferenceService) GetChapterFull(ctx context.Context, chapterID string) (*models.ReferenceChapter, error) {
	var ch models.ReferenceChapter
	err := s.db.QueryRow(ctx,
		`SELECT id, ref_id, chapter_no, chapter_id, title, content, word_count, is_deleted, created_at
		 FROM reference_book_chapters WHERE id=$1`, chapterID).
		Scan(&ch.ID, &ch.RefID, &ch.ChapterNo, &ch.ChapterID,
			&ch.Title, &ch.Content, &ch.WordCount, &ch.IsDeleted, &ch.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// ExportBundle builds the exportable JSON bundle for one or more references.
// All data is fetched in two queries (one for materials, one for chapters) to avoid N+1.
func (s *ReferenceService) ExportBundle(ctx context.Context, refIDs []string) (*models.ReferenceExportBundle, error) {
	bundle := &models.ReferenceExportBundle{
		Version:    1,
		ExportedAt: time.Now().UTC(),
	}
	if len(refIDs) == 0 {
		return bundle, nil
	}

	// ── 1. Fetch all reference materials in a single query ────────────────────
	matRows, err := s.db.Query(ctx,
		`SELECT id, project_id, title, author, genre, COALESCE(file_path, ''),
		        COALESCE(source_url, ''),
		        COALESCE(style_layer, '{}'), COALESCE(narrative_layer, '{}'), COALESCE(atmosphere_layer, '{}'),
		        COALESCE(migration_config, '{}'), COALESCE(style_collection, ''), status, created_at,
		        sample_texts,
		        COALESCE(fetch_status,'none'), COALESCE(fetch_done,0), COALESCE(fetch_total,0),
		        COALESCE(fetch_error,''), COALESCE(fetch_site,''), COALESCE(fetch_book_id,''),
		        COALESCE(fetch_chapter_ids,'[]'::jsonb)
		 FROM reference_materials WHERE id = ANY($1::uuid[])`, refIDs)
	if err != nil {
		return nil, err
	}
	defer matRows.Close()

	refMap := make(map[string]*models.ReferenceMaterial, len(refIDs))
	for matRows.Next() {
		var ref models.ReferenceMaterial
		if err := matRows.Scan(
			&ref.ID, &ref.ProjectID, &ref.Title, &ref.Author, &ref.Genre, &ref.FilePath,
			&ref.SourceURL,
			&ref.StyleLayer, &ref.NarrativeLayer, &ref.AtmosphereLayer,
			&ref.MigrationConfig, &ref.StyleCollection, &ref.Status, &ref.CreatedAt,
			&ref.SampleTexts,
			&ref.FetchStatus, &ref.FetchDone, &ref.FetchTotal,
			&ref.FetchError, &ref.FetchSite, &ref.FetchBookID, &ref.FetchChapterIDs,
		); err != nil {
			return nil, err
		}
		r := ref
		refMap[ref.ID] = &r
	}
	if err := matRows.Err(); err != nil {
		return nil, err
	}
	matRows.Close()

	// ── 2. Fetch all chapters for all refs in a single query ──────────────────
	chapRows, err := s.db.Query(ctx,
		`SELECT id, ref_id, chapter_no, chapter_id, title, content, word_count, is_deleted, created_at
		 FROM reference_book_chapters
		 WHERE ref_id = ANY($1::uuid[]) AND NOT is_deleted
		 ORDER BY ref_id, chapter_no`, refIDs)
	if err != nil {
		return nil, err
	}
	defer chapRows.Close()

	chapMap := make(map[string][]models.ReferenceChapter)
	for chapRows.Next() {
		var ch models.ReferenceChapter
		if err := chapRows.Scan(&ch.ID, &ch.RefID, &ch.ChapterNo, &ch.ChapterID,
			&ch.Title, &ch.Content, &ch.WordCount, &ch.IsDeleted, &ch.CreatedAt); err != nil {
			return nil, err
		}
		chapMap[ch.RefID] = append(chapMap[ch.RefID], ch)
	}
	if err := chapRows.Err(); err != nil {
		return nil, err
	}

	// ── 3. Assemble bundle preserving the requested order ─────────────────────
	// Fetch the latest completed analysis job for each reference in one query.
	analysisMap := make(map[string]*models.ReferenceAnalysisExport, len(refIDs))
	anaRows, err := s.db.Query(ctx,
		`SELECT DISTINCT ON (ref_id)
		        ref_id,
		        extracted_characters, extracted_world, extracted_outline,
		        extracted_glossary, extracted_foreshadowings
		 FROM reference_analysis_jobs
		 WHERE ref_id = ANY($1::uuid[]) AND status = 'completed'
		 ORDER BY ref_id, updated_at DESC`, refIDs)
	if err == nil {
		defer anaRows.Close()
		for anaRows.Next() {
			var refID string
			var ae models.ReferenceAnalysisExport
			if scanErr := anaRows.Scan(&refID,
				&ae.ExtractedCharacters, &ae.ExtractedWorld, &ae.ExtractedOutline,
				&ae.ExtractedGlossary, &ae.ExtractedForeshadowings,
			); scanErr == nil {
				cp := ae
				analysisMap[refID] = &cp
			}
		}
	}

	for _, id := range refIDs {
		ref, ok := refMap[id]
		if !ok {
			continue
		}
		chapters := chapMap[id]
		// If no DB chapters, fall back to reading the file (backward compatibility)
		if len(chapters) == 0 && ref.FilePath != "" {
			chapters = s.parseFileIntoChapters(ref.FilePath, id)
		}
		bundle.References = append(bundle.References, models.ReferenceExportItem{
			Material:     *ref,
			Chapters:     chapters,
			AnalysisData: analysisMap[id],
		})
	}
	return bundle, nil
}

// parseFileIntoChapters is a best-effort parser for the sidecar TXT format.
// It splits on lines that look like chapter headings (第X章...).
func (s *ReferenceService) parseFileIntoChapters(filePath, refID string) []models.ReferenceChapter {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	chapterHeading := regexp.MustCompile(`^第.{1,6}[章节回]`)

	var chapters []models.ReferenceChapter
	var currentTitle string
	var currentLines []string
	no := 0

	flush := func() {
		if no == 0 && currentTitle == "" {
			return
		}
		no++
		content := strings.TrimSpace(strings.Join(currentLines, "\n"))
		chapters = append(chapters, models.ReferenceChapter{
			RefID:     refID,
			ChapterNo: no,
			Title:     currentTitle,
			Content:   content,
			WordCount: len([]rune(content)),
		})
		currentLines = currentLines[:0]
	}

	for _, line := range lines {
		if chapterHeading.MatchString(line) {
			flush()
			currentTitle = strings.TrimSpace(line)
		} else {
			currentLines = append(currentLines, line)
		}
	}
	flush()
	return chapters
}

// ImportBundle imports an exported bundle into a target project.
// It creates new reference_materials records (new IDs) and inserts all chapters.
// Returns the list of newly created reference IDs.
func (s *ReferenceService) ImportBundle(ctx context.Context, projectID string, bundle *models.ReferenceExportBundle) ([]string, error) {
	var createdIDs []string
	for _, item := range bundle.References {
		m := item.Material
		// Create new reference record with fresh ID
		ref, err := s.Create(ctx, projectID, m.Title, m.Author, m.Genre, "", m.SourceURL)
		if err != nil {
			return createdIDs, fmt.Errorf("import reference %q: %w", m.Title, err)
		}
		// Copy analysis layers if present
		if len(m.StyleLayer) > 0 || len(m.NarrativeLayer) > 0 {
			s.UpdateAnalysis(ctx, ref.ID, m.StyleLayer, m.NarrativeLayer, m.AtmosphereLayer) //nolint
		}
		// Restore migration config and style collection if present
		if len(m.MigrationConfig) > 0 || m.StyleCollection != "" {
			s.db.Exec(ctx, //nolint
				`UPDATE reference_materials SET migration_config=$1, style_collection=$2 WHERE id=$3`,
				nullableJSON(m.MigrationConfig), m.StyleCollection, ref.ID)
		}
		// Insert chapters
		for _, ch := range item.Chapters {
			s.SaveChapter(ctx, ref.ID, ch.ChapterID, ch.Title, ch.Content, ch.ChapterNo) //nolint
		}
		// Mark fetch complete
		if len(item.Chapters) > 0 {
			s.db.Exec(ctx, //nolint
				`UPDATE reference_materials SET fetch_status='completed', fetch_done=$2, fetch_total=$2 WHERE id=$1`,
				ref.ID, len(item.Chapters))
		}
		// Restore deep-analysis results when present in the bundle
		if ad := item.AnalysisData; ad != nil {
			jobID := uuid.New().String()
			s.db.Exec(ctx, //nolint
				`INSERT INTO reference_analysis_jobs
				 (id, ref_id, project_id, status,
				  extracted_characters, extracted_world, extracted_outline,
				  extracted_glossary, extracted_foreshadowings)
				 VALUES ($1,$2,$3,'completed',$4,$5,$6,$7,$8)`,
				jobID, ref.ID, projectID,
				nullableJSON(ad.ExtractedCharacters),
				nullableJSON(ad.ExtractedWorld),
				nullableJSON(ad.ExtractedOutline),
				nullableJSON(ad.ExtractedGlossary),
				nullableJSON(ad.ExtractedForeshadowings),
			)
			s.db.Exec(ctx, //nolint
				`UPDATE reference_materials SET analysis_job_id=$1 WHERE id=$2`,
				jobID, ref.ID,
			)
		}
		createdIDs = append(createdIDs, ref.ID)
	}
	return createdIDs, nil
}

// ListDownloadingRefs returns all reference_materials currently in 'downloading' or 'failed' state.
func (s *ReferenceService) ListDownloadingRefs(ctx context.Context, projectID string) ([]models.ReferenceMaterial, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, title, author, genre,
		        COALESCE(file_path,''), COALESCE(source_url,''),
		        COALESCE(style_layer,'{}'), COALESCE(narrative_layer,'{}'), COALESCE(atmosphere_layer,'{}'),
		        COALESCE(migration_config,'{}'), COALESCE(style_collection,''), status, created_at,
		        sample_texts,
		        COALESCE(fetch_status,'none'), COALESCE(fetch_done,0), COALESCE(fetch_total,0),
		        COALESCE(fetch_error,''), COALESCE(fetch_site,''), COALESCE(fetch_book_id,''),
		        COALESCE(fetch_chapter_ids,'[]'::jsonb)
		 FROM reference_materials
		 WHERE project_id=$1 AND fetch_status IN ('downloading','failed')
		 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []models.ReferenceMaterial
	for rows.Next() {
		var ref models.ReferenceMaterial
		if err := rows.Scan(
			&ref.ID, &ref.ProjectID, &ref.Title, &ref.Author, &ref.Genre,
			&ref.FilePath, &ref.SourceURL,
			&ref.StyleLayer, &ref.NarrativeLayer, &ref.AtmosphereLayer,
			&ref.MigrationConfig, &ref.StyleCollection, &ref.Status, &ref.CreatedAt,
			&ref.SampleTexts,
			&ref.FetchStatus, &ref.FetchDone, &ref.FetchTotal,
			&ref.FetchError, &ref.FetchSite, &ref.FetchBookID, &ref.FetchChapterIDs,
		); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// nullableJSON returns nil when the raw message is empty or a JSON null,
// so it is stored as SQL NULL rather than an empty JSON object.
func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}
