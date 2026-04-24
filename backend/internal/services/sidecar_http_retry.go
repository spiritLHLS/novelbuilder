package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/novelbuilder/backend/internal/retry"
	"go.uber.org/zap"
)

var sidecarHTTPRetryConfig = retry.Config{
	MaxAttempts: 4,
	BaseDelay:   1 * time.Second,
	MaxDelay:    8 * time.Second,
	Jitter:      0.2,
}

func doRetriableJSONRequest(
	ctx context.Context,
	client *http.Client,
	logger *zap.Logger,
	operation string,
	buildRequest func(context.Context) (*http.Request, error),
) ([]byte, error) {
	var responseBody []byte

	err := retry.Do(ctx, sidecarHTTPRetryConfig, func(attempt int) (bool, error) {
		req, err := buildRequest(ctx)
		if err != nil {
			return false, err
		}

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			logSidecarRetry(logger, operation, attempt, 0, err)
			return true, fmt.Errorf("%s: %w", operation, err)
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			logSidecarRetry(logger, operation, attempt, resp.StatusCode, err)
			return true, fmt.Errorf("%s: read response: %w", operation, err)
		}

		if resp.StatusCode >= http.StatusBadRequest {
			requestErr := fmt.Errorf("%s returned %d: %s", operation, resp.StatusCode, compactSidecarBody(raw))
			if isRetryableSidecarStatus(resp.StatusCode) {
				logSidecarRetry(logger, operation, attempt, resp.StatusCode, requestErr)
				return true, requestErr
			}
			return false, requestErr
		}

		responseBody = raw
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return responseBody, nil
}

func isRetryableSidecarStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusInternalServerError ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}

func compactSidecarBody(raw []byte) string {
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return "empty response body"
	}
	if len(body) > 400 {
		return body[:400] + "..."
	}
	return body
}

func logSidecarRetry(logger *zap.Logger, operation string, attempt, statusCode int, err error) {
	if logger == nil {
		return
	}
	fields := []zap.Field{
		zap.String("operation", operation),
		zap.Int("attempt", attempt),
		zap.Error(err),
	}
	if statusCode > 0 {
		fields = append(fields, zap.Int("status", statusCode))
	}
	logger.Warn("transient sidecar request failed, retrying", fields...)
}
