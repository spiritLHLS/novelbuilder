package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/gateway"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// agentDef defines an agent's persona and focus area.
type agentDef struct {
	Role    models.AgentRole
	Name    string
	Emoji   string
	Persona string
}

// turn is a single entry in the debate conversation history.
type turn struct{ role, content string }

var agentRoster = []agentDef{
	{
		Role:  models.AgentOutlineCritic,
		Name:  "大纲批评家",
		Emoji: "📐",
		Persona: `你是一位专业的网文大纲批评家，专注于审查故事结构。
你的职责：
1. 检查三幕结构/起承转合是否清晰
2. 判断各卷节奏分配是否合理（起伏频率、爽点密度）
3. 识别大纲中逻辑跳跃、情节黑洞（事件发生无理由）
4. 评估主线与支线的交织是否有机
5. 指出可能让读者弃书的"节奏陷阱"
对话风格：严格、直接、以问题为导向，每条意见必须给出改进方案。`,
	},
	{
		Role:  models.AgentTimelineInspector,
		Name:  "时间线审核员",
		Emoji: "⏱️",
		Persona: `你是一位严苛的时间线审核员，专门追查叙事时序问题。
你的职责：
1. 梳理故事内时间轴，发现时间矛盾（A事件在B之前发生但逻辑上不可能）
2. 检查闪回/闪前是否清晰标记、是否必要
3. 验证角色年龄、成长周期与时间线的一致性
4. 检查世界内的历史事件时间是否自洽
5. 识别"时间橡皮泥"问题（为方便情节随意拉伸压缩时间）
对话风格：精确、追问细节，将时间问题具体到章/节/场景。`,
	},
	{
		Role:  models.AgentPlotCoherence,
		Name:  "剧情连贯性专家",
		Emoji: "🔗",
		Persona: `你是一位剧情连贯性专家，判断故事因果链是否完整。
你的职责：
1. 验证每个重要情节是否有充分的前因（无突兀转折）
2. 检查伏笔是否在合理范围内被回收（不能无限期搁置）
3. 识别"上帝视角漏洞"（读者/主角无法知晓的信息被当作行动依据）
4. 审查反派/对立势力的动机是否合理
5. 检查主角成长弧线与事件是否对应
对话风格：追溯因果，要求每个"为什么"都有答案，不接受"因为作者需要"的理由。`,
	},
	{
		Role:  models.AgentCharacterAnalyst,
		Name:  "角色设计分析师",
		Emoji: "👥",
		Persona: `你是一位资深角色设计分析师，专注于角色的深度与一致性。
你的职责：
1. 检查主要角色是否有清晰的核心欲望/恐惧/伤痛
2. 验证角色行为是否符合其性格设定（无OOC出戏）
3. 识别角色功能重叠（多个角色做相同的叙事功能）
4. 检查支线角色是否有自己的Agency（还是只是主角的工具）
5. 分析人际关系网络是否立体、有层次
对话风格：从心理动机出发，追问角色"为什么这么做"，而不仅是"做了什么"。`,
	},
	{
		Role:  models.AgentDevilsAdvocate,
		Name:  "魔鬼代言人",
		Emoji: "😈",
		Persona: `你是故事评审团的魔鬼代言人，专门提出其他人遗漏的尖锐质疑。
你的职责：
1. 以最挑剔的读者视角提出反驳：为什么读者不会买账？
2. 对其他智能体的意见提出反驳，防止达成过快的假共识
3. 指出情节中的隐藏问题：政治/文化敏感点、读者可能的反感点
4. 质疑"我们以为解决了"的问题是否真正解决
5. 提出"如果我是读者，我会在第X章弃书，因为..."
对话风格：说反话、提问题、永不满足，但每次质疑附带一个建设性替代方案。`,
	},
}

// AgentReviewService orchestrates multi-agent debate reviews.
type AgentReviewService struct {
	db     *pgxpool.Pool
	ai     *gateway.AIGateway
	logger *zap.Logger
}

func NewAgentReviewService(db *pgxpool.Pool, ai *gateway.AIGateway, logger *zap.Logger) *AgentReviewService {
	return &AgentReviewService{db: db, ai: ai, logger: logger}
}

// StreamReview runs the multi-agent debate and streams messages via the handler callback.
// Each call to handler receives a single AgentMessage (or a final summary message from the moderator).
func (s *AgentReviewService) StreamReview(
	ctx context.Context,
	projectID string,
	req models.AgentReviewRequest,
	onMessage func(msg models.AgentMessage),
) (*models.AgentReviewSession, error) {
	rounds := req.Rounds
	if rounds <= 0 {
		rounds = 3
	}

	sessionID := uuid.New().String()
	now := time.Now()

	session := &models.AgentReviewSession{
		ID:          sessionID,
		ProjectID:   projectID,
		ReviewScope: req.Scope,
		TargetID:    req.TargetID,
		Status:      "running",
		Rounds:      rounds,
		CreatedAt:   now,
	}

	// Persist session start
	if _, err := s.db.Exec(ctx,
		`INSERT INTO agent_review_sessions
		 (id, project_id, review_scope, target_id, status, rounds, consensus, created_at)
		 VALUES ($1,$2,$3,$4,'running',$5,'',$6)`,
		sessionID, projectID, req.Scope, req.TargetID, rounds, now); err != nil {
		return nil, fmt.Errorf("create review session: %w", err)
	}

	// Gather context
	briefing, err := s.buildBriefing(ctx, projectID, req)
	if err != nil {
		return nil, fmt.Errorf("build briefing: %w", err)
	}

	var allMessages []models.AgentMessage
	// conversationHistory tracks the debate so each agent sees prior turns
	history := []turn{}

	// ---- Debate rounds ----
	for round := 1; round <= rounds; round++ {
		for _, agent := range agentRoster {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			prompt := s.buildAgentPrompt(agent, round, rounds, briefing, history, allMessages)

			resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
				Task:      "agent_review",
				MaxTokens: 1200,
				Messages: []gateway.ChatMessage{
					{Role: "system", Content: agent.Persona},
					{Role: "user", Content: prompt},
				},
			})
			if err != nil {
				s.logger.Warn("agent response failed", zap.String("agent", string(agent.Role)), zap.Error(err))
				continue
			}

			content := strings.TrimSpace(resp.Content)
			tags := extractTags(content, round, rounds)

			msg := models.AgentMessage{
				Round:     round,
				Agent:     agent.Role,
				AgentName: agent.Emoji + " " + agent.Name,
				Content:   content,
				Tags:      tags,
			}
			allMessages = append(allMessages, msg)
			history = append(history, turn{
				role:    "assistant",
				content: fmt.Sprintf("[%s - 第%d轮] %s", agent.Name, round, content),
			})

			// Stream immediately; persist below via batch
			onMessage(msg)
		}
	}

	// ---- Moderator synthesis ----
	synthesisMsg, issues := s.runModerator(ctx, briefing, allMessages)
	allMessages = append(allMessages, synthesisMsg)
	onMessage(synthesisMsg)

	// Batch-insert all messages in one round-trip to avoid N+1
	msgBatch := &pgx.Batch{}
	for _, m := range allMessages {
		tagsJSON, _ := json.Marshal(m.Tags)
		agentRoleStr := string(m.Agent)
		agentRound := m.Round
		if agentRoleStr == "" {
			agentRoleStr = "moderator"
		}
		if agentRound == 0 {
			agentRound = rounds + 1
		}
		msgBatch.Queue(
			`INSERT INTO agent_review_messages (id, session_id, round, agent_role, agent_name, content, tags, created_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())`,
			uuid.New().String(), sessionID, agentRound, agentRoleStr, m.AgentName, m.Content, tagsJSON)
	}
	br := s.db.SendBatch(ctx, msgBatch)
	for range allMessages {
		br.Exec() //nolint:errcheck
	}
	br.Close()

	// Complete session
	nowCompleted := time.Now()
	session.Messages = allMessages
	session.Issues = issues
	session.Consensus = synthesisMsg.Content
	session.Status = "completed"
	session.CompletedAt = &nowCompleted

	issuesJSON, _ := json.Marshal(issues)
	s.db.Exec(ctx,
		`UPDATE agent_review_sessions
		 SET status='completed', consensus=$1, issues=$2, completed_at=$3
		 WHERE id=$4`,
		synthesisMsg.Content, issuesJSON, nowCompleted, sessionID)

	return session, nil
}

// GetSession retrieves a review session with its messages.
func (s *AgentReviewService) GetSession(ctx context.Context, sessionID string) (*models.AgentReviewSession, error) {
	var sess models.AgentReviewSession
	err := s.db.QueryRow(ctx,
		`SELECT id, project_id, review_scope, target_id, status, rounds, consensus, created_at, completed_at
		 FROM agent_review_sessions WHERE id = $1`, sessionID).Scan(
		&sess.ID, &sess.ProjectID, &sess.ReviewScope, &sess.TargetID,
		&sess.Status, &sess.Rounds, &sess.Consensus, &sess.CreatedAt, &sess.CompletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	rows, err := s.db.Query(ctx,
		`SELECT round, agent_role, agent_name, content, tags
		 FROM agent_review_messages WHERE session_id = $1 ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m models.AgentMessage
		var tagsJSON []byte
		if err := rows.Scan(&m.Round, &m.Agent, &m.AgentName, &m.Content, &tagsJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(tagsJSON, &m.Tags); err != nil {
			s.logger.Warn("GetSession: failed to unmarshal message tags",
				zap.String("session_id", sessionID), zap.Error(err))
		}
		sess.Messages = append(sess.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get session messages rows: %w", err)
	}
	return &sess, nil
}

// ListSessions lists review sessions for a project.
func (s *AgentReviewService) ListSessions(ctx context.Context, projectID string) ([]models.AgentReviewSession, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, review_scope, target_id, status, rounds, consensus, created_at, completed_at
		 FROM agent_review_sessions WHERE project_id = $1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []models.AgentReviewSession
	for rows.Next() {
		var sess models.AgentReviewSession
		if err := rows.Scan(&sess.ID, &sess.ProjectID, &sess.ReviewScope, &sess.TargetID,
			&sess.Status, &sess.Rounds, &sess.Consensus, &sess.CreatedAt, &sess.CompletedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions rows: %w", err)
	}
	return sessions, nil
}

// buildBriefing gathers all relevant project data for the agents.
func (s *AgentReviewService) buildBriefing(ctx context.Context, projectID string, req models.AgentReviewRequest) (string, error) {
	var sb strings.Builder

	sb.WriteString("# 项目资料摘要\n\n")

	// Project basic info
	var title, genre, description string
	s.db.QueryRow(ctx,
		`SELECT title, genre, description FROM projects WHERE id = $1`, projectID).
		Scan(&title, &genre, &description)
	sb.WriteString(fmt.Sprintf("**标题**: %s\n**类型**: %s\n**简介**: %s\n\n", title, genre, description))

	// World Bible
	var worldContent json.RawMessage
	if err := s.db.QueryRow(ctx,
		`SELECT content FROM world_bibles WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&worldContent); err == nil {
		sb.WriteString("## 世界观设定\n")
		sb.WriteString(string(worldContent))
		sb.WriteString("\n\n")
	}

	// Master Outline
	var masterOutline json.RawMessage
	if err := s.db.QueryRow(ctx,
		`SELECT master_outline FROM book_blueprints WHERE project_id = $1 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&masterOutline); err == nil {
		sb.WriteString("## 主线大纲\n")
		sb.WriteString(string(masterOutline))
		sb.WriteString("\n\n")
	}

	// Characters
	rows, _ := s.db.Query(ctx,
		`SELECT name, role_type, profile FROM characters WHERE project_id = $1`, projectID)
	if rows != nil {
		sb.WriteString("## 角色设定\n")
		for rows.Next() {
			var name, roleType string
			var profile json.RawMessage
			rows.Scan(&name, &roleType, &profile)
			sb.WriteString(fmt.Sprintf("- **%s**（%s）: %s\n", name, roleType, string(profile)))
		}
		rows.Close()
		sb.WriteString("\n")
	}

	// Outlines
	oRows, _ := s.db.Query(ctx,
		`SELECT level, order_num, title, content, tension_target FROM outlines
		 WHERE project_id = $1 ORDER BY order_num`, projectID)
	if oRows != nil {
		sb.WriteString("## 章节大纲列表\n")
		for oRows.Next() {
			var level, otitle string
			var orderNum int
			var ocontent json.RawMessage
			var tension float64
			oRows.Scan(&level, &orderNum, &otitle, &ocontent, &tension)
			sb.WriteString(fmt.Sprintf("- [%s #%d] %s (张力:%.1f): %s\n", level, orderNum, otitle, tension, string(ocontent)))
		}
		oRows.Close()
		sb.WriteString("\n")
	}

	// Foreshadowings
	fRows, _ := s.db.Query(ctx,
		`SELECT content, embed_method, status, priority FROM foreshadowings WHERE project_id = $1 ORDER BY priority DESC`, projectID)
	if fRows != nil {
		sb.WriteString("## 伏笔列表\n")
		for fRows.Next() {
			var content, embedMethod, status string
			var priority int
			fRows.Scan(&content, &embedMethod, &status, &priority)
			sb.WriteString(fmt.Sprintf("- [%s P%d] %s（埋设方式：%s）\n", status, priority, content, embedMethod))
		}
		fRows.Close()
		sb.WriteString("\n")
	}

	// If reviewing specific chapter
	if req.Scope == "chapter" && req.TargetID != "" {
		var chTitle, chContent, summary string
		var chapterNum int
		s.db.QueryRow(ctx,
			`SELECT chapter_num, title, content, summary FROM chapters WHERE id = $1`, req.TargetID).
			Scan(&chapterNum, &chTitle, &chContent, &summary)

		sb.WriteString(fmt.Sprintf("## 审核目标章节: 第%d章 《%s》\n", chapterNum, chTitle))
		sb.WriteString(fmt.Sprintf("摘要: %s\n\n", summary))
		// Truncate content at 2000 chars to avoid token overflow
		runes := []rune(chContent)
		if len(runes) > 2000 {
			chContent = string(runes[:2000]) + "...[略]"
		}
		sb.WriteString("章节内容(节选):\n")
		sb.WriteString(chContent)
		sb.WriteString("\n\n")
	}

	return sb.String(), nil
}

// buildAgentPrompt constructs the message that tells an agent what to do in this round.
func (s *AgentReviewService) buildAgentPrompt(
	agent agentDef,
	round, totalRounds int,
	briefing string,
	history []turn,
	prevMsgs []models.AgentMessage,
) string {
	var sb strings.Builder

	sb.WriteString(briefing)
	sb.WriteString("\n\n---\n## 评审团辩论记录\n\n")

	if len(prevMsgs) == 0 {
		sb.WriteString("（这是第一轮，尚无讨论记录）\n")
	} else {
		// Show last 10 messages to avoid token overflow
		start := 0
		if len(prevMsgs) > 10 {
			start = len(prevMsgs) - 10
		}
		for _, m := range prevMsgs[start:] {
			sb.WriteString(fmt.Sprintf("**[第%d轮 - %s]**: %s\n\n", m.Round, m.AgentName, m.Content))
		}
	}

	sb.WriteString(fmt.Sprintf("\n---\n## 你的任务（第 %d / %d 轮）\n\n", round, totalRounds))

	if round == 1 {
		sb.WriteString("这是第一轮分析。请从你的专业视角，对以上材料进行首次审查，列出你发现的主要问题（至少3条）并为每条提供改进建议。\n")
	} else if round < totalRounds {
		sb.WriteString("请基于其他专家的意见：\n")
		sb.WriteString("1. 回应/反驳其他人提出的与你专业相关的观点\n")
		sb.WriteString("2. 补充你在上一轮遗漏的问题\n")
		sb.WriteString("3. 对已有共识点表示认同或提出修正\n")
	} else {
		sb.WriteString("这是最后一轮。请：\n")
		sb.WriteString("1. 从你的专业角度提出最终判断：这部作品最需要解决的1-2个核心问题是什么？\n")
		sb.WriteString("2. 给出具体可操作的修改建议\n")
		sb.WriteString("3. 对整体的评分（满分10分）及理由\n")
	}

	sb.WriteString("\n请用中文回答，控制在500字以内，结构清晰。")
	return sb.String()
}

// runModerator generates the final consensus synthesis.
func (s *AgentReviewService) runModerator(
	ctx context.Context,
	briefing string,
	messages []models.AgentMessage,
) (models.AgentMessage, []models.AgentReviewIssue) {
	var debateSummary strings.Builder
	debateSummary.WriteString("以下是评审团的完整讨论记录：\n\n")
	for _, m := range messages {
		debateSummary.WriteString(fmt.Sprintf("**[%s - 第%d轮]**: %s\n\n", m.AgentName, m.Round, m.Content))
	}

	prompt := fmt.Sprintf(`%s

---

%s

---

## 你是评审主持人，请完成最终综合报告：

1. **共识问题列表**（列出≥3个专家达成共识的问题，按严重程度排序）
每条格式：
## [类别: 大纲/时间线/剧情/角色] [严重度: 严重/主要/次要]
**问题**: 具体描述
**建议**: 可操作的修改方案

2. **分歧点摘要**（哪些问题存在专家分歧，分歧原因是什么）

3. **综合评分**
- 大纲结构: X/10
- 时间线一致性: X/10
- 剧情连贯性: X/10
- 角色设计: X/10
- **综合评分**: X/10

4. **优先修改建议**（最重要的3件事，按优先级排序）

请用中文输出，格式清晰，实用性强。`, briefing, debateSummary.String())

	resp, err := s.ai.Chat(ctx, gateway.ChatRequest{
		Task:      "agent_review",
		MaxTokens: 2000,
		Messages: []gateway.ChatMessage{
			{Role: "system", Content: "你是一位权威的文学评审主持人，负责综合多位专家的意见，提炼共识，给出中肯的最终报告。你客观公正，逻辑清晰，善于发现讨论中的核心要点。"},
			{Role: "user", Content: prompt},
		},
	})

	content := "（主持人综合报告生成失败）"
	if err == nil {
		content = strings.TrimSpace(resp.Content)
	}

	msg := models.AgentMessage{
		Round:     len(messages)/len(agentRoster) + 1,
		Agent:     models.AgentModerator,
		AgentName: "🎯 主持人",
		Content:   content,
		Tags:      []string{"consensus", "final"},
	}

	// Extract structured issues from the moderator's report
	issues := extractIssuesFromReport(content, messages)
	return msg, issues
}

// extractTags infers tags based on round position and content keywords.
func extractTags(content string, round, totalRounds int) []string {
	tags := []string{}
	lower := strings.ToLower(content)
	if strings.Contains(lower, "问题") || strings.Contains(lower, "漏洞") || strings.Contains(lower, "缺陷") {
		tags = append(tags, "issue")
	}
	if strings.Contains(lower, "建议") || strings.Contains(lower, "可以改") || strings.Contains(lower, "应该") {
		tags = append(tags, "suggestion")
	}
	if strings.Contains(lower, "同意") || strings.Contains(lower, "确实") || strings.Contains(lower, "也发现") {
		tags = append(tags, "agreement")
	}
	if strings.Contains(lower, "反对") || strings.Contains(lower, "不同意") || strings.Contains(lower, "质疑") {
		tags = append(tags, "disagreement")
	}
	if round == totalRounds {
		tags = append(tags, "final")
	}
	return tags
}

// extractIssuesFromReport parses structured issues from the moderator's text.
func extractIssuesFromReport(report string, messages []models.AgentMessage) []models.AgentReviewIssue {
	var issues []models.AgentReviewIssue

	categoryMap := map[string]string{
		"大纲": "outline", "时间线": "timeline",
		"剧情": "plot", "角色": "character",
	}
	severityMap := map[string]string{
		"严重": "critical", "主要": "major", "次要": "minor",
	}

	lines := strings.Split(report, "\n")
	var currentIssue *models.AgentReviewIssue

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## [") {
			if currentIssue != nil {
				issues = append(issues, *currentIssue)
			}
			currentIssue = &models.AgentReviewIssue{
				Category:  "general",
				Severity:  "minor",
				Consensus: true,
			}
			for zh, en := range categoryMap {
				if strings.Contains(line, zh) {
					currentIssue.Category = en
					break
				}
			}
			for zh, en := range severityMap {
				if strings.Contains(line, zh) {
					currentIssue.Severity = en
					break
				}
			}
		} else if currentIssue != nil {
			if strings.HasPrefix(line, "**问题**:") {
				currentIssue.Title = strings.TrimPrefix(line, "**问题**:")
				currentIssue.Detail = strings.TrimSpace(currentIssue.Title)
			} else if strings.HasPrefix(line, "**建议**:") {
				currentIssue.Suggestion = strings.TrimSpace(strings.TrimPrefix(line, "**建议**:"))
			}
		}
	}
	if currentIssue != nil && currentIssue.Title != "" {
		issues = append(issues, *currentIssue)
	}

	// If structured parsing yielded nothing, create one issue per unique agent problem
	if len(issues) == 0 {
		seen := map[string]bool{}
		for _, m := range messages {
			if m.Round == 1 {
				key := string(m.Agent)
				if !seen[key] {
					seen[key] = true
					issues = append(issues, models.AgentReviewIssue{
						Category:  "general",
						Severity:  "major",
						Agent:     m.AgentName,
						Title:     fmt.Sprintf("%s的发现", m.AgentName),
						Detail:    truncateRunes(m.Content, 200),
						Consensus: false,
					})
				}
			}
		}
	}

	return issues
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n]) + "..."
	}
	return s
}
