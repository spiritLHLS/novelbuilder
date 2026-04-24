package services

import (
	"context"
	"errors"
	"strings"

	"github.com/novelbuilder/backend/internal/models"
)

func GenerateChapterWithQualityRetries(
	ctx context.Context,
	chapters *ChapterService,
	quality *QualityService,
	projectID string,
	chapterNum int,
	req models.GenerateChapterRequest,
) (*models.Chapter, *models.QualityReport, error) {
	return runChapterQualityRetries(
		ctx,
		chapters,
		quality,
		func() (*models.Chapter, error) {
			return chapters.Generate(ctx, projectID, chapterNum, req)
		},
		func(chapter *models.Chapter) (*models.Chapter, error) {
			return chapters.Regenerate(ctx, chapter.ID, req)
		},
	)
}

func RegenerateChapterWithQualityRetries(
	ctx context.Context,
	chapters *ChapterService,
	quality *QualityService,
	chapterID string,
	req models.GenerateChapterRequest,
) (*models.Chapter, *models.QualityReport, error) {
	if strings.TrimSpace(chapterID) == "" {
		return nil, nil, errors.New("chapter id is required")
	}
	chapter, err := chapters.Get(ctx, chapterID)
	if err != nil {
		return nil, nil, err
	}
	if chapter == nil {
		return nil, nil, errors.New("chapter not found")
	}

	return runChapterQualityRetries(
		ctx,
		chapters,
		quality,
		func() (*models.Chapter, error) {
			return chapters.Regenerate(ctx, chapterID, req)
		},
		func(chapter *models.Chapter) (*models.Chapter, error) {
			return chapters.Regenerate(ctx, chapter.ID, req)
		},
	)
}

func runChapterQualityRetries(
	ctx context.Context,
	chapters *ChapterService,
	quality *QualityService,
	firstAttempt func() (*models.Chapter, error),
	retryAttempt func(*models.Chapter) (*models.Chapter, error),
) (*models.Chapter, *models.QualityReport, error) {
	const maxAttempts = 3

	var chapter *models.Chapter
	var report *models.QualityReport

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var err error
		if chapter == nil {
			chapter, err = firstAttempt()
		} else {
			chapter, err = retryAttempt(chapter)
		}
		if err != nil {
			return nil, nil, err
		}

		report, err = quality.RunFullCheck(ctx, chapter.ID)
		if err != nil {
			return nil, nil, err
		}

		paused := !report.Pass && attempt == maxAttempts
		report.GenerationControl = &models.QualityGenerationControl{
			AttemptCount:      attempt,
			MaxAttempts:       maxAttempts,
			State:             qualityControlState(report.Pass, paused),
			Paused:            paused,
			RecommendedAction: qualityControlAction(report.Pass, paused),
			LastIssues:        summarizeQualityIssues(report.Issues, 5),
		}
		if err := quality.SaveReport(ctx, chapter.ID, report); err != nil {
			return nil, nil, err
		}

		if report.Pass || paused {
			if paused {
				_ = chapters.CreateSnapshot(ctx, chapter.ID, "quality_paused", "auto generation paused after max failed quality attempts")
			}
			if latest, getErr := chapters.Get(ctx, chapter.ID); getErr == nil && latest != nil {
				chapter = latest
			}
			return chapter, report, nil
		}
	}

	return chapter, report, nil
}

func summarizeQualityIssues(issues []models.QualityIssue, limit int) []string {
	if limit <= 0 {
		limit = 5
	}
	out := make([]string, 0, min(limit, len(issues)))
	for _, issue := range issues {
		desc := strings.TrimSpace(issue.Message)
		if desc == "" {
			desc = strings.TrimSpace(issue.Suggestion)
		}
		if desc == "" {
			continue
		}
		out = append(out, desc)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func qualityControlState(passed, paused bool) string {
	if passed {
		return "passed"
	}
	if paused {
		return "paused"
	}
	return "retrying"
}

func qualityControlAction(passed, paused bool) string {
	if passed {
		return "chapter_review"
	}
	if paused {
		return "manual_revise_then_recheck"
	}
	return "auto_retry"
}
