package models

import (
	"encoding/json"
	"time"
)

// ── Multi-Agent Review Types ──────────────────────────────────────────────────

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

// ── LangGraph Agent Session Types ────────────────────────────────────────────

type AgentRunRequest struct {
	TaskType     string                 `json:"task_type"` // generate_chapter | review | world_build
	UserPrompt   string                 `json:"user_prompt"`
	ChapterNum   *int                   `json:"chapter_num,omitempty"`
	OutlineHint  string                 `json:"outline_hint,omitempty"`
	StyleProfile map[string]interface{} `json:"style_profile,omitempty"`
	LLMConfig    map[string]interface{} `json:"llm_config,omitempty"`
	MaxRetries   int                    `json:"max_retries,omitempty"`
}

// BatchAgentRunRequest drives POST /agent/batch-run on the Python sidecar.
// Chapters are generated sequentially so that memory from each chapter
// feeds into the next (RecurrentGPT continuity requirement).
type BatchAgentRunRequest struct {
	ChapterNums  []int                  `json:"chapter_nums"`  // ordered list of chapter numbers
	OutlineHints map[string]string      `json:"outline_hints"` // str(chapter_num) -> hint
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

// ── Knowledge Graph (Neo4j) Types ─────────────────────────────────────────────

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

// ── Vector Store (Qdrant) Types ───────────────────────────────────────────────

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
	Items []map[string]interface{} `json:"items"`
}

// ── Per-Agent Model Routing ───────────────────────────────────────────────────

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

// ── Multi-Dimension Audit Types ───────────────────────────────────────────────

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
	LLMProfileID string `json:"llm_profile_id"`
}

type AuditReviseRequest struct {
	LLMProfileID string `json:"llm_profile_id"`
	MaxRounds    int    `json:"max_rounds"`
	Intensity    string `json:"intensity"`
}

// ── Book Rules ────────────────────────────────────────────────────────────────

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

// ── Creative Brief ────────────────────────────────────────────────────────────

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

// ── Anti-AI Rewrite ───────────────────────────────────────────────────────────

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

// ── Genre Templates ───────────────────────────────────────────────────────────

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
