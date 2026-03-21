// Package retry provides exponential back-off helpers for external API calls.
// Context-aware: cancellation or deadline expiry stops retries immediately.
package retry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"time"
)

// Config controls retry behaviour.
type Config struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Jitter      float64
}

// DefaultConfig suits LLM / sidecar HTTP calls.
var DefaultConfig = Config{
	MaxAttempts: 7,
	BaseDelay:   2 * time.Second,
	MaxDelay:    60 * time.Second,
	Jitter:      0.3,
}

// ErrMaxAttemptsReached is wrapped around the last error when all retries are exhausted.
var ErrMaxAttemptsReached = errors.New("max retry attempts reached")

// Do calls fn up to cfg.MaxAttempts times with exponential back-off.
// fn returns (true, err) to retry, (false, nil) on success, (false, err) on permanent error.
func Do(ctx context.Context, cfg Config, fn func(attempt int) (shouldRetry bool, err error)) error {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		retry, err := fn(attempt)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry || attempt == cfg.MaxAttempts {
			break
		}
		delay := backoffDelay(cfg, attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return fmt.Errorf("%w: %w", ErrMaxAttemptsReached, lastErr)
}

// backoffDelay returns capped, jittered back-off for attempt n (1-based).
func backoffDelay(cfg Config, attempt int) time.Duration {
	exp := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(cfg.BaseDelay) * exp)
	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	if cfg.Jitter > 0 {
		//nolint:gosec
		jitter := time.Duration(float64(delay) * cfg.Jitter * rand.Float64())
		delay += jitter
	}
	return delay
}

func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusInternalServerError ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}
