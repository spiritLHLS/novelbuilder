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

// LLMProfile represents a user-configured AI model profile stored in the database.
// API keys are stored encrypted at rest; the API response returns only HasAPIKey and MaskedAPIKey.
type LLMProfile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	BaseURL      string    `json:"base_url"`
	ModelName    string    `json:"model_name"`
	MaxTokens    int       `json:"max_tokens"`
	Temperature  float64   `json:"temperature"`
	IsDefault    bool      `json:"is_default"`
	HasAPIKey    bool      `json:"has_api_key"`
	MaskedAPIKey string    `json:"masked_api_key"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// LLMProfileFull includes the raw API key, used internally only (never serialized in API responses).
type LLMProfileFull struct {
	LLMProfile
	APIKey string `json:"-"`
}

type CreateLLMProfileRequest struct {
	Name        string  `json:"name" binding:"required"`
	Provider    string  `json:"provider" binding:"required"`
	BaseURL     string  `json:"base_url" binding:"required"`
	APIKey      string  `json:"api_key" binding:"required"`
	ModelName   string  `json:"model_name" binding:"required"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
	IsDefault   bool    `json:"is_default"`
}

type UpdateLLMProfileRequest struct {
	Name        string   `json:"name"`
	Provider    string   `json:"provider"`
	BaseURL     string   `json:"base_url"`
	APIKey      string   `json:"api_key"`
	ModelName   string   `json:"model_name"`
	MaxTokens   int      `json:"max_tokens"`
	Temperature *float64 `json:"temperature"`
	IsDefault   *bool    `json:"is_default"`
}

// RAGStatus is returned by GET /api/projects/:id/rag/status.
type RAGCollectionStat struct {
	Collection string `json:"collection"`
	Count      int    `json:"count"`
}

type RAGStatus struct {
	ProjectID   string              `json:"project_id"`
	Collections []RAGCollectionStat `json:"collections"`
	TotalChunks int                 `json:"total_chunks"`
}

type ReferenceMaterial struct {
	ID              string          `json:"id" db:"id"`
	ProjectID       string          `json:"project_id" db:"project_id"`
	Title           string          `json:"title" db:"title"`
	Author          string          `json:"author" db:"author"`
	Genre           string          `json:"genre" db:"genre"`
	FilePath        string          `json:"file_path" db:"file_path"`
	StyleLayer      json.RawMessage `json:"style_layer" db:"style_layer"`
	NarrativeLayer  json.RawMessage `json:"narrative_layer" db:"narrative_layer"`
	AtmosphereLayer json.RawMessage `json:"atmosphere_layer" db:"atmosphere_layer"`
	MigrationConfig json.RawMessage `json:"migration_config" db:"migration_config"`
	StyleCollection string          `json:"style_collection" db:"style_collection"`
	SampleTexts     json.RawMessage `json:"sample_texts,omitempty" db:"sample_texts"`
	Status          string          `json:"status" db:"status"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
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

type QualityReport struct {
	EstimatedPerplexity float64            `json:"estimated_perplexity"`
	EstimatedBurstiness float64            `json:"estimated_burstiness"`
	AIScoreEstimate     float64            `json:"ai_score_estimate"`
	OriginalityScore    float64            `json:"originality_score"`
	SuspiciousSegments  []string           `json:"suspicious_segments"`
	TechUsage           map[string]float64 `json:"tech_usage"`
	TensionCurve        []float64          `json:"tension_curve"`
	RewardDensity       float64            `json:"reward_density"`
	HookStrength        float64            `json:"hook_strength"`
	WorldConsistency    bool               `json:"world_consistency"`
	CharConsistency     bool               `json:"character_consistency"`
	TimeConsistency     bool               `json:"timeline_consistency"`
	OverallScore        float64            `json:"overall_score"`
	Pass                bool               `json:"pass"`
	Issues              []QualityIssue     `json:"issues"`
}

type QualityIssue struct {
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Location   string `json:"location"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
}

type StyleLayer struct {
	AvgSentenceLength      float64            `json:"avg_sentence_length"`
	SentenceLengthStdDev   float64            `json:"sentence_length_std_dev"`
	ShortSentenceRatio     float64            `json:"short_sentence_ratio"`
	LongSentenceRatio      float64            `json:"long_sentence_ratio"`
	DashInnerMonologueRate float64            `json:"dash_inner_monologue_rate"`
	EllipsisRate           float64            `json:"ellipsis_rate"`
	HighFreqAdverbs        []string           `json:"high_freq_adverbs"`
	HighFreqVerbs          []string           `json:"high_freq_verbs"`
	RareWordRate           float64            `json:"rare_word_rate"`
	SensoryDistribution    map[string]float64 `json:"sensory_distribution"`
	DirectEmotionRate      float64            `json:"direct_emotion_rate"`
	BehaviorEmotionRate    float64            `json:"behavior_emotion_rate"`
	SensoryEmotionRate     float64            `json:"sensory_emotion_rate"`
	NLDescription          string             `json:"nl_description"`
}

type NarrativeStructureLayer struct {
	POVType              string   `json:"pov_type"`
	POVDriftFrequency    float64  `json:"pov_drift_frequency"`
	ChronologicalRatio   float64  `json:"chronological_ratio"`
	FlashbackFrequency   float64  `json:"flashback_frequency"`
	FlashbackReturnTypes []string `json:"flashback_return_types"`
	AvgParaPerChapter    float64  `json:"avg_para_per_chapter"`
	DialogueDensity      float64  `json:"dialogue_density"`
	DescriptionDensity   float64  `json:"description_density"`
	SidelongWritingRate  float64  `json:"sidelong_writing_rate"`
}

type AtmosphereLayer struct {
	TemporalSetting       string   `json:"temporal_setting"`
	TechnologicalLevel    string   `json:"technological_level"`
	SocialAtmosphere      string   `json:"social_atmosphere"`
	ToneDescriptions      []string `json:"tone_descriptions"`
	PowerSystemType       string   `json:"power_system_type"`
	PowerSystemRules      []string `json:"power_system_rules"`
	EnvironmentTone       string   `json:"environment_tone"`
	ConflictStyle         string   `json:"conflict_style"`
	SignatureVocabDomains []string `json:"signature_vocab_domains"`
}

// ============================================================
// Multi-Agent Review Types
// ============================================================

// AgentRole identifies which specialist agent is speaking.
type AgentRole string

const (
	AgentOutlineCritic     AgentRole = "outline_critic"     // 大纲批评家
	AgentTimelineInspector AgentRole = "timeline_inspector" // 时间线审核员
	AgentPlotCoherence     AgentRole = "plot_coherence"     // 剧情连贯性专家
	AgentCharacterAnalyst  AgentRole = "character_analyst"  // 角色设计分析师
	AgentDevilsAdvocate    AgentRole = "devils_advocate"    // 魔鬼代言人（反驳者）
	AgentModerator         AgentRole = "moderator"          // 主持人（汇总共识）
)

// AgentMessage is a single turn in the multi-agent debate.
type AgentMessage struct {
	Round     int       `json:"round"`
	Agent     AgentRole `json:"agent"`
	AgentName string    `json:"agent_name"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags"` // e.g. ["issue","suggestion","consensus"]
}

// AgentReviewIssue is a distilled issue from the debate.
type AgentReviewIssue struct {
	Category   string `json:"category"` // outline|timeline|plot|character
	Severity   string `json:"severity"` // critical|major|minor
	Agent      string `json:"agent"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
	Consensus  bool   `json:"consensus"` // agreed by ≥3 agents
}

// AgentReviewSession stores a complete review run.
type AgentReviewSession struct {
	ID          string             `json:"id" db:"id"`
	ProjectID   string             `json:"project_id" db:"project_id"`
	ReviewScope string             `json:"review_scope" db:"review_scope"` // blueprint|chapter|full
	TargetID    string             `json:"target_id" db:"target_id"`       // blueprint_id or chapter_id
	Status      string             `json:"status" db:"status"`             // running|completed|failed
	Rounds      int                `json:"rounds" db:"rounds"`
	Messages    []AgentMessage     `json:"messages" db:"-"`
	Issues      []AgentReviewIssue `json:"issues" db:"-"`
	Consensus   string             `json:"consensus" db:"consensus"` // final summary
	CreatedAt   time.Time          `json:"created_at" db:"created_at"`
	CompletedAt *time.Time         `json:"completed_at" db:"completed_at"`
}

// AgentReviewRequest is the HTTP request body.
type AgentReviewRequest struct {
	Scope    string `json:"scope" binding:"required"` // blueprint|chapter|full
	TargetID string `json:"target_id"`
	Rounds   int    `json:"rounds"` // default 3
}

// ============================================================
// Change Propagation System (Re3 / PEARL-inspired)
// ============================================================

// ChangeEvent records a user-initiated edit to any entity (character, world_bible, etc.)
// along with AI-driven analysis status.
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

// ============================================================
// Prompt Presets (Ai-Novel feature: reusable prompt blocks)
// ============================================================

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

// ============================================================
// Glossary / 术语表 (Ai-Novel feature)
// ============================================================

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

// ============================================================
// Background Task Queue (Ai-Novel: rq_worker-style)
// ============================================================

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

// ============================================================
// Story Resource Ledger (InkOS: particle_ledger concept)
// ============================================================

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

// ============================================================
// Vocabulary Fatigue (InkOS: word fatigue detection)
// ============================================================

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

// ============================================================
// Webhook Notifications (InkOS: event-driven notifications)
// ============================================================

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

// ============================================================
// Agent / LangGraph session models
// ============================================================

type AgentRunRequest struct {
	TaskType     string                 `json:"task_type"` // generate_chapter | review | world_build
	UserPrompt   string                 `json:"user_prompt"`
	ChapterNum   *int                   `json:"chapter_num,omitempty"`
	OutlineHint  string                 `json:"outline_hint,omitempty"`
	StyleProfile map[string]interface{} `json:"style_profile,omitempty"`
	LLMConfig    map[string]interface{} `json:"llm_config,omitempty"`
	MaxRetries   int                    `json:"max_retries,omitempty"`
}

type AgentSessionStatus struct {
	SessionID string                   `json:"session_id"`
	Status    string                   `json:"status"` // running | done | error
	Progress  []map[string]interface{} `json:"progress,omitempty"`
	Result    *AgentResult             `json:"result,omitempty"`
	Error     string                   `json:"error,omitempty"`
}

type AgentResult struct {
	FinalText      string   `json:"final_text"`
	ChapterSummary string   `json:"chapter_summary"`
	QualityScore   float64  `json:"quality_score"`
	QualityIssues  []string `json:"quality_issues"`
}

// ============================================================
// Graph (Neo4j) models
// ============================================================

type GraphNode struct {
	ID    string                 `json:"id"`
	Label string                 `json:"label"`
	Name  string                 `json:"name"`
	Props map[string]interface{} `json:"props,omitempty"`
}

type GraphEdge struct {
	From     string `json:"from"`
	FromName string `json:"from_name"`
	To       string `json:"to"`
	ToName   string `json:"to_name"`
	Type     string `json:"type"`
}

type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type GraphUpsertRequest struct {
	EntityType string                   `json:"entity_type" binding:"required"`
	EntityID   string                   `json:"entity_id"   binding:"required"`
	Name       string                   `json:"name"        binding:"required"`
	Properties map[string]interface{}   `json:"properties,omitempty"`
	Relations  []map[string]interface{} `json:"relations,omitempty"`
}

type GraphQueryRequest struct {
	Cypher string                 `json:"cypher" binding:"required"`
	Params map[string]interface{} `json:"params,omitempty"`
}

// ============================================================
// Vector (Qdrant) models
// ============================================================

type VectorCollectionStat struct {
	Collection string `json:"collection"`
	Count      int    `json:"count"`
}

type VectorStatus struct {
	ProjectID   string                 `json:"project_id"`
	Collections []VectorCollectionStat `json:"collections"`
	TotalChunks int                    `json:"total_chunks"`
}

type VectorSearchRequest struct {
	Collection string `json:"collection" binding:"required"`
	Query      string `json:"query"      binding:"required"`
	Limit      int    `json:"limit,omitempty"`
}

type VectorRebuildRequest struct {
	Items []map[string]interface{} `json:"items" binding:"required"`
}

// ============================================================
// Per-Agent Model Routing
// ============================================================

// AgentType identifies which agent role a route targets.
type AgentType string

const (
	AgentTypeWriter    AgentType = "writer"
	AgentTypeAuditor   AgentType = "auditor"
	AgentTypePlanner   AgentType = "planner"
	AgentTypeReviser   AgentType = "reviser"
	AgentTypeRadar     AgentType = "radar"
	AgentTypeModerator AgentType = "moderator"
)

type AgentModelRoute struct {
	ID           string  `json:"id"`
	AgentType    string  `json:"agent_type"`
	LLMProfileID *string `json:"llm_profile_id"`
	ProjectID    *string `json:"project_id"`
	// Populated on read when llm_profile_id is set
	ProfileName     *string   `json:"profile_name,omitempty"`
	ProfileProvider *string   `json:"profile_provider,omitempty"`
	ProfileModel    *string   `json:"profile_model,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type UpsertAgentRouteRequest struct {
	AgentType    string  `json:"agent_type"`
	ProjectID    *string `json:"project_id"`
	LLMProfileID *string `json:"llm_profile_id"` // null = clear route (use global default)
}

// ============================================================
// Multi-Dimension Audit Reports
// ============================================================

type AuditDimension struct {
	Score  float64  `json:"score"` // 0.0 – 1.0
	Passed bool     `json:"passed"`
	Issues []string `json:"issues"`
}

type AuditReport struct {
	ID            string                    `json:"id"`
	ChapterID     string                    `json:"chapter_id"`
	ProjectID     string                    `json:"project_id"`
	Dimensions    map[string]AuditDimension `json:"dimensions"`
	OverallScore  float64                   `json:"overall_score"`
	Passed        bool                      `json:"passed"`
	AIProbability float64                   `json:"ai_probability"`
	Issues        []string                  `json:"issues"`
	RevisionCount int                       `json:"revision_count"`
	CreatedAt     time.Time                 `json:"created_at"`
}

type AuditChapterRequest struct {
	// Optional: supply llm_profile_id to use a specific profile for the LLM eval pass
	LLMProfileID string `json:"llm_profile_id"`
}

type AuditReviseRequest struct {
	LLMProfileID string `json:"llm_profile_id"`
	MaxRounds    int    `json:"max_rounds"`
	Intensity    string `json:"intensity"`
}

type RestoreChapterSnapshotRequest struct {
	SnapshotID string `json:"snapshot_id" binding:"required"`
}

// ============================================================
// Book Rules
// ============================================================

type BookRules struct {
	ID             string          `json:"id"`
	ProjectID      string          `json:"project_id"`
	RulesContent   string          `json:"rules_content"`
	StyleGuide     string          `json:"style_guide"`
	AntiAIWordlist json.RawMessage `json:"anti_ai_wordlist"`
	BannedPatterns json.RawMessage `json:"banned_patterns"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type UpdateBookRulesRequest struct {
	RulesContent   string          `json:"rules_content"`
	StyleGuide     string          `json:"style_guide"`
	AntiAIWordlist json.RawMessage `json:"anti_ai_wordlist"`
	BannedPatterns json.RawMessage `json:"banned_patterns"`
}

// ============================================================
// Creative Brief
// ============================================================

type CreativeBriefRequest struct {
	BriefText    string `json:"brief_text" binding:"required"`
	Genre        string `json:"genre"`
	LLMProfileID string `json:"llm_profile_id"`
}

type CreativeBriefResult struct {
	WorldBible     map[string]interface{} `json:"world_bible"`
	RulesContent   string                 `json:"rules_content"`
	StyleGuide     string                 `json:"style_guide"`
	AntiAIWordlist []string               `json:"anti_ai_wordlist"`
	BannedPatterns []string               `json:"banned_patterns"`
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

// ============================================================
// Anti-AI Rewrite
// ============================================================

type AntiDetectRequest struct {
	Intensity    string `json:"intensity"` // light|medium|heavy
	LLMProfileID string `json:"llm_profile_id"`
}

type AntiDetectResult struct {
	OriginalText  string   `json:"original_text"`
	RewrittenText string   `json:"rewritten_text"`
	ChangesMade   []string `json:"changes_made"`
	AIProbBefore  float64  `json:"ai_prob_before"`
	AIProbAfter   float64  `json:"ai_prob_after"`
}

// ============================================================
// Project extensions (fanfic + auto-write)
// ============================================================

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

// ============================================================
// Genre Templates
// ============================================================

type GenreTemplate struct {
	ID                   string          `json:"id"`
	Genre                string          `json:"genre"`
	RulesContent         string          `json:"rules_content"`
	LanguageConstraints  string          `json:"language_constraints"`
	RhythmRules          string          `json:"rhythm_rules"`
	AuditDimensionsExtra json.RawMessage `json:"audit_dimensions_extra"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type UpsertGenreTemplateRequest struct {
	RulesContent         string          `json:"rules_content"`
	LanguageConstraints  string          `json:"language_constraints"`
	RhythmRules          string          `json:"rhythm_rules"`
	AuditDimensionsExtra json.RawMessage `json:"audit_dimensions_extra"`
}
