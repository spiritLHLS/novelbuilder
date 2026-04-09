package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// isPrivateURL returns true if the URL resolves to a loopback, link-local,
// or RFC-1918 private address — used to block SSRF via webhook URLs.
func isPrivateURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true // treat unparsable URLs as unsafe
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return true // only allow HTTP/HTTPS
	}
	host := u.Hostname()
	ips, err := net.LookupHost(host)
	if err != nil {
		// If DNS fails, block the request conservatively
		return true
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return true
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
			return true
		}
	}
	return false
}

type WebhookService struct {
	db         *pgxpool.Pool
	httpClient *http.Client
	logger     *zap.Logger
}

func NewWebhookService(db *pgxpool.Pool, logger *zap.Logger) *WebhookService {
	return &WebhookService{
		db:         db,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}
}

func (s *WebhookService) List(ctx context.Context, projectID string) ([]models.NotificationWebhook, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, project_id, url, events, is_active, created_at, updated_at
		 FROM notification_webhooks WHERE project_id = $1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	defer rows.Close()

	var hooks []models.NotificationWebhook
	for rows.Next() {
		var h models.NotificationWebhook
		if err := rows.Scan(&h.ID, &h.ProjectID, &h.URL, &h.Events,
			&h.IsActive, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		hooks = append(hooks, h)
	}
	return hooks, rows.Err()
}

func (s *WebhookService) Create(ctx context.Context, projectID string, req models.CreateWebhookRequest) (*models.NotificationWebhook, error) {
	if isPrivateURL(req.URL) {
		return nil, fmt.Errorf("webhook URL must point to a public host")
	}
	id := uuid.New().String()
	now := time.Now()

	events := req.Events
	if events == nil {
		events = json.RawMessage("[]")
	}

	_, err := s.db.Exec(ctx,
		`INSERT INTO notification_webhooks (id, project_id, url, secret, events, is_active, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, TRUE, $6, $6)`,
		id, projectID, req.URL, req.Secret, events, now)
	if err != nil {
		return nil, fmt.Errorf("create webhook: %w", err)
	}

	return &models.NotificationWebhook{
		ID: id, ProjectID: projectID, URL: req.URL,
		Events: events, IsActive: true,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *WebhookService) Update(ctx context.Context, id string, req models.CreateWebhookRequest) (*models.NotificationWebhook, error) {
	var h models.NotificationWebhook
	err := s.db.QueryRow(ctx,
		`UPDATE notification_webhooks SET
		   url = COALESCE(NULLIF($1,''), url),
		   secret = CASE WHEN $2 != '' THEN $2 ELSE secret END,
		   events = COALESCE($3, events),
		   updated_at = NOW()
		 WHERE id = $4
		 RETURNING id, project_id, url, events, is_active, created_at, updated_at`,
		req.URL, req.Secret, req.Events, id).Scan(
		&h.ID, &h.ProjectID, &h.URL, &h.Events,
		&h.IsActive, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update webhook: %w", err)
	}
	return &h, nil
}

func (s *WebhookService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM notification_webhooks WHERE id = $1`, id)
	return err
}

// Fire dispatches an event to all active webhooks for the given project that subscribe to the event.
// Errors are logged but NOT propagated — Fire is always best-effort.
func (s *WebhookService) Fire(ctx context.Context, projectID, event string, payload any) {
	rows, err := s.db.Query(ctx,
		`SELECT id, url, secret FROM notification_webhooks
		 WHERE project_id = $1 AND is_active = TRUE
		   AND events @> to_jsonb($2::text)`, projectID, event)
	if err != nil {
		s.logger.Warn("webhook fire: query failed", zap.Error(err))
		return
	}
	defer rows.Close()

	body, err := json.Marshal(map[string]any{
		"event":      event,
		"project_id": projectID,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"data":       payload,
	})
	if err != nil {
		s.logger.Warn("webhook fire: marshal failed", zap.Error(err))
		return
	}

	type hookRow struct{ id, url, secret string }
	var hooks []hookRow
	for rows.Next() {
		var h hookRow
		if err := rows.Scan(&h.id, &h.url, &h.secret); err == nil {
			hooks = append(hooks, h)
		}
	}
	rows.Close()

	for _, h := range hooks {
		h := h
		go func() {
			if isPrivateURL(h.url) {
				s.logger.Warn("webhook fire: blocked private URL", zap.String("webhook_id", h.id), zap.String("url", h.url))
				return
			}
			deliveryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			sig := computeHMAC(body, h.secret)
			req, err := http.NewRequestWithContext(deliveryCtx, http.MethodPost, h.url, bytes.NewReader(body))
			if err != nil {
				s.logger.Warn("webhook fire: build request failed", zap.String("webhook_id", h.id), zap.Error(err))
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Webhook-Signature", "sha256="+sig)
			req.Header.Set("X-Webhook-Event", event)

			resp, err := s.httpClient.Do(req)
			if err != nil {
				s.logger.Warn("webhook delivery failed", zap.String("webhook_id", h.id), zap.Error(err))
				return
			}
			defer resp.Body.Close()
			s.logger.Info("webhook delivered",
				zap.String("webhook_id", h.id),
				zap.String("event", event),
				zap.Int("status", resp.StatusCode))
		}()
	}
}

// computeHMAC produces a hex-encoded HMAC-SHA256 signature. Returns empty string when secret is empty.
func computeHMAC(body []byte, secret string) string {
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
