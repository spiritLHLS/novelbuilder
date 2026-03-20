package models

import (
	"encoding/json"
	"time"
)

// ── Change Propagation System ─────────────────────────────────────────────────

// ChangeEvent records a user-initiated edit to any entity along with AI-driven analysis status.
type ChangeEvent struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"project_id"`
	EntityType    string          `json:"entity_type"`
	EntityID      string          `json:"entity_id"`
	ChangeSummary string          `json:"change_summary"`
	OldSnapshot   json.RawMessage `json:"old_snapshot,omitempty"`
	NewSnapshot   json.RawMessage `json:"new_snapshot,omitempty"`
	Status        string          `json:"status"` // pending|analyzed|patching|done|cancelled
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// PatchPlan is the AI-generated propagation plan for one ChangeEvent.
type PatchPlan struct {
	ID            string      `json:"id"`
	ChangeEventID string      `json:"change_event_id"`
	ProjectID     string      `json:"project_id"`
	ImpactSummary string      `json:"impact_summary"`
	TotalItems    int         `json:"total_items"`
	DoneItems     int         `json:"done_items"`
	Status        string      `json:"status"` // ready|executing|done|cancelled
	Items         []PatchItem `json:"items,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

// PatchItem is one artifact to rewrite within a PatchPlan.
type PatchItem struct {
	ID                string          `json:"id"`
	PlanID            string          `json:"plan_id"`
	ItemType          string          `json:"item_type"` // chapter|outline|foreshadowing
	ItemID            string          `json:"item_id"`
	ItemOrder         int             `json:"item_order"`
	ImpactDescription string          `json:"impact_description"`
	PatchInstruction  string          `json:"patch_instruction"`
	Status            string          `json:"status"` // pending|approved|executing|done|skipped|failed
	ResultSnapshot    json.RawMessage `json:"result_snapshot,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// CreateChangeEventRequest is the API body for POST /projects/:id/change-events.
type CreateChangeEventRequest struct {
	EntityType    string          `json:"entity_type" binding:"required"`
	EntityID      string          `json:"entity_id" binding:"required"`
	ChangeSummary string          `json:"change_summary" binding:"required"`
	OldSnapshot   json.RawMessage `json:"old_snapshot"`
	NewSnapshot   json.RawMessage `json:"new_snapshot"`
}

// UpdatePatchItemStatusRequest is the body for PUT /patch-items/:id/status.
type UpdatePatchItemStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// ── Prompt Presets ────────────────────────────────────────────────────────────

type PromptPreset struct {
	ID          string          `json:"id"`
	ProjectID   *string         `json:"project_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	Content     string          `json:"content"`
	Variables   json.RawMessage `json:"variables"`
	IsGlobal    bool            `json:"is_global"`
	SortOrder   int             `json:"sort_order"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type CreatePromptPresetRequest struct {
	Name        string          `json:"name" binding:"required"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	Content     string          `json:"content" binding:"required"`
	Variables   json.RawMessage `json:"variables"`
	IsGlobal    bool            `json:"is_global"`
	SortOrder   int             `json:"sort_order"`
}

type UpdatePromptPresetRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	Content     string          `json:"content"`
	Variables   json.RawMessage `json:"variables"`
	IsGlobal    *bool           `json:"is_global"`
	SortOrder   *int            `json:"sort_order"`
}

// ── Glossary ──────────────────────────────────────────────────────────────────

type GlossaryTerm struct {
	ID         string          `json:"id"`
	ProjectID  string          `json:"project_id"`
	Term       string          `json:"term"`
	Definition string          `json:"definition"`
	Aliases    json.RawMessage `json:"aliases"`
	Category   string          `json:"category"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type CreateGlossaryTermRequest struct {
	Term       string          `json:"term" binding:"required"`
	Definition string          `json:"definition" binding:"required"`
	Aliases    json.RawMessage `json:"aliases"`
	Category   string          `json:"category"`
}

// ── Task Queue ────────────────────────────────────────────────────────────────

type TaskQueueItem struct {
	ID           string          `json:"id"`
	ProjectID    *string         `json:"project_id"`
	TaskType     string          `json:"task_type"`
	Payload      json.RawMessage `json:"payload"`
	Status       string          `json:"status"` // pending|running|done|failed|cancelled
	Priority     int             `json:"priority"`
	Attempts     int             `json:"attempts"`
	MaxAttempts  int             `json:"max_attempts"`
	ErrorMessage string          `json:"error_message"`
	ScheduledAt  time.Time       `json:"scheduled_at"`
	StartedAt    *time.Time      `json:"started_at"`
	CompletedAt  *time.Time      `json:"completed_at"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type CreateTaskRequest struct {
	ProjectID   string          `json:"project_id"`
	TaskType    string          `json:"task_type" binding:"required"`
	Payload     json.RawMessage `json:"payload"`
	Priority    int             `json:"priority"`
	MaxAttempts int             `json:"max_attempts"`
}

// ── Story Resource Ledger ─────────────────────────────────────────────────────

type StoryResource struct {
	ID          string           `json:"id"`
	ProjectID   string           `json:"project_id"`
	Name        string           `json:"name"`
	Category    string           `json:"category"` // item|currency|skill|weapon|misc
	Quantity    float64          `json:"quantity"`
	Unit        string           `json:"unit"`
	Description string           `json:"description"`
	Holder      string           `json:"holder"` // character name or "party"
	Changes     []ResourceChange `json:"changes,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

type ResourceChange struct {
	ID         string    `json:"id"`
	ResourceID string    `json:"resource_id"`
	ChapterID  *string   `json:"chapter_id"`
	Delta      float64   `json:"delta"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}

type CreateStoryResourceRequest struct {
	Name        string  `json:"name" binding:"required"`
	Category    string  `json:"category"`
	Quantity    float64 `json:"quantity"`
	Unit        string  `json:"unit"`
	Description string  `json:"description"`
	Holder      string  `json:"holder"`
}

type RecordResourceChangeRequest struct {
	Delta     float64 `json:"delta" binding:"required"`
	Reason    string  `json:"reason" binding:"required"`
	ChapterID string  `json:"chapter_id"`
}

// ── Vocabulary Fatigue ────────────────────────────────────────────────────────

type VocabFatigueStat struct {
	Word                string  `json:"word"`
	TotalCount          int     `json:"total_count"`
	ChaptersAppeared    int     `json:"chapters_appeared"`
	FrequencyPerChapter float64 `json:"frequency_per_chapter"`
}

type VocabFatigueReport struct {
	ProjectID     string             `json:"project_id"`
	TopWords      []VocabFatigueStat `json:"top_words"`
	TotalChapters int                `json:"total_chapters"`
	AnalyzedAt    time.Time          `json:"analyzed_at"`
}

// ── Webhook Notifications ─────────────────────────────────────────────────────

type NotificationWebhook struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	URL       string          `json:"url"`
	Events    json.RawMessage `json:"events"` // ["chapter_generated","quality_failed",...]
	IsActive  bool            `json:"is_active"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	// Secret is never returned in API responses
}

type CreateWebhookRequest struct {
	URL    string          `json:"url" binding:"required"`
	Secret string          `json:"secret"`
	Events json.RawMessage `json:"events"`
}
