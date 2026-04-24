package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// OriginalityService runs async originality audits and plot-divergence checks.
// It calls the Python sidecar /metrics endpoint for AI-probability scoring and
// stores results in the originality_audits table.
type OriginalityService struct {
	db         *pgxpool.Pool
	sidecarURL string
	httpClient *http.Client
	logger     *zap.Logger
}

func NewOriginalityService(db *pgxpool.Pool, sidecarURL string, logger *zap.Logger) *OriginalityService {
	return &OriginalityService{
		db:         db,
		sidecarURL: sidecarURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
}

// metricsRequest body for the Python sidecar /metrics endpoint.
type metricsRequest struct {
	Text string `json:"text"`
}

// metricsResponse captures ai_probability + burst + perplexity from the sidecar.
type metricsResponse struct {
	Perplexity    float64 `json:"perplexity"`
	Burstiness    float64 `json:"burstiness"`
	AIProbability float64 `json:"ai_probability"`
	Verdict       string  `json:"verdict"`
}

// OriginalityAuditResult holds the final audit outcome.
type OriginalityAuditResult struct {
	SemanticSimilarity float64  `json:"semantic_similarity"`
	EventGraphDistance float64  `json:"event_graph_distance"`
	RoleOverlap        float64  `json:"role_overlap"`
	SuspiciousSegments []string `json:"suspicious_segments"`
	AIScore            float64  `json:"ai_score"`
	Pass               bool     `json:"pass"`
}

// AuditChapter runs the full originality audit for a chapter:
//  1. Calls Python sidecar /metrics for AI-probability
//  2. Writes to originality_audits table
//  3. Updates chapters.originality_score
//
// This is designed to be called in a goroutine after chapter save.
func (s *OriginalityService) AuditChapter(ctx context.Context, chapterID, projectID, content string) (*OriginalityAuditResult, error) {
	result := &OriginalityAuditResult{
		SemanticSimilarity: 0,
		EventGraphDistance: 1,
		RoleOverlap:        0,
		SuspiciousSegments: []string{},
		Pass:               true,
	}

	chapterNum := 0
	if err := s.db.QueryRow(ctx,
		`SELECT chapter_num FROM chapters WHERE id = $1`, chapterID).Scan(&chapterNum); err != nil {
		s.logger.Warn("failed to resolve chapter_num for originality", zap.Error(err), zap.String("chapter_id", chapterID))
	}

	// Pull recent chapter bodies in one query (no N+1).
	prevTexts, prevErr := s.loadRecentChapterTexts(ctx, projectID, chapterID, 5)
	if prevErr != nil {
		s.logger.Warn("failed to load recent chapters for originality", zap.Error(prevErr))
	}
	if len(prevTexts) > 0 {
		result.SemanticSimilarity = calcMaxShingleSimilarity(content, prevTexts)
		result.SuspiciousSegments = detectSuspiciousSegments(content, prevTexts, 6)
	}

	roleOverlap, roleErr := s.computeRoleOverlap(ctx, projectID, content, prevTexts)
	if roleErr != nil {
		s.logger.Warn("failed to compute role overlap", zap.Error(roleErr))
	} else {
		result.RoleOverlap = roleOverlap
	}

	if chapterNum > 0 {
		if divergence, _, err := s.CheckPlotDivergence(ctx, projectID, chapterNum); err == nil {
			result.EventGraphDistance = divergence
		} else {
			s.logger.Warn("plot divergence check failed", zap.Error(err))
		}
	}

	// ── Step 1: Call Python sidecar /metrics ──────────────────────────────────
	metricsResult, err := s.callMetrics(ctx, content)
	if err != nil {
		s.logger.Warn("metrics sidecar unavailable, skipping AI score", zap.Error(err))
	} else {
		result.AIScore = metricsResult.AIProbability
	}

	// Composite originality score in [0,1].
	novelty := (1.0-result.AIScore)*0.45 +
		(1.0-result.SemanticSimilarity)*0.35 +
		result.EventGraphDistance*0.15 +
		(1.0-result.RoleOverlap)*0.05
	novelty = clamp01(novelty)

	// Fail only when multiple signals are clearly risky.
	result.Pass = !(result.AIScore > 0.7 && result.SemanticSimilarity > 0.82)
	if novelty < 0.32 {
		result.Pass = false
	}

	// ── Step 2: Insert into originality_audits ────────────────────────────────
	suspiciousJSON, _ := json.Marshal(result.SuspiciousSegments)
	auditID := uuid.New().String()
	_, dbErr := s.db.Exec(ctx,
		`INSERT INTO originality_audits
		 (id, chapter_id, semantic_similarity, event_graph_distance, role_overlap,
		  suspicious_segments, pass, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		auditID, chapterID,
		result.SemanticSimilarity, result.EventGraphDistance, result.RoleOverlap,
		suspiciousJSON, result.Pass)
	if dbErr != nil {
		s.logger.Warn("failed to insert originality audit", zap.Error(dbErr))
	}

	// ── Step 3: Update originality_score on chapter ───────────────────────────
	// Higher score means more original.
	score := novelty
	s.db.Exec(ctx,
		`UPDATE chapters SET originality_score = $1, updated_at = NOW() WHERE id = $2`,
		score, chapterID)

	// Save a lightweight plot snapshot for subsequent divergence checks.
	nodes := buildSnapshotNodes(content)
	if len(nodes) > 0 {
		if err := s.SavePlotSnapshot(ctx, projectID, chapterID, nodes, []map[string]any{}); err != nil {
			s.logger.Warn("save plot snapshot failed", zap.Error(err))
		}
	}

	s.logger.Info("originality audit completed",
		zap.String("chapter_id", chapterID),
		zap.Float64("ai_score", result.AIScore),
		zap.Bool("pass", result.Pass))

	return result, nil
}

// callMetrics calls the Python sidecar /metrics endpoint.
func (s *OriginalityService) callMetrics(ctx context.Context, text string) (*metricsResponse, error) {
	body, _ := json.Marshal(metricsRequest{Text: text})
	raw, err := doRetriableJSONRequest(ctx, s.httpClient, s.logger, "POST /metrics", func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.sidecarURL+"/metrics", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("sidecar /metrics unreachable: %w", err)
	}

	var mr metricsResponse
	if err := json.Unmarshal(raw, &mr); err != nil {
		return nil, fmt.Errorf("decode metrics response: %w", err)
	}
	return &mr, nil
}

// CheckPlotDivergence evaluates whether the candidate chapter's outline node
// diverges sufficiently from the reference plot graph already stored.
// Returns (divergence_score 0–1, is_divergent_enough, error).
// A divergence_score ≥ 0.35 is considered sufficient novelty.
func (s *OriginalityService) CheckPlotDivergence(ctx context.Context, projectID string, chapterNum int) (float64, bool, error) {
	// Retrieve the most recent plot_graph_snapshot for this project.
	var nodesJSON []byte
	err := s.db.QueryRow(ctx,
		`SELECT nodes, edges FROM plot_graph_snapshots
		 WHERE project_id = $1
		 ORDER BY created_at DESC LIMIT 1`, projectID).Scan(&nodesJSON, new([]byte))
	if err != nil {
		// No snapshot yet – treat as fully divergent (first chapter)
		return 1.0, true, nil
	}

	var snapshotNodes []map[string]any
	if e := json.Unmarshal(nodesJSON, &snapshotNodes); e != nil {
		return 1.0, true, nil
	}
	prevSet := make(map[string]struct{}, len(snapshotNodes))
	for _, n := range snapshotNodes {
		label := normalizeToken(fmt.Sprint(n["label"]))
		if label != "" {
			prevSet[label] = struct{}{}
		}
	}

	var content string
	err = s.db.QueryRow(ctx,
		`SELECT content FROM chapters WHERE project_id = $1 AND chapter_num = $2 LIMIT 1`,
		projectID, chapterNum).Scan(&content)
	if err != nil {
		return 1.0, true, nil
	}

	currentNodes := buildSnapshotNodes(content)
	curSet := make(map[string]struct{}, len(currentNodes))
	for _, n := range currentNodes {
		label := normalizeToken(fmt.Sprint(n["label"]))
		if label != "" {
			curSet[label] = struct{}{}
		}
	}

	if len(prevSet) == 0 || len(curSet) == 0 {
		return 1.0, true, nil
	}
	overlap := jaccardSet(prevSet, curSet)
	divergence := clamp01(1.0 - overlap)
	return divergence, divergence >= 0.35, nil
}

// SavePlotSnapshot stores the event graph for the given chapter.
func (s *OriginalityService) SavePlotSnapshot(ctx context.Context, projectID, chapterID string, nodes, edges interface{}) error {
	nodesJSON, _ := json.Marshal(nodes)
	edgesJSON, _ := json.Marshal(edges)
	snapshotID := uuid.New().String()
	_, err := s.db.Exec(ctx,
		`INSERT INTO plot_graph_snapshots
		 (id, project_id, chapter_id, graph_type, nodes, edges, created_at)
		 VALUES ($1, $2, $3, 'chapter_event', $4, $5, NOW())`,
		snapshotID, projectID, chapterID, nodesJSON, edgesJSON)
	return err
}

func (s *OriginalityService) loadRecentChapterTexts(ctx context.Context, projectID, chapterID string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.Query(ctx,
		`SELECT content
		 FROM chapters
		 WHERE project_id = $1 AND id <> $2
		 ORDER BY chapter_num DESC
		 LIMIT $3`,
		projectID, chapterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0, limit)
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		if t != "" {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

func (s *OriginalityService) computeRoleOverlap(ctx context.Context, projectID, current string, previous []string) (float64, error) {
	rows, err := s.db.Query(ctx, `SELECT name FROM characters WHERE project_id = $1`, projectID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	names := make([]string, 0, 16)
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return 0, err
		}
		n = strings.TrimSpace(n)
		if n != "" {
			names = append(names, n)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(names) == 0 {
		return 0, nil
	}

	currentSet := make(map[string]struct{})
	prevSet := make(map[string]struct{})
	for _, name := range names {
		if strings.Contains(current, name) {
			currentSet[name] = struct{}{}
		}
		for _, t := range previous {
			if strings.Contains(t, name) {
				prevSet[name] = struct{}{}
				break
			}
		}
	}
	if len(currentSet) == 0 && len(prevSet) == 0 {
		return 0, nil
	}
	return jaccardSet(prevSet, currentSet), nil
}

func calcMaxShingleSimilarity(current string, previous []string) float64 {
	curSet := makeShingles(normalizeText(current), 5)
	if len(curSet) == 0 {
		return 0
	}
	maxSim := 0.0
	for _, t := range previous {
		prevSet := makeShingles(normalizeText(t), 5)
		sim := jaccardSet(curSet, prevSet)
		if sim > maxSim {
			maxSim = sim
		}
	}
	return math.Round(maxSim*1000) / 1000
}

func detectSuspiciousSegments(current string, previous []string, capN int) []string {
	if capN <= 0 {
		capN = 5
	}
	joined := strings.Join(previous, "\n")
	parts := splitSentences(current)
	out := make([]string, 0, capN)
	seen := map[string]struct{}{}
	for _, p := range parts {
		norm := strings.TrimSpace(p)
		if utf8.RuneCountInString(norm) < 24 {
			continue
		}
		if strings.Contains(joined, norm) {
			if _, ok := seen[norm]; !ok {
				seen[norm] = struct{}{}
				out = append(out, norm)
				if len(out) >= capN {
					break
				}
			}
		}
	}
	return out
}

func buildSnapshotNodes(content string) []map[string]any {
	tokens := extractTopTokens(content, 20)
	nodes := make([]map[string]any, 0, len(tokens))
	for _, t := range tokens {
		nodes = append(nodes, map[string]any{
			"label": t,
		})
	}
	return nodes
}

func splitSentences(text string) []string {
	re := regexp.MustCompile(`[。！？!?；;\n]+`)
	parts := re.Split(text, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func extractTopTokens(text string, limit int) []string {
	if limit <= 0 {
		limit = 20
	}
	re := regexp.MustCompile(`[\p{Han}\p{L}\p{N}]{2,12}`)
	matches := re.FindAllString(strings.ToLower(text), -1)
	if len(matches) == 0 {
		return nil
	}
	stop := map[string]struct{}{
		"我们": {}, "你们": {}, "他们": {}, "这个": {}, "那个": {}, "什么": {},
		"然后": {}, "于是": {}, "自己": {}, "已经": {}, "因为": {}, "所以": {},
		"就是": {}, "不是": {}, "但是": {}, "如果": {}, "还是": {}, "以及": {},
	}
	counts := map[string]int{}
	for _, m := range matches {
		t := normalizeToken(m)
		if t == "" {
			continue
		}
		if _, ok := stop[t]; ok {
			continue
		}
		counts[t]++
	}
	type kv struct {
		k string
		v int
	}
	items := make([]kv, 0, len(counts))
	for k, v := range counts {
		items = append(items, kv{k: k, v: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v == items[j].v {
			return items[i].k < items[j].k
		}
		return items[i].v > items[j].v
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.k)
	}
	return out
}

func normalizeText(s string) string {
	re := regexp.MustCompile(`[\s\p{P}]+`)
	return re.ReplaceAllString(strings.ToLower(s), "")
}

func normalizeToken(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if utf8.RuneCountInString(s) < 2 {
		return ""
	}
	return s
}

func makeShingles(s string, n int) map[string]struct{} {
	if n < 2 {
		n = 2
	}
	r := []rune(s)
	if len(r) < n {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(r)-n+1)
	for i := 0; i+n <= len(r); i++ {
		out[string(r[i:i+n])] = struct{}{}
	}
	return out
}

func jaccardSet(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
