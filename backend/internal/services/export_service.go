package services

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// ExportService builds plain-text and Markdown exports of an entire project.
type ExportService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewExportService(db *pgxpool.Pool, logger *zap.Logger) *ExportService {
	return &ExportService{db: db, logger: logger}
}

// ExportTXT returns the full novel as a UTF-8 plain-text byte slice.
// Only approved chapters are included, sorted by chapter_num.
func (s *ExportService) ExportTXT(ctx context.Context, projectID string) ([]byte, error) {
	// Fetch project metadata
	var title, genre string
	if err := s.db.QueryRow(ctx,
		`SELECT title, genre FROM projects WHERE id = $1`, projectID).Scan(&title, &genre); err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	chapters, err := s.loadApprovedChapters(ctx, projectID)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	sb.WriteString(title)
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("=", utf8.RuneCountInString(title)))
	sb.WriteString("\n")
	if genre != "" {
		sb.WriteString("类型：")
		sb.WriteString(genre)
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("共 %d 章\n\n", len(chapters)))

	for _, ch := range chapters {
		sb.WriteString(fmt.Sprintf("第%d章  %s\n", ch.num, ch.title))
		sb.WriteString(strings.Repeat("-", 40))
		sb.WriteString("\n")
		sb.WriteString(ch.content)
		sb.WriteString("\n\n")
	}

	return []byte(sb.String()), nil
}

// ExportMarkdown returns the full novel as a Markdown byte slice.
func (s *ExportService) ExportMarkdown(ctx context.Context, projectID string) ([]byte, error) {
	var title, genre string
	if err := s.db.QueryRow(ctx,
		`SELECT title, genre FROM projects WHERE id = $1`, projectID).Scan(&title, &genre); err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	chapters, err := s.loadApprovedChapters(ctx, projectID)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(title)
	sb.WriteString("\n\n")
	if genre != "" {
		sb.WriteString("> 类型：")
		sb.WriteString(genre)
		sb.WriteString("\n\n")
	}
	sb.WriteString(fmt.Sprintf("共 **%d** 章\n\n---\n\n", len(chapters)))

	for _, ch := range chapters {
		sb.WriteString(fmt.Sprintf("## 第%d章　%s\n\n", ch.num, ch.title))
		// Wrap paragraphs: each non-empty line becomes a paragraph
		for _, line := range strings.Split(ch.content, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				sb.WriteString(trimmed)
				sb.WriteString("\n\n")
			}
		}
	}

	return []byte(sb.String()), nil
}

type chapterExportRow struct {
	num     int
	title   string
	content string
}

func (s *ExportService) loadApprovedChapters(ctx context.Context, projectID string) ([]chapterExportRow, error) {
	rows, err := s.db.Query(ctx,
		`SELECT chapter_num, title, content FROM chapters
		 WHERE project_id = $1 AND status = 'approved'
		 ORDER BY chapter_num`,
		projectID)
	if err != nil {
		return nil, fmt.Errorf("load chapters: %w", err)
	}
	defer rows.Close()

	var chapters []chapterExportRow
	for rows.Next() {
		var ch chapterExportRow
		if err := rows.Scan(&ch.num, &ch.title, &ch.content); err != nil {
			return nil, err
		}
		chapters = append(chapters, ch)
	}
	if len(chapters) == 0 {
		s.logger.Warn("no approved chapters found for export", zap.String("project_id", projectID))
	}
	return chapters, nil
}
