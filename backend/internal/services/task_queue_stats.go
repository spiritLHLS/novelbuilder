package services

import (
	"sort"
	"strings"
	"time"

	"github.com/novelbuilder/backend/internal/models"
)

func populateTaskMetrics(t *models.TaskQueueItem, now time.Time) {
	if t == nil {
		return
	}
	if !t.ScheduledAt.IsZero() {
		waitEnd := now
		if t.StartedAt != nil && !t.StartedAt.IsZero() {
			waitEnd = *t.StartedAt
		}
		if waitEnd.After(t.ScheduledAt) {
			t.QueueWaitMs = waitEnd.Sub(t.ScheduledAt).Milliseconds()
		}
	}
	if t.StartedAt != nil && !t.StartedAt.IsZero() {
		runEnd := now
		if t.CompletedAt != nil && !t.CompletedAt.IsZero() {
			runEnd = *t.CompletedAt
		}
		if runEnd.After(*t.StartedAt) {
			t.RuntimeMs = runEnd.Sub(*t.StartedAt).Milliseconds()
		}
	}
}

func normalizeTaskFailureReason(message string) string {
	reason := strings.TrimSpace(message)
	if reason == "" {
		return "unknown failure"
	}
	reason = strings.Join(strings.Fields(reason), " ")
	runes := []rune(reason)
	if len(runes) <= 140 {
		return reason
	}
	return string(runes[:140]) + "..."
}

func sortedFailureReasons(counts map[string]int, limit int) []models.TaskQueueFailureReason {
	reasons := make([]models.TaskQueueFailureReason, 0, len(counts))
	for message, count := range counts {
		reasons = append(reasons, models.TaskQueueFailureReason{Message: message, Count: count})
	}
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].Count == reasons[j].Count {
			return reasons[i].Message < reasons[j].Message
		}
		return reasons[i].Count > reasons[j].Count
	})
	if limit > 0 && len(reasons) > limit {
		reasons = reasons[:limit]
	}
	return reasons
}

func sortedProjectThroughput(counts map[string]int, limit int) []models.TaskQueueProjectThroughput {
	items := make([]models.TaskQueueProjectThroughput, 0, len(counts))
	for projectID, count := range counts {
		items = append(items, models.TaskQueueProjectThroughput{ProjectID: projectID, Done24h: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Done24h == items[j].Done24h {
			return items[i].ProjectID < items[j].ProjectID
		}
		return items[i].Done24h > items[j].Done24h
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}
