package models

import (
	"encoding/json"
	"time"
)

type Project struct {
	ID          string    `json:"id" db:"id"`
	Title       string    `json:"title" db:"title"`
	Genre       string    `json:"genre" db:"genre"`
	Description string    `json:"description" db:"description"`
	Status      string    `json:"status" db:"status"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
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
	Title       string `json:"title" binding:"required"`
	Genre       string `json:"genre"`
	Description string `json:"description"`
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
	NarrativeOrder  string  `json:"narrative_order"`
	POVCharacter    string  `json:"pov_character"`
	AllowPOVDrift   bool    `json:"allow_pov_drift"`
	TargetPace      string  `json:"target_pace"`
	EndHookType     string  `json:"end_hook_type"`
	EndHookStrength int     `json:"end_hook_strength"`
	TensionLevel    float64 `json:"tension_level"`
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
