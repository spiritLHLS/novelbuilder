package models

import (
	"encoding/json"
	"time"
)

// ── LLM Profile Types ─────────────────────────────────────────────────────────

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

// ── RAG Types ─────────────────────────────────────────────────────────────────

// RAGCollectionStat is returned by GET /api/projects/:id/rag/status.
type RAGCollectionStat struct {
	Collection string `json:"collection"`
	Count      int    `json:"count"`
}

type RAGStatus struct {
	ProjectID   string              `json:"project_id"`
	Collections []RAGCollectionStat `json:"collections"`
	TotalChunks int                 `json:"total_chunks"`
}

// ── Reference Material Types ──────────────────────────────────────────────────

type ReferenceMaterial struct {
	ID              string          `json:"id" db:"id"`
	ProjectID       string          `json:"project_id" db:"project_id"`
	Title           string          `json:"title" db:"title"`
	Author          string          `json:"author" db:"author"`
	Genre           string          `json:"genre" db:"genre"`
	FilePath        string          `json:"file_path" db:"file_path"`
	SourceURL       string          `json:"source_url" db:"source_url"`
	StyleLayer      json.RawMessage `json:"style_layer" db:"style_layer"`
	NarrativeLayer  json.RawMessage `json:"narrative_layer" db:"narrative_layer"`
	AtmosphereLayer json.RawMessage `json:"atmosphere_layer" db:"atmosphere_layer"`
	MigrationConfig json.RawMessage `json:"migration_config" db:"migration_config"`
	StyleCollection string          `json:"style_collection" db:"style_collection"`
	SampleTexts     json.RawMessage `json:"sample_texts,omitempty" db:"sample_texts"`
	Status          string          `json:"status" db:"status"`
	// Download task tracking (migration 014)
	FetchStatus     string          `json:"fetch_status" db:"fetch_status"`
	FetchDone       int             `json:"fetch_done" db:"fetch_done"`
	FetchTotal      int             `json:"fetch_total" db:"fetch_total"`
	FetchError      string          `json:"fetch_error,omitempty" db:"fetch_error"`
	FetchSite       string          `json:"fetch_site,omitempty" db:"fetch_site"`
	FetchBookID     string          `json:"fetch_book_id,omitempty" db:"fetch_book_id"`
	FetchChapterIDs json.RawMessage `json:"fetch_chapter_ids,omitempty" db:"fetch_chapter_ids"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
}

// ReferenceChapter stores a single downloaded chapter of a reference book.
type ReferenceChapter struct {
	ID        string    `json:"id" db:"id"`
	RefID     string    `json:"ref_id" db:"ref_id"`
	ChapterNo int       `json:"chapter_no" db:"chapter_no"`
	ChapterID string    `json:"chapter_id" db:"chapter_id"`
	Title     string    `json:"title" db:"title"`
	Content   string    `json:"content,omitempty" db:"content"`
	WordCount int       `json:"word_count" db:"word_count"`
	IsDeleted bool      `json:"is_deleted" db:"is_deleted"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ReferenceExportBundle is the portable format for single/batch export.
type ReferenceExportBundle struct {
	Version    int                   `json:"version"`
	ExportedAt time.Time             `json:"exported_at"`
	References []ReferenceExportItem `json:"references"`
}

type ReferenceExportItem struct {
	Material ReferenceMaterial  `json:"material"`
	Chapters []ReferenceChapter `json:"chapters"`
}

// ── Quality Analysis Types ────────────────────────────────────────────────────

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
