package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// analyzeHTTPClient is used exclusively by AnalyzeReference to call the Python sidecar.
// A 120-second timeout accommodates large PDF/EPUB analysis while preventing hangs.
var analyzeHTTPClient = &http.Client{Timeout: 120 * time.Second}

// fetchImportHTTPClient is used for the long-lived SSE stream from the sidecar.
// No overall Timeout is set — large books can take many minutes.
// Only the dial and response-header phases are bounded.
var fetchImportHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	},
}

const maxReferenceUploadBytes int64 = 50 << 20

var allowedReferenceUploadExtensions = map[string]struct{}{
	".txt":      {},
	".text":     {},
	".md":       {},
	".markdown": {},
	".pdf":      {},
	".epub":     {},
}

func (h *Handler) ListReferences(c *gin.Context) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return
	}
	refs, err := h.references.List(c.Request.Context(), id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": refs})
}

func (h *Handler) UploadReference(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if _, ok := allowedReferenceUploadExtensions[ext]; !ok {
		c.JSON(400, gin.H{"error": "unsupported file type"})
		return
	}
	if header.Size > maxReferenceUploadBytes {
		c.JSON(413, gin.H{"error": "file is too large"})
		return
	}

	sniff := make([]byte, 512)
	n, readErr := file.Read(sniff)
	if readErr != nil && readErr != io.EOF {
		c.JSON(400, gin.H{"error": "failed to read file header"})
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		c.JSON(500, gin.H{"error": "failed to reset upload stream"})
		return
	}
	detectedType := http.DetectContentType(sniff[:n])
	if !isAllowedReferenceUploadContentType(ext, detectedType, sniff[:n]) {
		c.JSON(400, gin.H{"error": "unsupported file content type"})
		return
	}

	uploadDir := "/data/uploads"
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		c.JSON(500, gin.H{"error": "failed to create upload directory"})
		return
	}
	fileName := uuid.New().String() + ext
	filePath := filepath.Join(uploadDir, fileName)
	dst, err := os.Create(filePath)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to save file"})
		return
	}
	defer dst.Close()
	written, err := io.Copy(dst, io.LimitReader(file, maxReferenceUploadBytes+1))
	if err != nil {
		os.Remove(filePath) //nolint
		c.JSON(500, gin.H{"error": "failed to save file"})
		return
	}
	if written > maxReferenceUploadBytes {
		os.Remove(filePath) //nolint
		c.JSON(413, gin.H{"error": "file is too large"})
		return
	}

	title := strings.TrimSpace(c.PostForm("title"))
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	}
	author := strings.TrimSpace(c.PostForm("author"))
	genre := strings.TrimSpace(c.PostForm("genre"))

	ref, err := h.references.Create(c.Request.Context(), c.Param("id"), title, author, genre, filePath, "")
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": ref})
}

func isAllowedReferenceUploadContentType(ext, detected string, sample []byte) bool {
	if detected == "" {
		return true
	}
	if strings.HasPrefix(detected, "text/") {
		return ext == ".txt" || ext == ".text" || ext == ".md" || ext == ".markdown"
	}
	switch ext {
	case ".pdf":
		return detected == "application/pdf" || detected == "application/octet-stream"
	case ".epub":
		return detected == "application/epub+zip" || detected == "application/zip" || detected == "application/octet-stream"
	default:
		return detected == "application/octet-stream" && looksMostlyText(sample)
	}
}

func looksMostlyText(sample []byte) bool {
	if len(sample) == 0 {
		return true
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	control := 0
	for _, b := range sample {
		if b < 0x09 || (b > 0x0d && b < 0x20) {
			control++
		}
	}
	return control*10 < len(sample)
}

func (h *Handler) GetReference(c *gin.Context) {
	ref, err := h.references.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if ref == nil {
		c.JSON(404, gin.H{"error": "reference not found"})
		return
	}
	c.JSON(200, gin.H{"data": ref})
}

func (h *Handler) ImportReferenceFromURL(c *gin.Context) {
	var body struct {
		URL    string `json:"url" binding:"required"`
		Title  string `json:"title"`
		Author string `json:"author"`
		Genre  string `json:"genre"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ref, err := h.references.CreateFromURL(c.Request.Context(), c.Param("id"), body.URL, body.Title, body.Author, body.Genre)
	if err != nil {
		if containsStr(err.Error(), "private/reserved") || containsStr(err.Error(), "only http") || containsStr(err.Error(), "invalid URL") || containsStr(err.Error(), "unsupported content type") {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"data": ref})
}

func (h *Handler) DeleteReference(c *gin.Context) {
	if err := h.references.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(204, nil)
}

// ListReferenceNovelSites proxies the searchable site catalog from the Python sidecar.
func (h *Handler) ListReferenceNovelSites(c *gin.Context) {
	sidecarURL := h.sidecar.BaseURL()

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, sidecarURL+"/novels/sites", nil)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to build request"})
		return
	}

	resp, err := analyzeHTTPClient.Do(httpReq)
	if err != nil {
		c.JSON(502, gin.H{"error": "site catalog service unavailable: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", raw)
}

// SearchReferenceNovels proxies keyword search to the Python sidecar's /novels/search endpoint.
func (h *Handler) SearchReferenceNovels(c *gin.Context) {
	var body struct {
		Keyword      string   `json:"keyword" binding:"required"`
		Sites        []string `json:"sites"`
		Limit        int      `json:"limit"`          // 0 = unlimited
		PerSiteLimit int      `json:"per_site_limit"` // 0 = use sidecar default (10)
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// Do NOT cap to 20; 0 means "unlimited" on the sidecar side.
	if body.PerSiteLimit <= 0 {
		body.PerSiteLimit = 10
	}

	sidecarURL := h.sidecar.BaseURL()

	reqBody, _ := json.Marshal(map[string]interface{}{
		"keyword":        body.Keyword,
		"sites":          body.Sites,
		"limit":          body.Limit,
		"per_site_limit": body.PerSiteLimit,
	})
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, sidecarURL+"/novels/search", bytes.NewReader(reqBody))
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to build request"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := analyzeHTTPClient.Do(httpReq)
	if err != nil {
		c.JSON(502, gin.H{"error": "search service unavailable: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", raw)
}

// ResolveReferenceNovelURL resolves a pasted book URL into a site/book_id pair.
func (h *Handler) ResolveReferenceNovelURL(c *gin.Context) {
	var body struct {
		URL string `json:"url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	sidecarURL := h.sidecar.BaseURL()
	reqBody, _ := json.Marshal(map[string]string{"url": body.URL})
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, sidecarURL+"/novels/resolve-url", bytes.NewReader(reqBody))
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to build request"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := analyzeHTTPClient.Do(httpReq)
	if err != nil {
		c.JSON(502, gin.H{"error": "URL resolve service unavailable: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", raw)
}

// SearchReferenceNovelsStream proxies the streaming search endpoint from the
// Python sidecar (/novels/search-stream) and relays site-by-site NDJSON batches.
func (h *Handler) SearchReferenceNovelsStream(c *gin.Context) {
	var body struct {
		Keyword      string   `json:"keyword" binding:"required"`
		Sites        []string `json:"sites"`
		PerSiteLimit int      `json:"per_site_limit"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if body.PerSiteLimit <= 0 {
		body.PerSiteLimit = 10
	}

	sidecarURL := h.sidecar.BaseURL()

	reqBody, _ := json.Marshal(map[string]interface{}{
		"keyword":        body.Keyword,
		"sites":          body.Sites,
		"per_site_limit": body.PerSiteLimit,
	})

	// Use a long-lived HTTP client without a short deadline (same as fetch-import).
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, sidecarURL+"/novels/search-stream", bytes.NewReader(reqBody))
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to build sidecar request"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := fetchImportHTTPClient.Do(httpReq)
	if err != nil {
		c.JSON(502, gin.H{"error": "search stream service unavailable: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		c.JSON(502, gin.H{"error": string(errBody)})
		return
	}

	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")

	flusher, canFlush := c.Writer.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		fmt.Fprintf(c.Writer, "%s\n", line)
		if canFlush {
			flusher.Flush()
		}
	}
}

// GetReferenceBookInfo proxies to /novels/book-info on the Python sidecar.
func (h *Handler) GetReferenceBookInfo(c *gin.Context) {
	var body struct {
		Site   string `json:"site" binding:"required"`
		BookID string `json:"book_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	sidecarURL := h.sidecar.BaseURL()

	reqBody, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, sidecarURL+"/novels/book-info", bytes.NewReader(reqBody))
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to build request"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := analyzeHTTPClient.Do(httpReq)
	if err != nil {
		c.JSON(502, gin.H{"error": "book info service unavailable: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", raw)
}

// FetchImportReference creates a reference record immediately and starts a background
// goroutine to download chapters from the sidecar. The response returns instantly with
// the new ref_id so the frontend can poll progress via GET /references/:id.
func (h *Handler) FetchImportReference(c *gin.Context) {
	projectID := c.Param("id")
	var body struct {
		Site       string   `json:"site" binding:"required"`
		BookID     string   `json:"book_id" binding:"required"`
		Title      string   `json:"title"`
		Author     string   `json:"author"`
		Genre      string   `json:"genre"`
		ChapterIDs []string `json:"chapter_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if len(body.ChapterIDs) == 0 {
		c.JSON(400, gin.H{"error": "chapter_ids must not be empty"})
		return
	}

	// Create the DB record immediately so it survives browser disconnection.
	ref, err := h.references.CreateDownloadTask(c.Request.Context(),
		projectID, body.Title, body.Author, body.Genre,
		body.Site, body.BookID, body.ChapterIDs)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to create download task: " + err.Error()})
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"ref_id":      ref.ID,
		"site":        body.Site,
		"book_id":     body.BookID,
		"title":       body.Title,
		"author":      body.Author,
		"chapter_ids": body.ChapterIDs,
	})
	task, err := h.taskQueue.Enqueue(c.Request.Context(), models.CreateTaskRequest{
		ProjectID:   projectID,
		TaskType:    "reference_fetch_import",
		Payload:     payload,
		Priority:    6,
		MaxAttempts: 1,
	})
	if err != nil {
		h.logger.Error("failed to enqueue reference download", zap.String("ref_id", ref.ID), zap.Error(err))
		h.references.MarkFetchFailed(context.Background(), ref.ID, "failed to enqueue download task: "+err.Error()) //nolint
		c.JSON(500, gin.H{"error": "failed to enqueue download task: " + err.Error()})
		return
	}
	h.logger.Info("reference download task queued", zap.String("ref_id", ref.ID), zap.String("task_id", task.ID))
	c.JSON(202, gin.H{
		"ref_id":      ref.ID,
		"task_id":     task.ID,
		"status":      "downloading",
		"fetch_total": len(body.ChapterIDs),
	})
}

// RunReferenceDownload calls the sidecar SSE stream and stores each chapter in the DB.
func (h *Handler) RunReferenceDownload(ctx context.Context, refID, sidecarURL, site, bookID, title, author string, chapterIDs []string) error {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"site":        site,
		"book_id":     bookID,
		"title":       title,
		"author":      author,
		"chapter_ids": chapterIDs,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		sidecarURL+"/novels/fetch-import", bytes.NewReader(reqBody))
	if err != nil {
		h.logger.Error("runBackgroundDownload: build request", zap.String("ref_id", refID), zap.Error(err))
		h.references.MarkFetchFailed(ctx, refID, "failed to build sidecar request: "+err.Error()) //nolint
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := fetchImportHTTPClient.Do(httpReq)
	if err != nil {
		h.logger.Error("runBackgroundDownload: sidecar unavailable", zap.String("ref_id", refID), zap.Error(err))
		h.references.MarkFetchFailed(ctx, refID, "sidecar unavailable: "+err.Error()) //nolint
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		h.references.MarkFetchFailed(ctx, refID, string(errBody)) //nolint
		return fmt.Errorf("sidecar download failed: %s", string(errBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 8<<20), 8<<20) // 8 MB buffer — chapters can be large
	progressDone := 0
	totalChapters := len(chapterIDs)

	h.logger.Info("background download started",
		zap.String("ref_id", refID),
		zap.String("title", title),
		zap.Int("total_chapters", totalChapters),
	)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			h.logger.Warn("runBackgroundDownload: unparseable line", zap.String("ref_id", refID), zap.String("line", line))
			continue
		}
		evType, _ := event["type"].(string)
		switch evType {
		case "progress":
			if done, ok := event["done"].(float64); ok {
				progressDone = int(done)
				h.references.UpdateFetchProgress(ctx, refID, progressDone) //nolint
				chTitle, _ := event["chapter_title"].(string)
				h.logger.Info("download progress",
					zap.String("ref_id", refID),
					zap.Int("done", progressDone),
					zap.Int("total", totalChapters),
					zap.String("chapter_title", chTitle),
				)
			}
		case "chapter":
			chapterNo, _ := event["chapter_no"].(float64)
			chapterID, _ := event["chapter_id"].(string)
			chTitle, _ := event["title"].(string)
			content, _ := event["content"].(string)
			if err := h.references.SaveChapter(ctx, refID, chapterID, chTitle, content, int(chapterNo)); err != nil {
				h.logger.Warn("runBackgroundDownload: failed to save chapter",
					zap.String("ref_id", refID),
					zap.String("chapter_id", chapterID),
					zap.Error(err),
				)
			}
		case "log":
			// Informational/warning messages from the sidecar (e.g. retry attempts)
			msg, _ := event["message"].(string)
			level, _ := event["level"].(string)
			if level == "error" {
				h.logger.Error("sidecar-download", zap.String("ref_id", refID), zap.String("msg", msg))
			} else {
				h.logger.Warn("sidecar-download", zap.String("ref_id", refID), zap.String("msg", msg))
			}
		case "done":
			filePath, _ := event["file_path"].(string)
			totalDownloaded := progressDone
			if tc, ok := event["total_chapters"].(float64); ok {
				totalDownloaded = int(tc)
			}
			skipped := 0
			if sk, ok := event["skipped_chapters"].(float64); ok {
				skipped = int(sk)
			}
			h.references.MarkFetchComplete(ctx, refID, filePath, totalDownloaded) //nolint
			h.logger.Info("background download complete",
				zap.String("ref_id", refID),
				zap.String("title", title),
				zap.Int("downloaded", totalDownloaded),
				zap.Int("skipped", skipped),
			)
			return nil
		case "error":
			msg, _ := event["message"].(string)
			h.references.MarkFetchFailed(ctx, refID, msg) //nolint
			h.logger.Error("background download failed",
				zap.String("ref_id", refID),
				zap.String("title", title),
				zap.String("error", msg),
			)
			return fmt.Errorf("download stream error: %s", msg)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if err := scanner.Err(); err != nil {
		h.logger.Error("runBackgroundDownload: stream read error",
			zap.String("ref_id", refID),
			zap.Error(err),
		)
		h.references.MarkFetchFailed(ctx, refID, "stream read error: "+err.Error()) //nolint
		return err
	}

	h.logger.Error("runBackgroundDownload: stream ended without terminal event",
		zap.String("ref_id", refID),
		zap.String("title", title),
	)
	h.references.MarkFetchFailed(ctx, refID, "download stream ended unexpectedly before completion") //nolint
	return fmt.Errorf("download stream ended unexpectedly before completion")
}

// ResumeReferenceDownload restarts a failed or interrupted download for the remaining chapters.
func (h *Handler) ResumeReferenceDownload(c *gin.Context) {
	refID := c.Param("id")
	ref, err := h.references.Get(c.Request.Context(), refID)
	if err != nil || ref == nil {
		c.JSON(404, gin.H{"error": "reference not found"})
		return
	}
	if ref.FetchStatus == "completed" {
		c.JSON(400, gin.H{"error": "download already completed"})
		return
	}

	// Determine which chapter IDs have been saved already
	existing, _ := h.references.ListChapters(c.Request.Context(), refID)
	doneIDs := make(map[string]bool, len(existing))
	for _, ch := range existing {
		doneIDs[ch.ChapterID] = true
	}

	// Parse the full list from fetch_chapter_ids
	var allIDs []string
	if err := json.Unmarshal(ref.FetchChapterIDs, &allIDs); err != nil || len(allIDs) == 0 {
		c.JSON(400, gin.H{"error": "no chapter_ids recorded for this download; cannot resume"})
		return
	}

	var remaining []string
	for _, id := range allIDs {
		if !doneIDs[id] {
			remaining = append(remaining, id)
		}
	}
	if len(remaining) == 0 {
		// All chapters are already saved — just mark complete.
		h.references.MarkFetchComplete(c.Request.Context(), refID, ref.FilePath, len(existing)) //nolint
		c.JSON(200, gin.H{"ref_id": refID, "status": "completed", "message": "all chapters already downloaded"})
		return
	}

	// Reset status to downloading and update counter
	h.references.UpdateFetchProgress(c.Request.Context(), refID, len(existing)) //nolint
	h.references.SetFetchStatus(c.Request.Context(), refID, "downloading")      //nolint

	payload, _ := json.Marshal(map[string]interface{}{
		"ref_id":      refID,
		"site":        ref.FetchSite,
		"book_id":     ref.FetchBookID,
		"title":       ref.Title,
		"author":      ref.Author,
		"chapter_ids": remaining,
	})
	task, err := h.taskQueue.Enqueue(c.Request.Context(), models.CreateTaskRequest{
		ProjectID:   ref.ProjectID,
		TaskType:    "reference_fetch_import",
		Payload:     payload,
		Priority:    6,
		MaxAttempts: 1,
	})
	if err != nil {
		h.references.MarkFetchFailed(c.Request.Context(), refID, "failed to enqueue resume task: "+err.Error()) //nolint
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{
		"ref_id":    refID,
		"task_id":   task.ID,
		"status":    "downloading",
		"remaining": len(remaining),
	})
}

// ListReferenceChapters lists non-deleted chapters of a reference book (without content).
func (h *Handler) ListReferenceChapters(c *gin.Context) {
	refID := c.Param("id")
	chapters, err := h.references.ListChapters(c.Request.Context(), refID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if chapters == nil {
		chapters = []models.ReferenceChapter{}
	}
	c.JSON(200, gin.H{"data": chapters})
}

// DeleteReferenceChapter soft-deletes a single chapter.
func (h *Handler) DeleteReferenceChapter(c *gin.Context) {
	chapterID := c.Param("id")
	if err := h.references.SoftDeleteChapter(c.Request.Context(), chapterID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "deleted"})
}

// BatchDeleteReferenceChapters soft-deletes multiple chapters by ID.
func (h *Handler) BatchDeleteReferenceChapters(c *gin.Context) {
	refID := c.Param("id")
	var body struct {
		IDs []string `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := h.references.BatchSoftDeleteChapters(c.Request.Context(), refID, body.IDs); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "deleted", "count": len(body.IDs)})
}

// ExportReferenceSingle exports a single reference book as a JSON bundle download.
func (h *Handler) ExportReferenceSingle(c *gin.Context) {
	refID := c.Param("id")
	bundle, err := h.references.ExportBundle(c.Request.Context(), []string{refID})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if len(bundle.References) == 0 {
		c.JSON(404, gin.H{"error": "reference not found"})
		return
	}
	title := bundle.References[0].Material.Title
	if title == "" {
		title = refID
	}
	filename := fmt.Sprintf("ref_%s.json", strings.ReplaceAll(title, " ", "_"))
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.JSON(200, bundle)
}

// ExportReferenceBatch exports multiple references as a single JSON bundle.
func (h *Handler) ExportReferenceBatch(c *gin.Context) {
	var body struct {
		IDs []string `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	bundle, err := h.references.ExportBundle(c.Request.Context(), body.IDs)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="references_export.json"`)
	c.JSON(200, bundle)
}

// ImportReferenceLocal imports a JSON bundle previously exported from another instance.
func (h *Handler) ImportReferenceLocal(c *gin.Context) {
	projectID := c.Param("id")
	var bundle models.ReferenceExportBundle
	if err := c.ShouldBindJSON(&bundle); err != nil {
		c.JSON(400, gin.H{"error": "invalid bundle format: " + err.Error()})
		return
	}
	if bundle.Version != 1 {
		c.JSON(400, gin.H{"error": fmt.Sprintf("unsupported bundle version %d", bundle.Version)})
		return
	}
	if len(bundle.References) == 0 {
		c.JSON(400, gin.H{"error": "bundle contains no references"})
		return
	}
	createdIDs, err := h.references.ImportBundle(c.Request.Context(), projectID, &bundle)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"created_ids": createdIDs, "count": len(createdIDs)})
}

func (h *Handler) UpdateMigrationConfig(c *gin.Context) {
	var body json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := h.references.UpdateMigrationConfig(c.Request.Context(), c.Param("id"), body); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "updated"})
}

func (h *Handler) AnalyzeReference(c *gin.Context) {
	refID := c.Param("id")
	ref, err := h.references.Get(c.Request.Context(), refID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if ref == nil {
		c.JSON(404, gin.H{"error": "reference not found"})
		return
	}

	// If already in progress, return immediately so the frontend can keep polling.
	if ref.Status == "analyzing" {
		c.JSON(202, gin.H{"ref_id": refID, "status": "analyzing"})
		return
	}

	// Persist 'analyzing' status immediately so the frontend (and page refresh) can see it.
	h.references.SetStatus(c.Request.Context(), refID, "analyzing") //nolint

	payload, _ := json.Marshal(map[string]string{"ref_id": refID})
	task, err := h.taskQueue.Enqueue(c.Request.Context(), models.CreateTaskRequest{
		ProjectID:   ref.ProjectID,
		TaskType:    "reference_analyze",
		Payload:     payload,
		Priority:    5,
		MaxAttempts: 1,
	})
	if err != nil {
		h.references.SetStatus(c.Request.Context(), refID, "failed") //nolint
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"ref_id": refID, "task_id": task.ID, "status": "analyzing"})
}

func (h *Handler) RunReferenceAnalyze(ctx context.Context, refID string) error {
	ref, err := h.references.Get(ctx, refID)
	if err != nil || ref == nil {
		return fmt.Errorf("reference not found")
	}
	sidecarURL := h.sidecar.BaseURL()
	analysisFilePath := ref.FilePath
	var tempFilePath string
	if analysisFilePath == "" {
		text, err := h.references.GetChaptersContent(ctx, refID)
		if err != nil || text == "" {
			h.references.SetStatus(ctx, refID, "failed") //nolint
			return fmt.Errorf("no content to analyze: reference has no file and no downloaded chapters")
		}
		uploadDir := "/data/uploads"
		if mkErr := os.MkdirAll(uploadDir, 0o755); mkErr != nil {
			h.references.SetStatus(ctx, refID, "failed") //nolint
			return fmt.Errorf("failed to create upload directory: %w", mkErr)
		}
		tmpPath := filepath.Join(uploadDir, "analyze_"+refID+".txt")
		if writeErr := os.WriteFile(tmpPath, []byte(text), 0o644); writeErr != nil {
			h.references.SetStatus(ctx, refID, "failed") //nolint
			return fmt.Errorf("failed to prepare analysis file: %w", writeErr)
		}
		analysisFilePath = tmpPath
		tempFilePath = tmpPath
	}
	return h.runReferenceAnalyze(ctx, refID, ref.ProjectID, analysisFilePath, tempFilePath, sidecarURL)
}

// runReferenceAnalyze performs the actual sidecar call and DB update.
func (h *Handler) runReferenceAnalyze(ctx context.Context, refID, projectID, analysisFilePath, tempFilePath, sidecarURL string) error {
	if tempFilePath != "" {
		defer os.Remove(tempFilePath) //nolint
	}

	reqBody, _ := json.Marshal(map[string]string{
		"file_path":   analysisFilePath,
		"material_id": refID,
		"project_id":  projectID,
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, sidecarURL+"/analyze", bytes.NewReader(reqBody))
	if err != nil {
		h.logger.Error("runBackgroundAnalyze: build request failed", zap.String("ref_id", refID), zap.Error(err))
		h.references.SetStatus(ctx, refID, "failed") //nolint
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := analyzeHTTPClient.Do(httpReq)
	if resp != nil {
		defer resp.Body.Close()
	}

	if err != nil || resp == nil || resp.StatusCode != 200 {
		h.logger.Warn("runBackgroundAnalyze: Python sidecar unavailable, using AI fallback",
			zap.String("ref_id", refID), zap.Error(err))
		styleJSON := json.RawMessage(`{"nl_description": "默认风格分析（Python分析服务不可用）"}`)
		narrativeJSON := json.RawMessage(`{"pov_type": "限制性第三人称"}`)
		atmosphereJSON := json.RawMessage(`{"tone_descriptions": ["待分析"]}`)
		h.references.UpdateAnalysis(ctx, refID, styleJSON, narrativeJSON, atmosphereJSON) //nolint
		return nil
	}

	var analysisResult struct {
		StyleLayer      json.RawMessage `json:"style_layer"`
		NarrativeLayer  json.RawMessage `json:"narrative_layer"`
		AtmosphereLayer json.RawMessage `json:"atmosphere_layer"`
		StyleSamples    []string        `json:"style_samples"`
		SensorySamples  []string        `json:"sensory_samples"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&analysisResult); err != nil {
		h.logger.Error("runBackgroundAnalyze: decode failed", zap.String("ref_id", refID), zap.Error(err))
		h.references.SetStatus(ctx, refID, "failed") //nolint
		return err
	}

	h.references.UpdateAnalysis(ctx, refID,
		analysisResult.StyleLayer, analysisResult.NarrativeLayer, analysisResult.AtmosphereLayer) //nolint

	ingestCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if ingestErr := h.references.IngestSamples(
		ingestCtx, projectID, refID,
		analysisResult.StyleSamples, analysisResult.SensorySamples,
	); ingestErr != nil {
		h.logger.Warn("RAG ingest failed", zap.String("ref_id", refID), zap.Error(ingestErr))
	}
	return nil
}
