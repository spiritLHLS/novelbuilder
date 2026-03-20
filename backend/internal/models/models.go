package models

import (
	"encoding/json"
	"time"
)

type Project struct {
	ID               string    `json:"id" db:"id"`
	Title            string    `json:"title" db:"title"`
	Genre            string    `json:"genre" db:"genre"`
	Description      string    `json:"description" db:"description"`
	StyleDescription string    `json:"style_description" db:"style_description"`
	TargetWords      int       `json:"target_words" db:"target_words"`
	ChapterWords     int       `json:"chapter_words" db:"chapter_words"`
	Status           string    `json:"status" db:"status"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

type WorldBible struct {
	ID              string          `json:"id" db:"id"`
	ProjectID       string          `json:"project_id" db:"project_id"`
	Content         json.RawMessage `json:"content" db:"content"`
	MigrationSource *string         `json:"migration_source" db:"migration_source"`
	Version         int             `json:"version" db:"version"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
}

type WorldBibleConstitution struct {
	ID               string          `json:"id" db:"id"`
	ProjectID        string          `json:"project_id" db:"project_id"`
	ImmutableRules   json.RawMessage `json:"immutable_rules" db:"immutable_rules"`
	MutableRules     json.RawMessage `json:"mutable_rules" db:"mutable_rules"`
	ForbiddenAnchors json.RawMessage `json:"forbidden_anchors" db:"forbidden_anchors"`
	Version          int             `json:"version" db:"version"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at" db:"updated_at"`
}

type Character struct {
	ID              string          `json:"id" db:"id"`
	ProjectID       string          `json:"project_id" db:"project_id"`
	Name            string          `json:"name" db:"name"`
	RoleType        string          `json:"role_type" db:"role_type"`
	Profile         json.RawMessage `json:"profile" db:"profile"`
	CurrentState    json.RawMessage `json:"current_state" db:"current_state"`
	VoiceCollection string          `json:"voice_collection" db:"voice_collection"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
}

type Outline struct {
	ID            string          `json:"id" db:"id"`
	ProjectID     string          `json:"project_id" db:"project_id"`
	Level         string          `json:"level" db:"level"`
	ParentID      *string         `json:"parent_id" db:"parent_id"`
	OrderNum      int             `json:"order_num" db:"order_num"`
	Title         string          `json:"title" db:"title"`
	Content       json.RawMessage `json:"content" db:"content"`
	TensionTarget float64         `json:"tension_target" db:"tension_target"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`
}

type Foreshadowing struct {
	ID               string    `json:"id" db:"id"`
	ProjectID        string    `json:"project_id" db:"project_id"`
	Content          string    `json:"content" db:"content"`
	EmbedChapterID   *string   `json:"embed_chapter_id" db:"embed_chapter_id"`
	ResolveChapterID *string   `json:"resolve_chapter_id" db:"resolve_chapter_id"`
	EmbedMethod      string    `json:"embed_method" db:"embed_method"`
	ResolveMethod    string    `json:"resolve_method" db:"resolve_method"`
	Priority         int       `json:"priority" db:"priority"`
	Status           string    `json:"status" db:"status"`
	Tags             []string  `json:"tags" db:"tags"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

type BookBlueprint struct {
	ID             string          `json:"id" db:"id"`
	ProjectID      string          `json:"project_id" db:"project_id"`
	WorldBibleRef  *string         `json:"world_bible_ref" db:"world_bible_ref"`
	MasterOutline  json.RawMessage `json:"master_outline" db:"master_outline"`
	RelationGraph  json.RawMessage `json:"relation_graph" db:"relation_graph"`
	GlobalTimeline json.RawMessage `json:"global_timeline" db:"global_timeline"`
	Status         string          `json:"status" db:"status"`
	Version        int             `json:"version" db:"version"`
	ReviewComment  string          `json:"review_comment" db:"review_comment"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at" db:"updated_at"`
}

type Volume struct {
	ID            string    `json:"id" db:"id"`
	ProjectID     string    `json:"project_id" db:"project_id"`
	VolumeNum     int       `json:"volume_num" db:"volume_num"`
	Title         string    `json:"title" db:"title"`
	BlueprintID   *string   `json:"blueprint_id" db:"blueprint_id"`
	Status        string    `json:"status" db:"status"`
	ChapterStart  int       `json:"chapter_start" db:"chapter_start"`
	ChapterEnd    int       `json:"chapter_end" db:"chapter_end"`
	ReviewComment string    `json:"review_comment" db:"review_comment"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

type Chapter struct {
	ID               string          `json:"id" db:"id"`
	ProjectID        string          `json:"project_id" db:"project_id"`
	VolumeID         *string         `json:"volume_id" db:"volume_id"`
	ChapterNum       int             `json:"chapter_num" db:"chapter_num"`
	Title            string          `json:"title" db:"title"`
	Content          string          `json:"content" db:"content"`
	WordCount        int             `json:"word_count" db:"word_count"`
	Summary          string          `json:"summary" db:"summary"`
	GenParams        json.RawMessage `json:"gen_params" db:"gen_params"`
	QualityReport    json.RawMessage `json:"quality_report" db:"quality_report"`
	OriginalityScore float64         `json:"originality_score" db:"originality_score"`
	Status           string          `json:"status" db:"status"`
	Version          int             `json:"version" db:"version"`
	ReviewComment    string          `json:"review_comment" db:"review_comment"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at" db:"updated_at"`
}

type ChapterSnapshot struct {
	ID               string          `json:"id"`
	ChapterID        string          `json:"chapter_id"`
	Version          int             `json:"version"`
	Title            string          `json:"title"`
	Content          string          `json:"content"`
	WordCount        int             `json:"word_count"`
	Summary          string          `json:"summary"`
	QualityReport    json.RawMessage `json:"quality_report"`
	OriginalityScore float64         `json:"originality_score"`
	Source           string          `json:"source"`
	Note             string          `json:"note"`
	CreatedAt        time.Time       `json:"created_at"`
}

type WorkflowRun struct {
	ID           string    `json:"id" db:"id"`
	ProjectID    string    `json:"project_id" db:"project_id"`
	StrictReview bool      `json:"strict_review" db:"strict_review"`
	CurrentStep  string    `json:"current_step" db:"current_step"`
	Status       string    `json:"status" db:"status"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

type WorkflowStep struct {
	ID            string     `json:"id" db:"id"`
	RunID         string     `json:"run_id" db:"run_id"`
	StepKey       string     `json:"step_key" db:"step_key"`
	StepOrder     int        `json:"step_order" db:"step_order"`
	GateLevel     string     `json:"gate_level" db:"gate_level"`
	Status        string     `json:"status" db:"status"`
	OutputRef     *string    `json:"output_ref" db:"output_ref"`
	SnapshotRef   *string    `json:"snapshot_ref" db:"snapshot_ref"`
	ReviewComment string     `json:"review_comment" db:"review_comment"`
	Version       int        `json:"version" db:"version"`
	GeneratedAt   *time.Time `json:"generated_at" db:"generated_at"`
	ReviewedAt    *time.Time `json:"reviewed_at" db:"reviewed_at"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
}

type WorkflowReview struct {
	ID            string    `json:"id" db:"id"`
	StepID        string    `json:"step_id" db:"step_id"`
	Action        string    `json:"action" db:"action"`
	Operator      string    `json:"operator" db:"operator"`
	Reason        string    `json:"reason" db:"reason"`
	FromStepOrder int       `json:"from_step_order" db:"from_step_order"`
	ToStepOrder   int       `json:"to_step_order" db:"to_step_order"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

type WorkflowSnapshot struct {
	ID             string          `json:"id" db:"id"`
	RunID          string          `json:"run_id" db:"run_id"`
	StepKey        string          `json:"step_key" db:"step_key"`
	Params         json.RawMessage `json:"params" db:"params"`
	ContextPayload json.RawMessage `json:"context_payload" db:"context_payload"`
	OutputPayload  json.RawMessage `json:"output_payload" db:"output_payload"`
	QualityPayload json.RawMessage `json:"quality_payload" db:"quality_payload"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}

type VectorStoreEntry struct {
	ID         string          `json:"id" db:"id"`
	ProjectID  string          `json:"project_id" db:"project_id"`
	Collection string          `json:"collection" db:"collection"`
	Content    string          `json:"content" db:"content"`
	Metadata   json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
}

// Request/Response types

type CreateProjectRequest struct {
	Title            string `json:"title" binding:"required"`
	Genre            string `json:"genre"`
	Description      string `json:"description"`
	StyleDescription string `json:"style_description"`
	TargetWords      int    `json:"target_words"`
	ChapterWords     int    `json:"chapter_words"`
}

type GenerateBlueprintRequest struct {
	Idea              string `json:"idea" binding:"required"`
	Genre             string `json:"genre"`
	VolumeCount       int    `json:"volume_count"`
	ChaptersPerVolume int    `json:"chapters_per_volume"`
}

type ReviewRequest struct {
	ReviewComment string `json:"review_comment"`
	StrictReview  bool   `json:"strict_review"`
}

type RollbackRequest struct {
	TargetStepID string `json:"target_step_id" binding:"required"`
	Reason       string `json:"reason" binding:"required"`
}

type ContinueGenerateRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
}

type GenerateChapterRequest struct {
	ChapterNum      int     `json:"chapter_num"`
	NarrativeOrder  string  `json:"narrative_order"`
	POVCharacter    string  `json:"pov_character"`
	AllowPOVDrift   bool    `json:"allow_pov_drift"`
	TargetPace      string  `json:"target_pace"`
	EndHookType     string  `json:"end_hook_type"`
	EndHookStrength int     `json:"end_hook_strength"`
	TensionLevel    float64 `json:"tension_level"`
	// ChapterWords is the target word count for this chapter (0 = use project default or 3000).
	ChapterWords int `json:"chapter_words"`
	// ContextHint is an optional per-chapter creative direction injected into the user prompt.
	ContextHint string `json:"context_hint"`
	// LLMConfig is populated internally by the handler via agent routing; not sent from the frontend.
	LLMConfig map[string]interface{} `json:"llm_config,omitempty"`
}

type UploadReferenceRequest struct {
	Title  string `json:"title" binding:"required"`
	Author string `json:"author"`
	Genre  string `json:"genre"`
}

type MigrationConfigRequest struct {
	Config map[string]bool `json:"config" binding:"required"`
}

type RestoreChapterSnapshotRequest struct {
	SnapshotID string `json:"snapshot_id" binding:"required"`
}


// ============================================================
// Chapter Import
// ============================================================

type ChapterImport struct {
	ID                string          `json:"id"`
	ProjectID         string          `json:"project_id"`
	SourceText        string          `json:"source_text,omitempty"`
	SplitPattern      string          `json:"split_pattern"`
	FanficMode        *string         `json:"fanfic_mode"`
	Status            string          `json:"status"`
	TotalChapters     int             `json:"total_chapters"`
	ProcessedChapters int             `json:"processed_chapters"`
	ErrorMessage      string          `json:"error_message"`
	ReverseEngineered json.RawMessage `json:"reverse_engineered"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type CreateImportRequest struct {
	SourceText   string  `json:"source_text"   binding:"required"`
	SplitPattern string  `json:"split_pattern"`
	FanficMode   *string `json:"fanfic_mode"`
	LLMProfileID string  `json:"llm_profile_id"`
}

type ProjectFull struct {
	Project
	FanficMode        *string `json:"fanfic_mode"`
	AutoWriteEnabled  bool    `json:"auto_write_enabled"`
	AutoWriteInterval int     `json:"auto_write_interval"`
}

type UpdateProjectFanficRequest struct {
	FanficMode       *string `json:"fanfic_mode"`
	FanficSourceText string  `json:"fanfic_source_text"`
}

type AutoWriteRequest struct {
	IntervalMinutes int    `json:"interval_minutes"`
	LLMProfileID    string `json:"llm_profile_id"`
}

