package services

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"html"
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load chapters rows: %w", err)
	}
	if len(chapters) == 0 {
		s.logger.Warn("no approved chapters found for export", zap.String("project_id", projectID))
	}
	return chapters, nil
}

// ExportEPUB returns the full novel as a valid EPUB 3 byte slice.
// Each approved chapter becomes a separate XHTML document inside the archive.
func (s *ExportService) ExportEPUB(ctx context.Context, projectID string) ([]byte, error) {
	var title, genre string
	if err := s.db.QueryRow(ctx,
		`SELECT title, COALESCE(genre,'') FROM projects WHERE id = $1`, projectID).Scan(&title, &genre); err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	chapters, err := s.loadApprovedChapters(ctx, projectID)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// 1. mimetype (must be first, uncompressed)
	mw, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		return nil, err
	}
	mw.Write([]byte("application/epub+zip"))

	// 2. META-INF/container.xml
	cw, _ := zw.Create("META-INF/container.xml")
	cw.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`))

	// 3. OEBPS/content.opf
	var manifestItems, spineItems strings.Builder
	manifestItems.WriteString(`    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>` + "\n")
	for _, ch := range chapters {
		id := fmt.Sprintf("ch%d", ch.num)
		href := fmt.Sprintf("chapter_%d.xhtml", ch.num)
		manifestItems.WriteString(fmt.Sprintf(`    <item id=%q href=%q media-type="application/xhtml+xml"/>`, id, href) + "\n")
		spineItems.WriteString(fmt.Sprintf(`    <itemref idref=%q/>`, id) + "\n")
	}

	opf := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package version="3.0" xmlns="http://www.idpf.org/2007/opf" unique-identifier="uid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>%s</dc:title>
    <dc:language>zh</dc:language>
    <dc:identifier id="uid">urn:uuid:%s</dc:identifier>
    %s
  </metadata>
  <manifest>
%s  </manifest>
  <spine>
    <itemref idref="nav"/>
%s  </spine>
</package>`,
		html.EscapeString(title),
		projectID,
		func() string {
			if genre != "" {
				return fmt.Sprintf(`<dc:subject>%s</dc:subject>`, html.EscapeString(genre))
			}
			return ""
		}(),
		manifestItems.String(),
		spineItems.String(),
	)
	ow, _ := zw.Create("OEBPS/content.opf")
	ow.Write([]byte(opf))

	// 4. OEBPS/nav.xhtml (navigation document)
	var navItems strings.Builder
	for _, ch := range chapters {
		chapterTitle := ch.title
		if chapterTitle == "" {
			chapterTitle = fmt.Sprintf("第%d章", ch.num)
		}
		navItems.WriteString(fmt.Sprintf(
			`      <li><a href="chapter_%d.xhtml">%s</a></li>`+"\n",
			ch.num, html.EscapeString(fmt.Sprintf("第%d章　%s", ch.num, chapterTitle))))
	}
	nav := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><meta charset="UTF-8"/><title>目录</title></head>
<body>
  <nav epub:type="toc" id="toc">
    <h1>目录</h1>
    <ol>
%s    </ol>
  </nav>
</body>
</html>`, navItems.String())
	nw, _ := zw.Create("OEBPS/nav.xhtml")
	nw.Write([]byte(nav))

	// 5. One XHTML per chapter
	for _, ch := range chapters {
		chapterTitle := ch.title
		if chapterTitle == "" {
			chapterTitle = fmt.Sprintf("第%d章", ch.num)
		}
		var paras strings.Builder
		for _, line := range strings.Split(ch.content, "\n") {
			t := strings.TrimSpace(line)
			if t != "" {
				paras.WriteString(fmt.Sprintf("  <p>%s</p>\n", html.EscapeString(t)))
			}
		}
		chXHTML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><meta charset="UTF-8"/><title>%s</title></head>
<body>
<h1>%s</h1>
%s</body>
</html>`,
			html.EscapeString(fmt.Sprintf("第%d章　%s", ch.num, chapterTitle)),
			html.EscapeString(fmt.Sprintf("第%d章　%s", ch.num, chapterTitle)),
			paras.String(),
		)
		xw, _ := zw.Create(fmt.Sprintf("OEBPS/chapter_%d.xhtml", ch.num))
		xw.Write([]byte(chXHTML))
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("zip close: %w", err)
	}
	return buf.Bytes(), nil
}

// epubSizeHint returns rough UTF-8 char count across chapters (for logging).
func epubSizeHint(chapters []chapterExportRow) int {
	total := 0
	for _, ch := range chapters {
		total += utf8.RuneCountInString(ch.content)
	}
	return total
}
