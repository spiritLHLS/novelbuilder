package handlers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ── Fanqie Account CRUD ──────────────────────────────────────────────────────

// ConfigureFanqie saves or updates the Fanqie account for a project.
func (h *Handler) ConfigureFanqie(c *gin.Context) {
	projectID := c.Param("id")
	if _, err := uuid.Parse(projectID); err != nil {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return
	}

	var req struct {
		Cookies   string `json:"cookies"`
		BookID    string `json:"book_id"`
		BookTitle string `json:"book_title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Cookies == "" {
		c.JSON(400, gin.H{"error": "cookies is required"})
		return
	}

	ctx := c.Request.Context()
	db := h.projects.DB()

	// Upsert fanqie_accounts
	_, err := db.Exec(ctx, `
		INSERT INTO fanqie_accounts (project_id, cookies, book_id, book_title, status, last_validated_at)
		VALUES ($1, $2, $3, $4, 'active', now())
		ON CONFLICT (project_id) DO UPDATE SET
			cookies           = EXCLUDED.cookies,
			book_id           = EXCLUDED.book_id,
			book_title        = EXCLUDED.book_title,
			status            = 'active',
			last_validated_at = now(),
			updated_at        = now()
	`, projectID, req.Cookies, req.BookID, req.BookTitle)
	if err != nil {
		h.logger.Error("upsert fanqie account", zap.Error(err))
		c.JSON(500, gin.H{"error": "保存番茄账号配置失败"})
		return
	}

	c.JSON(200, gin.H{"message": "番茄账号配置已保存"})
}

// GetFanqieAccount returns the Fanqie configuration for a project.
func (h *Handler) GetFanqieAccount(c *gin.Context) {
	projectID := c.Param("id")
	if _, err := uuid.Parse(projectID); err != nil {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return
	}

	ctx := c.Request.Context()
	db := h.projects.DB()

	var bookID, bookTitle, status string
	var lastValidated *time.Time
	err := db.QueryRow(ctx, `
		SELECT book_id, book_title, status, last_validated_at
		FROM fanqie_accounts WHERE project_id = $1
	`, projectID).Scan(&bookID, &bookTitle, &status, &lastValidated)
	if err != nil {
		// No account configured yet — return empty
		c.JSON(200, gin.H{
			"configured": false,
			"book_id":    "",
			"book_title": "",
			"status":     "unconfigured",
		})
		return
	}

	c.JSON(200, gin.H{
		"configured":     true,
		"book_id":        bookID,
		"book_title":     bookTitle,
		"status":         status,
		"last_validated": lastValidated,
	})
}

// ValidateFanqieCookies validates stored cookies by calling the Python sidecar.
func (h *Handler) ValidateFanqieCookies(c *gin.Context) {
	projectID := c.Param("id")
	if _, err := uuid.Parse(projectID); err != nil {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return
	}

	ctx := c.Request.Context()
	cookies, err := h.getFanqieCookies(ctx, projectID)
	if err != nil {
		c.JSON(400, gin.H{"error": "未配置番茄账号"})
		return
	}

	raw, err := h.sidecar.Post(ctx, "/fanqie/validate", map[string]interface{}{
		"project_id": projectID,
		"cookies":    cookies,
	})
	if err != nil {
		h.logger.Error("validate fanqie cookies", zap.Error(err))
		c.JSON(500, gin.H{"error": "验证失败: " + err.Error()})
		return
	}

	c.Data(200, "application/json", raw)
}

// ListFanqieBooks lists the user's books on Fanqie.
func (h *Handler) ListFanqieBooks(c *gin.Context) {
	projectID := c.Param("id")
	if _, err := uuid.Parse(projectID); err != nil {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return
	}

	ctx := c.Request.Context()
	cookies, err := h.getFanqieCookies(ctx, projectID)
	if err != nil {
		c.JSON(400, gin.H{"error": "未配置番茄账号"})
		return
	}

	raw, err := h.sidecar.Post(ctx, "/fanqie/books", map[string]interface{}{
		"project_id": projectID,
		"cookies":    cookies,
	})
	if err != nil {
		h.logger.Error("list fanqie books", zap.Error(err))
		c.JSON(500, gin.H{"error": "获取作品列表失败: " + err.Error()})
		return
	}

	c.Data(200, "application/json", raw)
}

// ── Chapter Upload ───────────────────────────────────────────────────────────

// UploadChapterToFanqie uploads a single chapter to Fanqie.
func (h *Handler) UploadChapterToFanqie(c *gin.Context) {
	projectID := c.Param("id")
	chapterID := c.Param("chapter_id")
	if _, err := uuid.Parse(projectID); err != nil {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return
	}
	if _, err := uuid.Parse(chapterID); err != nil {
		c.JSON(400, gin.H{"error": "invalid chapter id"})
		return
	}

	ctx := c.Request.Context()

	// Get fanqie account
	cookies, bookID, err := h.getFanqieAccountFull(ctx, projectID)
	if err != nil {
		c.JSON(400, gin.H{"error": "未配置番茄账号或作品"})
		return
	}
	if bookID == "" {
		c.JSON(400, gin.H{"error": "请先在番茄配置中选择目标作品"})
		return
	}

	// Get chapter content
	chapter, err := h.chapters.Get(ctx, chapterID)
	if err != nil {
		c.JSON(404, gin.H{"error": "章节不存在"})
		return
	}

	// Record upload attempt
	db := h.projects.DB()
	_, _ = db.Exec(ctx, `
		INSERT INTO fanqie_uploads (project_id, chapter_id, status)
		VALUES ($1, $2, 'uploading')
		ON CONFLICT (project_id, chapter_id) DO UPDATE SET
			status = 'uploading', error_message = '', updated_at = now()
	`, projectID, chapterID)

	// Call sidecar
	raw, err := h.sidecar.Post(ctx, "/fanqie/upload-with-cookies", map[string]interface{}{
		"project_id": projectID,
		"cookies":    cookies,
		"book_id":    bookID,
		"title":      chapter.Title,
		"content":    chapter.Content,
		"chapter_id": chapterID,
	})
	if err != nil {
		// Record failure
		_, _ = db.Exec(ctx, `
			UPDATE fanqie_uploads SET status = 'failed', error_message = $3, updated_at = now()
			WHERE project_id = $1 AND chapter_id = $2
		`, projectID, chapterID, err.Error())

		h.logger.Error("upload chapter to fanqie", zap.Error(err))
		c.JSON(500, gin.H{"error": "上传失败: " + err.Error()})
		return
	}

	// Parse result
	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err == nil {
		status := "success"
		if s, ok := result["status"].(string); ok && s != "success" {
			status = s
		}
		fanqieChapterID := ""
		if fid, ok := result["fanqie_chapter_id"].(string); ok {
			fanqieChapterID = fid
		}

		_, _ = db.Exec(ctx, `
			UPDATE fanqie_uploads
			SET status = $3, fanqie_chapter_id = $4, uploaded_at = now(), updated_at = now()
			WHERE project_id = $1 AND chapter_id = $2
		`, projectID, chapterID, status, fanqieChapterID)
	}

	c.Data(200, "application/json", raw)
}

// BatchUploadToFanqie uploads multiple chapters sequentially.
func (h *Handler) BatchUploadToFanqie(c *gin.Context) {
	projectID := c.Param("id")
	if _, err := uuid.Parse(projectID); err != nil {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return
	}

	var req struct {
		ChapterIDs []string `json:"chapter_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.ChapterIDs) == 0 {
		c.JSON(400, gin.H{"error": "chapter_ids is required"})
		return
	}

	ctx := c.Request.Context()

	cookies, bookID, err := h.getFanqieAccountFull(ctx, projectID)
	if err != nil || bookID == "" {
		c.JSON(400, gin.H{"error": "未配置番茄账号或作品"})
		return
	}

	// Gather chapter data
	chapters := make([]map[string]interface{}, 0, len(req.ChapterIDs))
	for _, cid := range req.ChapterIDs {
		if _, err := uuid.Parse(cid); err != nil {
			continue
		}
		ch, err := h.chapters.Get(ctx, cid)
		if err != nil {
			continue
		}
		chapters = append(chapters, map[string]interface{}{
			"chapter_id": cid,
			"title":      ch.Title,
			"content":    ch.Content,
		})
	}

	if len(chapters) == 0 {
		c.JSON(400, gin.H{"error": "未找到有效章节"})
		return
	}

	// Call sidecar batch upload
	raw, err := h.sidecar.Post(ctx, "/fanqie/batch-upload", map[string]interface{}{
		"project_id": projectID,
		"cookies":    cookies,
		"book_id":    bookID,
		"chapters":   chapters,
	})
	if err != nil {
		h.logger.Error("batch upload to fanqie", zap.Error(err))
		c.JSON(500, gin.H{"error": "批量上传失败: " + err.Error()})
		return
	}

	// Update upload records
	db := h.projects.DB()
	var batchResult struct {
		Results []struct {
			ChapterID string `json:"chapter_id"`
			Status    string `json:"status"`
			Error     string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &batchResult); err == nil {
		for _, r := range batchResult.Results {
			if r.ChapterID == "" {
				continue
			}
			errMsg := r.Error
			_, _ = db.Exec(ctx, `
				INSERT INTO fanqie_uploads (project_id, chapter_id, status, error_message, uploaded_at)
				VALUES ($1, $2, $3, $4, CASE WHEN $3 = 'success' THEN now() ELSE NULL END)
				ON CONFLICT (project_id, chapter_id) DO UPDATE SET
					status = EXCLUDED.status,
					error_message = EXCLUDED.error_message,
					uploaded_at = EXCLUDED.uploaded_at,
					updated_at = now()
			`, projectID, r.ChapterID, r.Status, errMsg)
		}
	}

	c.Data(200, "application/json", raw)
}

// ── Upload Status ────────────────────────────────────────────────────────────

// ListFanqieUploads returns upload records for all chapters in a project.
func (h *Handler) ListFanqieUploads(c *gin.Context) {
	projectID := c.Param("id")
	if _, err := uuid.Parse(projectID); err != nil {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return
	}

	ctx := c.Request.Context()
	db := h.projects.DB()

	rows, err := db.Query(ctx, `
		SELECT fu.chapter_id, c.chapter_num, c.title, fu.status,
		       fu.fanqie_chapter_id, fu.error_message, fu.uploaded_at
		FROM fanqie_uploads fu
		JOIN chapters c ON c.id = fu.chapter_id
		WHERE fu.project_id = $1
		ORDER BY c.chapter_num
	`, projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type uploadRow struct {
		ChapterID       string     `json:"chapter_id"`
		ChapterNum      int        `json:"chapter_num"`
		Title           string     `json:"title"`
		Status          string     `json:"status"`
		FanqieChapterID string     `json:"fanqie_chapter_id"`
		ErrorMessage    string     `json:"error_message"`
		UploadedAt      *time.Time `json:"uploaded_at"`
	}

	uploads := make([]uploadRow, 0)
	for rows.Next() {
		var r uploadRow
		if err := rows.Scan(&r.ChapterID, &r.ChapterNum, &r.Title, &r.Status,
			&r.FanqieChapterID, &r.ErrorMessage, &r.UploadedAt); err != nil {
			continue
		}
		uploads = append(uploads, r)
	}

	c.JSON(200, gin.H{"data": uploads})
}

// GetFanqieLoginScreenshot returns a screenshot of the Fanqie login page.
func (h *Handler) GetFanqieLoginScreenshot(c *gin.Context) {
	projectID := c.Param("id")
	if _, err := uuid.Parse(projectID); err != nil {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return
	}

	raw, err := h.sidecar.Post(c.Request.Context(), "/fanqie/login-screenshot", map[string]interface{}{
		"project_id": projectID,
	})
	if err != nil {
		c.JSON(500, gin.H{"error": "获取登录页截图失败: " + err.Error()})
		return
	}

	c.Data(200, "application/json", raw)
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func (h *Handler) getFanqieCookies(ctx context.Context, projectID string) (string, error) {
	db := h.projects.DB()
	var cookies string
	err := db.QueryRow(ctx, `
		SELECT cookies FROM fanqie_accounts WHERE project_id = $1 AND status != 'unconfigured'
	`, projectID).Scan(&cookies)
	return cookies, err
}

func (h *Handler) getFanqieAccountFull(ctx context.Context, projectID string) (cookies, bookID string, err error) {
	db := h.projects.DB()
	err = db.QueryRow(ctx, `
		SELECT cookies, book_id FROM fanqie_accounts
		WHERE project_id = $1 AND status != 'unconfigured'
	`, projectID).Scan(&cookies, &bookID)
	return
}
