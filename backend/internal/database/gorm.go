package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/novelbuilder/backend/internal/config"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type ProjectSchema struct {
	ID                       string  `gorm:"type:uuid;primaryKey"`
	Title                    string  `gorm:"type:varchar(300);not null"`
	Genre                    string  `gorm:"type:varchar(50)"`
	Description              string  `gorm:"type:text"`
	StyleDescription         string  `gorm:"type:text"`
	Language                 string  `gorm:"type:varchar(20);not null;default:zh-CN"`
	TargetWords              int     `gorm:"not null;default:500000"`
	ChapterWords             int     `gorm:"not null;default:3000"`
	Status                   string  `gorm:"type:varchar(20);default:active"`
	ProjectType              string  `gorm:"type:varchar(20);not null;default:original"`
	ContinuationRefID        *string `gorm:"type:uuid"`
	ContinuationStartChapter int     `gorm:"not null;default:1"`
	FanficMode               *string `gorm:"type:varchar(20)"`
	FanficSourceText         string  `gorm:"type:text"`
	AutoWriteEnabled         bool    `gorm:"not null;default:false"`
	AutoWriteInterval        int     `gorm:"not null;default:60"`
	CreatedAt                *time.Time
	UpdatedAt                *time.Time
}

func (ProjectSchema) TableName() string { return "projects" }

type ReferenceMaterialSchema struct {
	ID                string  `gorm:"type:uuid;primaryKey"`
	ProjectID         *string `gorm:"type:uuid;index"`
	Title             string  `gorm:"type:varchar(200)"`
	Author            string  `gorm:"type:varchar(100)"`
	Genre             string  `gorm:"type:varchar(50)"`
	FilePath          string  `gorm:"type:varchar(500)"`
	SourceURL         string  `gorm:"type:varchar(2000)"`
	FetchStatus       string  `gorm:"type:varchar(20);default:none"`
	FetchDone         int     `gorm:"default:0"`
	FetchTotal        int     `gorm:"default:0"`
	FetchError        string  `gorm:"type:text"`
	FetchSite         string  `gorm:"type:varchar(100)"`
	FetchBookID       string  `gorm:"type:varchar(200)"`
	FetchChapterIDs   JSONB   `gorm:"type:jsonb;default:'[]'"`
	StyleLayer        JSONB   `gorm:"type:jsonb"`
	NarrativeLayer    JSONB   `gorm:"type:jsonb"`
	AtmosphereLayer   JSONB   `gorm:"type:jsonb"`
	MigrationConfig   JSONB   `gorm:"type:jsonb"`
	StyleCollection   string  `gorm:"type:varchar(100)"`
	VectorFingerprint string  `gorm:"type:vector(1024)"`
	SampleTexts       JSONB   `gorm:"type:jsonb"`
	Status            string  `gorm:"type:varchar(20);default:processing"`
	AnalysisJobID     *string `gorm:"type:uuid"`
	CreatedAt         *time.Time
}

func (ReferenceMaterialSchema) TableName() string { return "reference_materials" }

type WorldBibleSchema struct {
	ID              string  `gorm:"type:uuid;primaryKey"`
	ProjectID       *string `gorm:"type:uuid;uniqueIndex"`
	Content         JSONB   `gorm:"type:jsonb;not null;default:'{}'"`
	MigrationSource *string `gorm:"type:uuid"`
	Version         int     `gorm:"default:1"`
	CreatedAt       *time.Time
	UpdatedAt       *time.Time
}

func (WorldBibleSchema) TableName() string { return "world_bibles" }

type WorldBibleConstitutionSchema struct {
	ID               string  `gorm:"type:uuid;primaryKey"`
	ProjectID        *string `gorm:"type:uuid;uniqueIndex"`
	ImmutableRules   JSONB   `gorm:"type:jsonb;not null;default:'[]'"`
	MutableRules     JSONB   `gorm:"type:jsonb;not null;default:'[]'"`
	ForbiddenAnchors JSONB   `gorm:"type:jsonb;not null;default:'[]'"`
	Version          int     `gorm:"default:1"`
	CreatedAt        *time.Time
	UpdatedAt        *time.Time
}

func (WorldBibleConstitutionSchema) TableName() string { return "world_bible_constitutions" }

type CharacterSchema struct {
	ID              string  `gorm:"type:uuid;primaryKey"`
	ProjectID       *string `gorm:"type:uuid;uniqueIndex:uq_characters_project_name;index"`
	Name            string  `gorm:"type:varchar(100);not null;uniqueIndex:uq_characters_project_name"`
	RoleType        string  `gorm:"type:varchar(30);default:supporting"`
	Profile         JSONB   `gorm:"type:jsonb;not null;default:'{}'"`
	CurrentState    JSONB   `gorm:"type:jsonb;default:'{}'"`
	VoiceCollection string  `gorm:"type:varchar(100)"`
	CreatedAt       *time.Time
	UpdatedAt       *time.Time
}

func (CharacterSchema) TableName() string { return "characters" }

type OutlineSchema struct {
	ID            string  `gorm:"type:uuid;primaryKey"`
	ProjectID     *string `gorm:"type:uuid;index:idx_outlines_project_level_parent_order"`
	Level         string  `gorm:"type:varchar(20);not null;index:idx_outlines_project_level_parent_order"`
	ParentID      *string `gorm:"type:uuid;index:idx_outlines_project_level_parent_order"`
	OrderNum      int     `gorm:"not null;default:0;index:idx_outlines_project_level_parent_order"`
	Title         string  `gorm:"type:varchar(300)"`
	Content       JSONB   `gorm:"type:jsonb;not null;default:'{}'"`
	TensionTarget float64 `gorm:"default:0.5"`
	CreatedAt     *time.Time
	UpdatedAt     *time.Time
}

func (OutlineSchema) TableName() string { return "outlines" }

type ForeshadowingSchema struct {
	ID                    string    `gorm:"type:uuid;primaryKey"`
	ProjectID             *string   `gorm:"type:uuid;index"`
	Content               string    `gorm:"type:text;not null"`
	EmbedChapterID        *string   `gorm:"type:uuid"`
	ResolveChapterID      *string   `gorm:"type:uuid"`
	EmbedMethod           string    `gorm:"type:varchar(100)"`
	ResolveMethod         string    `gorm:"type:varchar(100)"`
	PlannedEmbedChapter   int       `gorm:"default:0"`
	PlannedResolveChapter int       `gorm:"default:0"`
	Priority              int16     `gorm:"default:3"`
	Status                string    `gorm:"type:varchar(20);default:planned"`
	Tags                  TextArray `gorm:"type:text[]"`
	Origin                string    `gorm:"type:varchar(30);not null;default:manual"`
	CrossVolume           bool      `gorm:"not null;default:false"`
	CreatedAt             *time.Time
	UpdatedAt             *time.Time
}

func (ForeshadowingSchema) TableName() string { return "foreshadowings" }

type BookBlueprintSchema struct {
	ID             string  `gorm:"type:uuid;primaryKey"`
	ProjectID      *string `gorm:"type:uuid;uniqueIndex"`
	WorldBibleRef  *string `gorm:"type:uuid"`
	MasterOutline  JSONB   `gorm:"type:jsonb;not null;default:'{}'"`
	RelationGraph  JSONB   `gorm:"type:jsonb;not null;default:'{}'"`
	GlobalTimeline JSONB   `gorm:"type:jsonb;not null;default:'[]'"`
	Status         string  `gorm:"type:varchar(20);default:draft"`
	Version        int     `gorm:"default:1"`
	ReviewComment  string  `gorm:"type:text"`
	ErrorMessage   string  `gorm:"type:text"`
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
}

func (BookBlueprintSchema) TableName() string { return "book_blueprints" }

type VolumeSchema struct {
	ID            string  `gorm:"type:uuid;primaryKey"`
	ProjectID     *string `gorm:"type:uuid;uniqueIndex:uq_volumes_project_volume;index"`
	VolumeNum     int     `gorm:"not null;uniqueIndex:uq_volumes_project_volume"`
	Title         string  `gorm:"type:varchar(200)"`
	BlueprintID   *string `gorm:"type:uuid"`
	Status        string  `gorm:"type:varchar(20);default:draft"`
	ChapterStart  int
	ChapterEnd    int
	ReviewComment string `gorm:"type:text"`
	CreatedAt     *time.Time
	UpdatedAt     *time.Time
}

func (VolumeSchema) TableName() string { return "volumes" }

type ChapterSchema struct {
	ID                   string  `gorm:"type:uuid;primaryKey"`
	ProjectID            *string `gorm:"type:uuid;uniqueIndex:uq_chapters_project_chapter;index"`
	VolumeID             *string `gorm:"type:uuid"`
	ChapterNum           int     `gorm:"not null;uniqueIndex:uq_chapters_project_chapter"`
	Title                string  `gorm:"type:varchar(200)"`
	Content              string  `gorm:"type:text;default:''"`
	WordCount            int     `gorm:"default:0"`
	Summary              string  `gorm:"type:text;default:''"`
	GenParams            JSONB   `gorm:"type:jsonb;default:'{}'"`
	QualityReport        JSONB   `gorm:"type:jsonb;default:'{}'"`
	OriginalityScore     float64 `gorm:"default:0"`
	InputTokens          int     `gorm:"not null;default:0"`
	OutputTokens         int     `gorm:"not null;default:0"`
	GenreComplianceScore float64 `gorm:"not null;default:1.0"`
	GenreViolations      JSONB   `gorm:"type:jsonb;not null;default:'[]'"`
	Status               string  `gorm:"type:varchar(20);default:draft"`
	Version              int     `gorm:"default:1"`
	ReviewComment        string  `gorm:"type:text"`
	CreatedAt            *time.Time
	UpdatedAt            *time.Time
}

func (ChapterSchema) TableName() string { return "chapters" }

type ChapterSnapshotSchema struct {
	ID               string  `gorm:"type:uuid;primaryKey"`
	ChapterID        string  `gorm:"type:uuid;not null;index"`
	Version          int     `gorm:"not null"`
	Title            string  `gorm:"type:varchar(200)"`
	Content          string  `gorm:"type:text;not null"`
	WordCount        int     `gorm:"default:0"`
	Summary          string  `gorm:"type:text;default:''"`
	QualityReport    JSONB   `gorm:"type:jsonb;default:'{}'"`
	OriginalityScore float64 `gorm:"default:0"`
	Source           string  `gorm:"type:varchar(40);not null;default:manual"`
	Note             string  `gorm:"type:text;default:''"`
	CreatedAt        *time.Time
}

func (ChapterSnapshotSchema) TableName() string { return "chapter_snapshots" }

type WorkflowRunSchema struct {
	ID           string  `gorm:"type:uuid;primaryKey"`
	ProjectID    *string `gorm:"type:uuid;index"`
	StrictReview bool    `gorm:"default:true"`
	CurrentStep  string  `gorm:"type:varchar(50);not null;default:init"`
	Status       string  `gorm:"type:varchar(20);default:running"`
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

func (WorkflowRunSchema) TableName() string { return "workflow_runs" }

type WorkflowStepSchema struct {
	ID            string  `gorm:"type:uuid;primaryKey"`
	RunID         *string `gorm:"type:uuid;index"`
	StepKey       string  `gorm:"type:varchar(50);not null"`
	StepOrder     int     `gorm:"not null"`
	GateLevel     string  `gorm:"type:varchar(20);not null"`
	Status        string  `gorm:"type:varchar(20);not null;default:pending;index"`
	OutputRef     *string `gorm:"type:uuid"`
	SnapshotRef   *string `gorm:"type:uuid"`
	ReviewComment string  `gorm:"type:text"`
	Version       int     `gorm:"default:1"`
	GeneratedAt   *time.Time
	ReviewedAt    *time.Time
	CreatedAt     *time.Time
}

func (WorkflowStepSchema) TableName() string { return "workflow_steps" }

type WorkflowReviewSchema struct {
	ID            string  `gorm:"type:uuid;primaryKey"`
	StepID        *string `gorm:"type:uuid;index"`
	Action        string  `gorm:"type:varchar(20);not null"`
	Operator      string  `gorm:"type:varchar(20);default:admin"`
	Reason        string  `gorm:"type:text"`
	FromStepOrder int
	ToStepOrder   int
	CreatedAt     *time.Time
}

func (WorkflowReviewSchema) TableName() string { return "workflow_reviews" }

type WorkflowSnapshotSchema struct {
	ID             string  `gorm:"type:uuid;primaryKey"`
	RunID          *string `gorm:"type:uuid;index"`
	StepKey        string  `gorm:"type:varchar(50);not null"`
	Params         JSONB   `gorm:"type:jsonb;not null;default:'{}'"`
	ContextPayload JSONB   `gorm:"type:jsonb"`
	OutputPayload  JSONB   `gorm:"type:jsonb"`
	QualityPayload JSONB   `gorm:"type:jsonb"`
	CreatedAt      *time.Time
}

func (WorkflowSnapshotSchema) TableName() string { return "workflow_snapshots" }

type IdempotencyKeySchema struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	IdempotencyKey string `gorm:"type:varchar(128);not null;uniqueIndex:uq_idempotency_action"`
	Action         string `gorm:"type:varchar(200);not null;uniqueIndex:uq_idempotency_action"`
	RequestHash    string `gorm:"type:varchar(128)"`
	StatusCode     int
	ResponseBody   JSONB `gorm:"type:jsonb"`
	CreatedAt      *time.Time
}

func (IdempotencyKeySchema) TableName() string { return "idempotency_keys" }

type PlotGraphSnapshotSchema struct {
	ID        string  `gorm:"type:uuid;primaryKey"`
	ProjectID *string `gorm:"type:uuid;index"`
	ChapterID *string `gorm:"type:uuid"`
	GraphType string  `gorm:"type:varchar(20);not null"`
	Nodes     JSONB   `gorm:"type:jsonb;not null;default:'[]'"`
	Edges     JSONB   `gorm:"type:jsonb;not null;default:'[]'"`
	CreatedAt *time.Time
}

func (PlotGraphSnapshotSchema) TableName() string { return "plot_graph_snapshots" }

type OriginalityAuditSchema struct {
	ID                 string  `gorm:"type:uuid;primaryKey"`
	ChapterID          *string `gorm:"type:uuid;index"`
	SemanticSimilarity float64 `gorm:"default:0"`
	EventGraphDistance float64 `gorm:"default:0"`
	RoleOverlap        float64 `gorm:"default:0"`
	SuspiciousSegments JSONB   `gorm:"type:jsonb;default:'[]'"`
	Pass               bool    `gorm:"default:false"`
	CreatedAt          *time.Time
}

func (OriginalityAuditSchema) TableName() string { return "originality_audits" }

type VectorStoreSchema struct {
	ID         string  `gorm:"type:uuid;primaryKey"`
	ProjectID  *string `gorm:"type:uuid;index"`
	Collection string  `gorm:"type:varchar(100);not null;index"`
	Content    string  `gorm:"type:text;not null"`
	Metadata   JSONB   `gorm:"type:jsonb;default:'{}'"`
	Embedding  string  `gorm:"type:vector(1024)"`
	SourceType string  `gorm:"type:varchar(50);not null;default:reference"`
	SourceID   string  `gorm:"type:varchar(100);index"`
	CreatedAt  *time.Time
}

func (VectorStoreSchema) TableName() string { return "vector_store" }

type ContentDependencySchema struct {
	ID            string `gorm:"type:uuid;primaryKey"`
	ProjectID     string `gorm:"type:uuid;not null;index"`
	DependentType string `gorm:"type:varchar(30);not null;uniqueIndex:uq_content_dep"`
	DependentID   string `gorm:"type:uuid;not null;uniqueIndex:uq_content_dep;index"`
	SourceType    string `gorm:"type:varchar(30);not null;uniqueIndex:uq_content_dep;index"`
	SourceID      string `gorm:"type:uuid;not null;uniqueIndex:uq_content_dep;index"`
	CreatedAt     *time.Time
}

func (ContentDependencySchema) TableName() string { return "content_dependencies" }

type ChangeEventSchema struct {
	ID            string `gorm:"type:uuid;primaryKey"`
	ProjectID     string `gorm:"type:uuid;not null;index"`
	EntityType    string `gorm:"type:varchar(30);not null"`
	EntityID      string `gorm:"type:uuid;not null"`
	ChangeSummary string `gorm:"type:text;not null;default:''"`
	OldSnapshot   JSONB  `gorm:"type:jsonb"`
	NewSnapshot   JSONB  `gorm:"type:jsonb"`
	Status        string `gorm:"type:varchar(20);not null;default:pending"`
	CreatedAt     *time.Time
	UpdatedAt     *time.Time
}

func (ChangeEventSchema) TableName() string { return "change_events" }

type PatchPlanSchema struct {
	ID            string `gorm:"type:uuid;primaryKey"`
	ChangeEventID string `gorm:"type:uuid;not null;index"`
	ProjectID     string `gorm:"type:uuid;not null;index"`
	ImpactSummary string `gorm:"type:text;not null;default:''"`
	TotalItems    int    `gorm:"not null;default:0"`
	DoneItems     int    `gorm:"not null;default:0"`
	Status        string `gorm:"type:varchar(20);not null;default:ready"`
	CreatedAt     *time.Time
	UpdatedAt     *time.Time
}

func (PatchPlanSchema) TableName() string { return "patch_plans" }

type PatchItemSchema struct {
	ID                string `gorm:"type:uuid;primaryKey"`
	PlanID            string `gorm:"type:uuid;not null;index:idx_patch_items_plan"`
	ItemType          string `gorm:"type:varchar(30);not null"`
	ItemID            string `gorm:"type:uuid;not null"`
	ItemOrder         int    `gorm:"not null;default:0;index:idx_patch_items_plan"`
	ImpactDescription string `gorm:"type:text;not null;default:''"`
	PatchInstruction  string `gorm:"type:text;not null;default:''"`
	Status            string `gorm:"type:varchar(20);not null;default:pending"`
	ResultSnapshot    JSONB  `gorm:"type:jsonb"`
	CreatedAt         *time.Time
	UpdatedAt         *time.Time
}

func (PatchItemSchema) TableName() string { return "patch_items" }

type TaskQueueSchema struct {
	ID           string     `gorm:"type:uuid;primaryKey"`
	ProjectID    *string    `gorm:"type:uuid;index"`
	TaskType     string     `gorm:"type:varchar(100);not null"`
	Payload      JSONB      `gorm:"type:jsonb;not null;default:'{}'"`
	Status       string     `gorm:"type:varchar(20);not null;default:pending;index"`
	Priority     int        `gorm:"not null;default:5;index"`
	Attempts     int        `gorm:"not null;default:0"`
	MaxAttempts  int        `gorm:"not null;default:3"`
	ErrorMessage string     `gorm:"type:text;not null;default:''"`
	ScheduledAt  *time.Time `gorm:"index"`
	StartedAt    *time.Time
	CompletedAt  *time.Time
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

func (TaskQueueSchema) TableName() string { return "task_queue" }

type AgentReviewSessionSchema struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	ProjectID   string `gorm:"type:uuid;not null;index"`
	ReviewScope string `gorm:"type:varchar(50);not null;default:full"`
	TargetID    string `gorm:"type:varchar(255)"`
	Status      string `gorm:"type:varchar(50);not null;default:running;index"`
	Rounds      int    `gorm:"not null;default:3"`
	Consensus   string `gorm:"type:text;not null;default:''"`
	Issues      JSONB  `gorm:"type:jsonb;not null;default:'[]'"`
	CreatedAt   *time.Time
	CompletedAt *time.Time
}

func (AgentReviewSessionSchema) TableName() string { return "agent_review_sessions" }

type AgentReviewMessageSchema struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	SessionID string `gorm:"type:uuid;not null;index"`
	Round     int    `gorm:"not null"`
	AgentRole string `gorm:"type:varchar(100);not null"`
	AgentName string `gorm:"type:varchar(100);not null"`
	Content   string `gorm:"type:text;not null"`
	Tags      JSONB  `gorm:"type:jsonb;not null;default:'[]'"`
	CreatedAt *time.Time
}

func (AgentReviewMessageSchema) TableName() string { return "agent_review_messages" }

type LLMProfileSchema struct {
	ID              string  `gorm:"type:uuid;primaryKey"`
	Name            string  `gorm:"type:varchar(100);not null;uniqueIndex"`
	Provider        string  `gorm:"type:varchar(50);not null;default:openai"`
	BaseURL         string  `gorm:"type:varchar(500);not null;default:https://api.openai.com/v1"`
	APIKey          string  `gorm:"type:text;not null"`
	ModelName       string  `gorm:"type:varchar(200);not null"`
	MaxTokens       int     `gorm:"not null;default:8192"`
	Temperature     float64 `gorm:"not null;default:0.7"`
	IsDefault       bool    `gorm:"not null;default:false;index"`
	RPMLimit        int     `gorm:"not null;default:0"`
	OmitMaxTokens   bool    `gorm:"not null;default:false"`
	OmitTemperature bool    `gorm:"not null;default:false"`
	APIStyle        string  `gorm:"type:varchar(50);not null;default:chat_completions"`
	CreatedAt       *time.Time
	UpdatedAt       *time.Time
}

func (LLMProfileSchema) TableName() string { return "llm_profiles" }

type AgentModelRouteSchema struct {
	ID           string  `gorm:"type:uuid;primaryKey"`
	AgentType    string  `gorm:"type:varchar(50);not null;index"`
	LLMProfileID *string `gorm:"type:uuid"`
	ProjectID    *string `gorm:"type:uuid;index"`
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

func (AgentModelRouteSchema) TableName() string { return "agent_model_routes" }

type AgentSessionSchema struct {
	ID           string  `gorm:"type:uuid;primaryKey"`
	ProjectID    *string `gorm:"type:uuid;index"`
	TaskType     string  `gorm:"type:varchar(50);not null;default:generate_chapter"`
	Status       string  `gorm:"type:varchar(20);not null;default:running;index"`
	SessionKey   string  `gorm:"type:varchar(200)"`
	InputParams  JSONB   `gorm:"type:jsonb;default:'{}'"`
	Result       JSONB   `gorm:"type:jsonb"`
	ErrorMsg     string  `gorm:"type:text"`
	QualityScore float64
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

func (AgentSessionSchema) TableName() string { return "agent_sessions" }

type PromptPresetSchema struct {
	ID          string  `gorm:"type:uuid;primaryKey"`
	ProjectID   *string `gorm:"type:uuid;index"`
	Name        string  `gorm:"type:varchar(200);not null"`
	Description string  `gorm:"type:text;not null;default:''"`
	Category    string  `gorm:"type:varchar(50);not null;default:general;index"`
	Content     string  `gorm:"type:text;not null"`
	Variables   JSONB   `gorm:"type:jsonb;not null;default:'[]'"`
	IsGlobal    bool    `gorm:"not null;default:false;index"`
	SortOrder   int     `gorm:"not null;default:0"`
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
}

func (PromptPresetSchema) TableName() string { return "prompt_presets" }

type GlossaryTermSchema struct {
	ID         string `gorm:"type:uuid;primaryKey"`
	ProjectID  string `gorm:"type:uuid;not null;uniqueIndex:uq_glossary_term_project;index"`
	Term       string `gorm:"type:varchar(200);not null;uniqueIndex:uq_glossary_term_project"`
	Definition string `gorm:"type:text;not null;default:''"`
	Aliases    JSONB  `gorm:"type:jsonb;not null;default:'[]'"`
	Category   string `gorm:"type:varchar(50);not null;default:general;index"`
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
}

func (GlossaryTermSchema) TableName() string { return "glossary_terms" }

type BookRuleSchema struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	ProjectID      string `gorm:"type:uuid;not null;uniqueIndex"`
	RulesContent   string `gorm:"type:text;not null;default:''"`
	StyleGuide     string `gorm:"type:text;not null;default:''"`
	AntiAIWordlist JSONB  `gorm:"type:jsonb;not null;default:'[]'"`
	BannedPatterns JSONB  `gorm:"type:jsonb;not null;default:'[]'"`
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
}

func (BookRuleSchema) TableName() string { return "book_rules" }

type ChapterImportSchema struct {
	ID                string  `gorm:"type:uuid;primaryKey"`
	ProjectID         string  `gorm:"type:uuid;not null;index"`
	SourceText        string  `gorm:"type:text;not null"`
	SplitPattern      string  `gorm:"type:varchar(200);not null;default:第.{1,4}[章节回]"`
	FanficMode        *string `gorm:"type:varchar(20)"`
	Status            string  `gorm:"type:varchar(20);not null;default:pending"`
	TotalChapters     int     `gorm:"not null;default:0"`
	ProcessedChapters int     `gorm:"not null;default:0"`
	ErrorMessage      string  `gorm:"type:text;not null;default:''"`
	ReverseEngineered JSONB   `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt         *time.Time
	UpdatedAt         *time.Time
}

func (ChapterImportSchema) TableName() string { return "chapter_imports" }

type StoryResourceSchema struct {
	ID          string  `gorm:"type:uuid;primaryKey"`
	ProjectID   string  `gorm:"type:uuid;not null;index"`
	Name        string  `gorm:"type:varchar(200);not null"`
	Category    string  `gorm:"type:varchar(50);not null;default:item"`
	Quantity    float64 `gorm:"type:numeric(15,2);not null;default:0"`
	Unit        string  `gorm:"type:varchar(50);not null;default:''"`
	Description string  `gorm:"type:text;not null;default:''"`
	Holder      string  `gorm:"type:varchar(200);not null;default:''"`
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
}

func (StoryResourceSchema) TableName() string { return "story_resources" }

type StoryResourceChangeSchema struct {
	ID         string  `gorm:"type:uuid;primaryKey"`
	ResourceID string  `gorm:"type:uuid;not null;index"`
	ChapterID  *string `gorm:"type:uuid;index"`
	Delta      float64 `gorm:"type:numeric(15,2);not null;default:0"`
	Reason     string  `gorm:"type:text;not null;default:''"`
	CreatedAt  *time.Time
}

func (StoryResourceChangeSchema) TableName() string { return "story_resource_changes" }

type SystemSettingSchema struct {
	Key       string `gorm:"type:varchar(100);primaryKey"`
	Value     string `gorm:"type:text;not null;default:''"`
	UpdatedAt *time.Time
}

func (SystemSettingSchema) TableName() string { return "system_settings" }

type NotificationWebhookSchema struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	ProjectID string `gorm:"type:uuid;not null;index"`
	URL       string `gorm:"type:varchar(500);not null"`
	Secret    string `gorm:"type:varchar(200);not null;default:''"`
	Events    JSONB  `gorm:"type:jsonb;not null;default:'[\"chapter_generated\",\"quality_failed\",\"workflow_step\"]'"`
	IsActive  bool   `gorm:"not null;default:true"`
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

func (NotificationWebhookSchema) TableName() string { return "notification_webhooks" }

type ChapterSummarySchema struct {
	ID         string  `gorm:"type:uuid;primaryKey"`
	ProjectID  *string `gorm:"type:uuid;index"`
	ChapterID  *string `gorm:"type:uuid;uniqueIndex"`
	ChapterNum int     `gorm:"not null;index"`
	Summary    string  `gorm:"type:text;not null;default:''"`
	QdrantSync bool    `gorm:"not null;default:false;index"`
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
}

func (ChapterSummarySchema) TableName() string { return "chapter_summaries" }

type GraphSyncLogSchema struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	ProjectID   string `gorm:"type:uuid;not null;index"`
	EntityType  string `gorm:"type:varchar(50);not null;uniqueIndex:uq_graph_sync_log"`
	EntityID    string `gorm:"type:uuid;not null;uniqueIndex:uq_graph_sync_log"`
	SyncedAt    *time.Time
	Neo4jNodeID string `gorm:"type:varchar(200)"`
}

func (GraphSyncLogSchema) TableName() string { return "graph_sync_log" }

type AuditReportSchema struct {
	ID            string  `gorm:"type:uuid;primaryKey"`
	ChapterID     string  `gorm:"type:uuid;not null;index"`
	ProjectID     string  `gorm:"type:uuid;not null;index"`
	Dimensions    JSONB   `gorm:"type:jsonb;not null;default:'{}'"`
	OverallScore  float64 `gorm:"not null;default:0"`
	Passed        bool    `gorm:"not null;default:false"`
	AIProbability float64 `gorm:"not null;default:0"`
	Issues        JSONB   `gorm:"type:jsonb;not null;default:'[]'"`
	RevisionCount int     `gorm:"not null;default:0"`
	CreatedAt     *time.Time
}

func (AuditReportSchema) TableName() string { return "audit_reports" }

type ReferenceBookChapterSchema struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	RefID     string `gorm:"type:uuid;not null;index"`
	ChapterNo int    `gorm:"not null;index"`
	ChapterID string `gorm:"type:varchar(200);not null;default:''"`
	Title     string `gorm:"type:varchar(500);not null;default:''"`
	Content   string `gorm:"type:text;not null;default:''"`
	WordCount int    `gorm:"not null;default:0"`
	IsDeleted bool   `gorm:"not null;default:false;index"`
	CreatedAt *time.Time
}

func (ReferenceBookChapterSchema) TableName() string { return "reference_book_chapters" }

type ReferenceAnalysisJobSchema struct {
	ID                      string `gorm:"type:uuid;primaryKey"`
	RefID                   string `gorm:"type:uuid;not null;index"`
	ProjectID               string `gorm:"type:uuid;not null;index"`
	Status                  string `gorm:"type:text;not null;default:pending;index"`
	TotalChunks             int    `gorm:"not null;default:0"`
	DoneChunks              int    `gorm:"not null;default:0"`
	ErrorMessage            string `gorm:"type:text"`
	ExtractedCharacters     JSONB  `gorm:"type:jsonb"`
	ExtractedWorld          JSONB  `gorm:"type:jsonb"`
	ExtractedOutline        JSONB  `gorm:"type:jsonb"`
	ChunkResults            JSONB  `gorm:"type:jsonb;not null;default:'[]'"`
	ExtractedGlossary       JSONB  `gorm:"type:jsonb"`
	ExtractedForeshadowings JSONB  `gorm:"type:jsonb"`
	CreatedAt               *time.Time
	UpdatedAt               *time.Time
}

func (ReferenceAnalysisJobSchema) TableName() string { return "reference_analysis_jobs" }

type SubplotSchema struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	ProjectID      string `gorm:"type:uuid;not null;index"`
	Title          string `gorm:"type:varchar(200);not null"`
	LineLabel      string `gorm:"type:varchar(10);not null;default:A"`
	Description    string `gorm:"type:text;not null;default:''"`
	Status         string `gorm:"type:varchar(20);not null;default:active"`
	Priority       int    `gorm:"not null;default:3"`
	StartChapter   *int
	ResolveChapter *int
	Tags           TextArray `gorm:"type:text[];not null;default:'{}'"`
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
}

func (SubplotSchema) TableName() string { return "subplots" }

type SubplotCheckpointSchema struct {
	ID         string  `gorm:"type:uuid;primaryKey"`
	SubplotID  string  `gorm:"type:uuid;not null;index"`
	ChapterID  *string `gorm:"type:uuid"`
	ChapterNum *int    `gorm:"index"`
	Note       string  `gorm:"type:text;not null;default:''"`
	Progress   int     `gorm:"not null;default:0"`
	CreatedAt  *time.Time
}

func (SubplotCheckpointSchema) TableName() string { return "subplot_checkpoints" }

type EmotionalArcEntrySchema struct {
	ID          string  `gorm:"type:uuid;primaryKey"`
	ProjectID   string  `gorm:"type:uuid;not null;index"`
	CharacterID string  `gorm:"type:uuid;not null;uniqueIndex:uq_emotional_arc_char_chapter;index"`
	ChapterID   *string `gorm:"type:uuid"`
	ChapterNum  int     `gorm:"not null;uniqueIndex:uq_emotional_arc_char_chapter;index"`
	Emotion     string  `gorm:"type:varchar(50);not null;default:neutral"`
	Intensity   float64 `gorm:"not null;default:0.5"`
	Note        string  `gorm:"type:text;not null;default:''"`
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
}

func (EmotionalArcEntrySchema) TableName() string { return "emotional_arc_entries" }

type CharacterInteractionSchema struct {
	ID                  string `gorm:"type:uuid;primaryKey"`
	ProjectID           string `gorm:"type:uuid;not null;uniqueIndex:uq_character_interaction;index"`
	CharAID             string `gorm:"type:uuid;not null;uniqueIndex:uq_character_interaction;index"`
	CharBID             string `gorm:"type:uuid;not null;uniqueIndex:uq_character_interaction;index"`
	FirstMeetChapter    *int
	LastInteractChapter *int
	Relationship        string `gorm:"type:varchar(100);not null;default:acquaintance"`
	InfoKnownByA        JSONB  `gorm:"type:jsonb;not null;default:'[]'"`
	InfoKnownByB        JSONB  `gorm:"type:jsonb;not null;default:'[]'"`
	InteractionCount    int    `gorm:"not null;default:1"`
	Notes               string `gorm:"type:text;not null;default:''"`
	CreatedAt           *time.Time
	UpdatedAt           *time.Time
}

func (CharacterInteractionSchema) TableName() string { return "character_interactions" }

type RadarScanResultSchema struct {
	ID        string  `gorm:"type:uuid;primaryKey"`
	ProjectID *string `gorm:"type:uuid;index"`
	Genre     string  `gorm:"type:varchar(50);not null;default:''"`
	Platform  string  `gorm:"type:varchar(50);not null;default:general"`
	Result    JSONB   `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt *time.Time
}

func (RadarScanResultSchema) TableName() string { return "radar_scan_results" }

type GenreTemplateSchema struct {
	ID                   string `gorm:"type:uuid;primaryKey"`
	Genre                string `gorm:"type:text;not null;uniqueIndex"`
	RulesContent         string `gorm:"type:text;not null;default:''"`
	LanguageConstraints  string `gorm:"type:text;not null;default:''"`
	RhythmRules          string `gorm:"type:text;not null;default:''"`
	AuditDimensionsExtra JSONB  `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt            *time.Time
	UpdatedAt            *time.Time
}

func (GenreTemplateSchema) TableName() string { return "genre_templates" }

type VolumeArcSummarySchema struct {
	ID                string `gorm:"type:uuid;primaryKey"`
	ProjectID         string `gorm:"type:uuid;not null;uniqueIndex:uq_volume_arc_project_volume;index"`
	VolumeID          string `gorm:"type:uuid;not null;uniqueIndex:uq_volume_arc_project_volume"`
	Summary           string `gorm:"type:text;not null;default:''"`
	KeyEvents         string `gorm:"type:text;not null;default:''"`
	UnresolvedThreads string `gorm:"type:text;not null;default:''"`
	LastChapterNum    int    `gorm:"not null;default:0"`
	CreatedAt         *time.Time
	UpdatedAt         *time.Time
}

func (VolumeArcSummarySchema) TableName() string { return "volume_arc_summaries" }

type ChapterSimilarityLogSchema struct {
	ID               string  `gorm:"type:uuid;primaryKey"`
	ProjectID        string  `gorm:"type:uuid;not null;index"`
	ChapterID        string  `gorm:"type:uuid;not null;index"`
	SimilarChapterID string  `gorm:"type:uuid;not null"`
	SimilarityScore  float64 `gorm:"not null;default:0"`
	SimilarSegments  JSONB   `gorm:"type:jsonb;not null;default:'[]'"`
	CreatedAt        *time.Time
}

func (ChapterSimilarityLogSchema) TableName() string { return "chapter_similarity_log" }

type EntityProvenanceSchema struct {
	ID              string  `gorm:"type:uuid;primaryKey"`
	ProjectID       string  `gorm:"type:uuid;not null;uniqueIndex:uq_entity_provenance;index"`
	EntityType      string  `gorm:"type:varchar(30);not null;uniqueIndex:uq_entity_provenance"`
	EntityName      string  `gorm:"type:varchar(200);not null;uniqueIndex:uq_entity_provenance"`
	FirstChapterID  *string `gorm:"type:uuid"`
	FirstChapterNum *int
	SourceType      string `gorm:"type:varchar(30);not null;default:outlined"`
	SourceDetail    string `gorm:"type:text;not null;default:''"`
	IsJustified     bool   `gorm:"not null;default:true"`
	CreatedAt       *time.Time
}

func (EntityProvenanceSchema) TableName() string { return "entity_provenance" }

type FanqieAccountSchema struct {
	ID              string `gorm:"type:uuid;primaryKey"`
	ProjectID       string `gorm:"type:uuid;not null;uniqueIndex"`
	BookID          string `gorm:"type:text;not null;default:''"`
	BookTitle       string `gorm:"type:text;not null;default:''"`
	Cookies         string `gorm:"type:text;not null;default:''"`
	Status          string `gorm:"type:text;not null;default:unconfigured"`
	LastValidatedAt *time.Time
	CreatedAt       *time.Time
	UpdatedAt       *time.Time
}

func (FanqieAccountSchema) TableName() string { return "fanqie_accounts" }

func NewGORM(cfg config.DatabaseConfig, logger *zap.Logger) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch strings.ToLower(cfg.Driver) {
	case "", "postgres", "postgresql":
		dsn := fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
		)
		dialector = postgres.Open(dsn)
	case "sqlite", "sqlite3":
		if cfg.SQLitePath == "" {
			return nil, fmt.Errorf("SQLITE_PATH is required when DB_DRIVER=sqlite")
		}
		if err := os.MkdirAll(filepath.Dir(cfg.SQLitePath), 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite directory: %w", err)
		}
		dialector = sqlite.Open(cfg.SQLitePath)
	default:
		return nil, fmt.Errorf("unsupported DB_DRIVER %q", cfg.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open gorm database: %w", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}
	if logger != nil {
		logger.Info("GORM database opened", zap.String("driver", db.Dialector.Name()))
	}
	return db, nil
}

func AutoMigrate(ctx context.Context, db *gorm.DB, logger *zap.Logger) error {
	migrator := db.WithContext(ctx)
	if db.Dialector.Name() == "postgres" {
		if err := migrator.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
			return fmt.Errorf("create uuid extension: %w", err)
		}
		if err := migrator.Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
			return fmt.Errorf("create vector extension: %w", err)
		}
	}
	if db.Dialector.Name() == "postgres" {
		if err := migrator.Exec(`CREATE SCHEMA IF NOT EXISTS quarantine_zone`).Error; err != nil {
			return fmt.Errorf("create quarantine schema: %w", err)
		}
	}

	models := []interface{}{
		&ProjectSchema{}, &ReferenceMaterialSchema{},
		&WorldBibleSchema{}, &WorldBibleConstitutionSchema{}, &CharacterSchema{}, &OutlineSchema{},
		&ForeshadowingSchema{}, &BookBlueprintSchema{}, &VolumeSchema{}, &ChapterSchema{}, &ChapterSnapshotSchema{},
		&WorkflowRunSchema{}, &WorkflowStepSchema{}, &WorkflowReviewSchema{}, &WorkflowSnapshotSchema{}, &IdempotencyKeySchema{},
		&PlotGraphSnapshotSchema{}, &OriginalityAuditSchema{}, &VectorStoreSchema{},
		&ContentDependencySchema{}, &ChangeEventSchema{}, &PatchPlanSchema{}, &PatchItemSchema{}, &TaskQueueSchema{},
		&AgentReviewSessionSchema{}, &AgentReviewMessageSchema{}, &LLMProfileSchema{}, &AgentModelRouteSchema{}, &AgentSessionSchema{},
		&PromptPresetSchema{}, &GlossaryTermSchema{}, &BookRuleSchema{}, &ChapterImportSchema{},
		&StoryResourceSchema{}, &StoryResourceChangeSchema{}, &SystemSettingSchema{}, &NotificationWebhookSchema{},
		&ChapterSummarySchema{}, &GraphSyncLogSchema{}, &AuditReportSchema{},
		&ReferenceBookChapterSchema{}, &ReferenceAnalysisJobSchema{},
		&SubplotSchema{}, &SubplotCheckpointSchema{}, &EmotionalArcEntrySchema{}, &CharacterInteractionSchema{},
		&RadarScanResultSchema{}, &GenreTemplateSchema{},
		&VolumeArcSummarySchema{}, &ChapterSimilarityLogSchema{}, &EntityProvenanceSchema{},
		&FanqieAccountSchema{}, &FanqieUploadSchema{},
	}
	if db.Dialector.Name() == "postgres" {
		models = append(models, &PlotElementSchema{})
	} else if db.Dialector.Name() == "sqlite" {
		models = append(models, &PlotElementSQLiteSchema{})
	}
	if err := migrator.AutoMigrate(models...); err != nil {
		return fmt.Errorf("gorm automigrate: %w", err)
	}
	if err := ensurePostgresIndexes(ctx, db); err != nil {
		return err
	}
	if logger != nil {
		logger.Info("database schema auto-migrated", zap.String("driver", db.Dialector.Name()))
	}
	return nil
}

func ensurePostgresIndexes(ctx context.Context, db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_projects_continuation_ref ON projects(continuation_ref_id) WHERE continuation_ref_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_ref_book_chapters_ref_all ON reference_book_chapters(ref_id, chapter_no)`,
		`CREATE INDEX IF NOT EXISTS idx_task_queue_pending ON task_queue(priority DESC, scheduled_at ASC) WHERE status = 'pending'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_profiles_single_default ON llm_profiles(is_default) WHERE is_default = TRUE`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_route_project ON agent_model_routes(agent_type, project_id) WHERE project_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_route_global ON agent_model_routes(agent_type) WHERE project_id IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_chapter_summaries_nosync ON chapter_summaries(project_id) WHERE qdrant_sync = FALSE`,
		`CREATE INDEX IF NOT EXISTS idx_vector_store_source_id ON vector_store(project_id, source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chapter_snapshots_chapter_created ON chapter_snapshots(chapter_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_chapter_similarity_project ON chapter_similarity_log(project_id, chapter_id)`,
	}
	for _, stmt := range statements {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("ensure postgres index %q: %w", stmt, err)
		}
	}
	return nil
}
