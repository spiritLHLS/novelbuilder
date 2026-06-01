package services

import (
	"context"
	"fmt"
	"github.com/novelbuilder/backend/internal/database"
)

type ContinuationContext struct {
	Enabled               bool
	RefID                 string
	StartChapter          int
	ReferenceChapterCount int
	LastReferenceChapter  int
}

type ReferenceChapterSnippet struct {
	ChapterNo int
	Title     string
	Snippet   string
}

func loadContinuationContext(ctx context.Context, db *database.DB, projectID string) (ContinuationContext, error) {
	var out ContinuationContext
	var isContinuation bool
	var refID *string
	var startChapter int
	if err := db.QueryRow(ctx,
		`SELECT COALESCE(project_type, 'original') = 'continuation',
		        continuation_ref_id,
		        COALESCE(continuation_start_chapter, 0)
		 FROM projects WHERE id = $1`,
		projectID,
	).Scan(&isContinuation, &refID, &startChapter); err != nil {
		return out, fmt.Errorf("load continuation project state: %w", err)
	}
	if !isContinuation || refID == nil || *refID == "" {
		return out, nil
	}
	out.Enabled = true
	out.RefID = *refID
	out.StartChapter = startChapter
	if out.StartChapter <= 0 {
		out.StartChapter = 1
	}

	_ = db.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(MAX(chapter_no), 0)
		 FROM reference_book_chapters
		 WHERE ref_id = $1 AND is_deleted = FALSE`,
		out.RefID,
	).Scan(&out.ReferenceChapterCount, &out.LastReferenceChapter)

	if out.LastReferenceChapter > 0 && out.StartChapter <= out.LastReferenceChapter {
		out.StartChapter = out.LastReferenceChapter + 1
	}
	return out, nil
}

func nextWritableChapterNum(ctx context.Context, db *database.DB, projectID string) (int, error) {
	var maxChapter int
	if err := db.QueryRow(ctx,
		`SELECT COALESCE(MAX(chapter_num), 0) FROM chapters WHERE project_id = $1`,
		projectID,
	).Scan(&maxChapter); err != nil {
		return 0, fmt.Errorf("load max chapter num: %w", err)
	}
	next := maxChapter + 1
	cc, err := loadContinuationContext(ctx, db, projectID)
	if err != nil {
		return 0, err
	}
	if cc.Enabled && cc.ReferenceChapterCount > 0 && next < cc.StartChapter {
		next = cc.StartChapter
	}
	return next, nil
}

func loadContinuationTail(ctx context.Context, db *database.DB, refID string, beforeChapter int, limit int, snippetRunes int) ([]ReferenceChapterSnippet, error) {
	if refID == "" || beforeChapter <= 1 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	if snippetRunes <= 0 {
		snippetRunes = 800
	}
	rows, err := db.Query(ctx,
		`SELECT chapter_no, title, LEFT(content, $4) AS snippet
		 FROM reference_book_chapters
		 WHERE ref_id = $1 AND is_deleted = FALSE AND chapter_no < $2
		 ORDER BY chapter_no DESC
		 LIMIT $3`,
		refID, beforeChapter, limit, snippetRunes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reversed := make([]ReferenceChapterSnippet, 0, limit)
	for rows.Next() {
		var item ReferenceChapterSnippet
		if err := rows.Scan(&item.ChapterNo, &item.Title, &item.Snippet); err != nil {
			return nil, err
		}
		reversed = append(reversed, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, nil
}
