package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/models"
)

// ── Audit Context Builder ────────────────────────────────────────────────────

func (h *Handler) buildAuditContext(ctx context.Context, chapter *models.Chapter) map[string]interface{} {
	ctxPayload := map[string]interface{}{}

	if rules, err := h.bookRules.Get(ctx, chapter.ProjectID); err == nil && rules != nil {
		ctxPayload["book_rules"] = rules.RulesContent
		if rules.StyleGuide != "" {
			ctxPayload["style_guide"] = rules.StyleGuide
		}
	}

	if summaries, err := h.chapters.GetRecentSummaries(ctx, chapter.ProjectID, chapter.ChapterNum, 3); err == nil && len(summaries) > 0 {
		ctxPayload["previous_summaries"] = summaries
	}

	if chars, err := h.characters.List(ctx, chapter.ProjectID); err == nil && len(chars) > 0 {
		compact := make([]map[string]any, 0, len(chars))
		for _, ch := range chars {
			compact = append(compact, map[string]any{
				"name":          ch.Name,
				"role_type":     ch.RoleType,
				"current_state": ch.CurrentState,
			})
		}
		ctxPayload["characters"] = compact
	}

	if resources, err := h.resourceLedger.List(ctx, chapter.ProjectID); err == nil && len(resources) > 0 {
		compact := make([]map[string]any, 0, len(resources))
		for _, r := range resources {
			compact = append(compact, map[string]any{
				"name":     r.Name,
				"category": r.Category,
				"quantity": r.Quantity,
				"unit":     r.Unit,
				"holder":   r.Holder,
			})
		}
		ctxPayload["resources"] = compact
	}

	if hooks, err := h.foreshadowings.List(ctx, chapter.ProjectID); err == nil && len(hooks) > 0 {
		compact := make([]map[string]any, 0, len(hooks))
		for _, fh := range hooks {
			compact = append(compact, map[string]any{
				"content":  fh.Content,
				"priority": fh.Priority,
				"status":   fh.Status,
			})
		}
		ctxPayload["foreshadowings"] = compact
	}

	return ctxPayload
}

// ── 33-Dimension Audit ────────────────────────────────────────────────────────

func (h *Handler) AuditChapter(c *gin.Context) {
	chapterID := c.Param("id")
	var req models.AuditChapterRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	chapter, err := h.chapters.Get(c.Request.Context(), chapterID)
	if err != nil {
		c.JSON(404, gin.H{"error": "chapter not found"})
		return
	}

	llmCfg := map[string]interface{}{}
	if req.LLMProfileID != "" {
		profile, pErr := h.llmProfiles.GetFull(c.Request.Context(), req.LLMProfileID)
		if pErr != nil {
			c.JSON(500, gin.H{"error": pErr.Error()})
			return
		}
		if profile == nil {
			c.JSON(404, gin.H{"error": "llm profile not found"})
			return
		}
		llmCfg = map[string]interface{}{
			"api_key":     profile.APIKey,
			"model":       profile.ModelName,
			"base_url":    profile.BaseURL,
			"max_tokens":  profile.MaxTokens,
			"temperature": profile.Temperature,
		}
	} else {
		llmCfg, err = h.resolveLLMConfig(c.Request.Context())
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
	}

	auditContext := h.buildAuditContext(c.Request.Context(), chapter)

	report, err := h.audit.RunAudit(c.Request.Context(), chapter, chapter.ProjectID, llmCfg, auditContext)
	if err != nil {
		c.JSON(502, gin.H{"error": "audit failed: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": report})
}

func (h *Handler) RunAuditRevisePipeline(ctx context.Context, chapterID string, req models.AuditReviseRequest) (gin.H, error) {
	chapter, err := h.chapters.Get(ctx, chapterID)
	if err != nil || chapter == nil {
		return nil, fmt.Errorf("chapter not found")
	}

	var auditorCfg map[string]interface{}
	if req.LLMProfileID != "" {
		profile, pErr := h.llmProfiles.GetFull(ctx, req.LLMProfileID)
		if pErr != nil {
			return nil, pErr
		}
		if profile == nil {
			return nil, fmt.Errorf("llm profile not found")
		}
		auditorCfg = map[string]interface{}{
			"api_key":     profile.APIKey,
			"model":       profile.ModelName,
			"base_url":    profile.BaseURL,
			"provider":    profile.Provider,
			"max_tokens":  profile.MaxTokens,
			"temperature": profile.Temperature,
		}
	} else {
		auditorCfg, err = h.resolveAgentLLMConfig(ctx, "auditor", chapter.ProjectID)
		if err != nil {
			return nil, err
		}
	}

	reviserCfg, rErr := h.resolveAgentLLMConfig(ctx, "reviser", chapter.ProjectID)
	if rErr != nil {
		reviserCfg = auditorCfg
	}

	maxRounds := req.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 2
	}
	if maxRounds > 5 {
		maxRounds = 5
	}
	intensity := req.Intensity
	if intensity == "" {
		intensity = "medium"
	}

	rounds := make([]gin.H, 0, maxRounds)
	rules, _ := h.bookRules.Get(ctx, chapter.ProjectID)
	var latest *models.AuditReport
	for i := 1; i <= maxRounds; i++ {
		auditContext := h.buildAuditContext(ctx, chapter)
		report, aErr := h.audit.RunAudit(ctx, chapter, chapter.ProjectID, auditorCfg, auditContext)
		if aErr != nil {
			return nil, aErr
		}
		latest = report
		round := gin.H{"round": i, "audit": report, "rewritten": false}

		if report.Passed && report.AIProbability <= 0.67 {
			rounds = append(rounds, round)
			break
		}

		if i == maxRounds {
			rounds = append(rounds, round)
			break
		}

		rewritten := false

		if !report.Passed {
			narrativeRewrite, nrErr := h.bookRules.NarrativeRevise(ctx, chapter.ID, chapter.Content, report, reviserCfg)
			if nrErr == nil && narrativeRewrite != nil && narrativeRewrite.RewrittenText != "" &&
				narrativeRewrite.RewrittenText != chapter.Content {
				_ = h.chapters.CreateSnapshot(ctx, chapter.ID, "before_narrative_revise", fmt.Sprintf("narrative revise round %d", i))
				updated, upErr := h.chapters.UpdateContent(ctx, chapter.ID, narrativeRewrite.RewrittenText, "needs_recheck")
				if upErr == nil {
					_ = h.chapters.CreateSnapshot(ctx, chapter.ID, "after_narrative_revise", fmt.Sprintf("narrative revise round %d", i))
					chapter = updated
					rewritten = true
					round["narrative_rewrite"] = narrativeRewrite
				}
			} else if nrErr != nil {
				round["narrative_rewrite_error"] = nrErr.Error()
			}
		}

		if report.AIProbability > 0.67 {
			antiRewrite, rwErr := h.bookRules.AntiDetectRewrite(ctx, chapter.ID, chapter.Content, intensity, rules, reviserCfg)
			if rwErr != nil {
				round["rewrite_error"] = rwErr.Error()
				rounds = append(rounds, round)
				break
			}
			if antiRewrite == nil || antiRewrite.RewrittenText == "" || antiRewrite.RewrittenText == chapter.Content {
				round["rewrite_error"] = "rewrite produced no effective changes"
				rounds = append(rounds, round)
				break
			}
			_ = h.chapters.CreateSnapshot(ctx, chapter.ID, "before_auto_revise", fmt.Sprintf("auto revise round %d", i))
			updated, upErr := h.chapters.UpdateContent(ctx, chapter.ID, antiRewrite.RewrittenText, "needs_recheck")
			if upErr != nil {
				round["rewrite_error"] = upErr.Error()
				rounds = append(rounds, round)
				break
			}
			_ = h.chapters.CreateSnapshot(ctx, chapter.ID, "after_auto_revise", fmt.Sprintf("auto revise round %d", i))
			chapter = updated
			rewritten = true
			round["rewrite_result"] = antiRewrite
		}

		if !rewritten {
			rounds = append(rounds, round)
			break
		}

		round["rewritten"] = true
		rounds = append(rounds, round)
	}

	finalChapter, _ := h.chapters.Get(ctx, chapterID)
	return gin.H{"chapter": finalChapter, "final_audit": latest, "rounds": rounds}, nil
}

func (h *Handler) AuditReviseChapter(c *gin.Context) {
	var req models.AuditReviseRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	result, err := h.RunAuditRevisePipeline(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (h *Handler) ListChapterSnapshots(c *gin.Context) {
	items, err := h.chapters.ListSnapshots(c.Request.Context(), c.Param("id"), 30)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": items})
}

func (h *Handler) RestoreChapterSnapshot(c *gin.Context) {
	var req models.RestoreChapterSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ch, err := h.chapters.RestoreFromSnapshot(c.Request.Context(), c.Param("id"), req.SnapshotID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if ch == nil {
		c.JSON(404, gin.H{"error": "snapshot not found"})
		return
	}
	c.JSON(200, gin.H{"data": ch})
}

func (h *Handler) GetChapterAuditReport(c *gin.Context) {
	report, err := h.audit.GetLatestReport(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if report == nil {
		c.JSON(404, gin.H{"error": "no audit report found"})
		return
	}
	c.JSON(200, gin.H{"data": report})
}

// ── Anti-AI Rewrite ───────────────────────────────────────────────────────────

func (h *Handler) AntiDetectChapter(c *gin.Context) {
	chapterID := c.Param("id")
	var req models.AntiDetectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Intensity == "" {
		req.Intensity = "medium"
	}

	chapter, err := h.chapters.Get(c.Request.Context(), chapterID)
	if err != nil {
		c.JSON(404, gin.H{"error": "chapter not found"})
		return
	}

	llmCfg, err := h.resolveLLMConfig(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	rules, _ := h.bookRules.Get(c.Request.Context(), chapter.ProjectID)

	result, err := h.bookRules.AntiDetectRewrite(c.Request.Context(), chapterID, chapter.Content, req.Intensity, rules, llmCfg)
	if err != nil {
		c.JSON(502, gin.H{"error": "anti-detect rewrite failed: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": result})
}

// ── Book Rules ────────────────────────────────────────────────────────────────

func (h *Handler) GetBookRules(c *gin.Context) {
	rules, err := h.bookRules.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": rules})
}

func (h *Handler) UpdateBookRules(c *gin.Context) {
	var req models.UpdateBookRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	rules, err := h.bookRules.Upsert(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": rules})
}

// ── Creative Brief ────────────────────────────────────────────────────────────

func (h *Handler) GenerateCreativeBrief(c *gin.Context) {
	projectID := c.Param("id")
	var req models.CreativeBriefRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	llmCfg, err := h.resolveLLMConfig(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	result, err := h.bookRules.GenerateFromBrief(c.Request.Context(), projectID, req, llmCfg)
	if err != nil {
		c.JSON(502, gin.H{"error": "creative brief failed: " + err.Error()})
		return
	}

	if result.RulesContent != "" || result.StyleGuide != "" {
		antiJSON, _ := json.Marshal(result.AntiAIWordlist)
		bannedJSON, _ := json.Marshal(result.BannedPatterns)
		h.bookRules.Upsert(c.Request.Context(), projectID, models.UpdateBookRulesRequest{
			RulesContent:   result.RulesContent,
			StyleGuide:     result.StyleGuide,
			AntiAIWordlist: antiJSON,
			BannedPatterns: bannedJSON,
		})
	}

	c.JSON(200, gin.H{"data": result})
}
