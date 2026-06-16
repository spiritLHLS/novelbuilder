package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type apiErrorResponseWriter struct {
	gin.ResponseWriter
	status    int
	size      int
	buffering bool
	body      bytes.Buffer
}

func (w *apiErrorResponseWriter) WriteHeader(code int) {
	if code < 100 {
		return
	}
	if w.status != 0 {
		return
	}
	w.status = code
	if code >= http.StatusBadRequest {
		w.buffering = true
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *apiErrorResponseWriter) WriteHeaderNow() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
}

func (w *apiErrorResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.buffering {
		n, err := w.body.Write(data)
		w.size += n
		return n, err
	}
	n, err := w.ResponseWriter.Write(data)
	w.size += n
	return n, err
}

func (w *apiErrorResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *apiErrorResponseWriter) Status() int {
	if w.status != 0 {
		return w.status
	}
	return w.ResponseWriter.Status()
}

func (w *apiErrorResponseWriter) Size() int {
	if w.size > 0 {
		return w.size
	}
	return w.ResponseWriter.Size()
}

func (w *apiErrorResponseWriter) Written() bool {
	return w.status != 0
}

// NormalizeAPIErrorResponses gives all JSON API errors a stable shape while
// preserving legacy "error" and "message" fields consumed by the frontend.
func NormalizeAPIErrorResponses() gin.HandlerFunc {
	return func(c *gin.Context) {
		if shouldSkipErrorNormalization(c) {
			c.Next()
			return
		}

		writer := &apiErrorResponseWriter{ResponseWriter: c.Writer}
		c.Writer = writer
		c.Next()

		if !writer.buffering {
			return
		}

		status := writer.Status()
		payload := normalizeErrorPayload(status, writer.body.Bytes(), c.GetString(RequestIDKey))
		body, err := json.Marshal(payload)
		if err != nil {
			body = []byte(fmt.Sprintf(`{"ok":false,"status":%d,"code":"HTTP_%d","message":%q,"error":%q}`,
				status, status, http.StatusText(status), http.StatusText(status)))
		}
		header := writer.ResponseWriter.Header()
		header.Set("Content-Type", "application/json; charset=utf-8")
		header.Del("Content-Length")
		writer.ResponseWriter.WriteHeader(status)
		_, _ = writer.ResponseWriter.Write(body)
	}
}

func shouldSkipErrorNormalization(c *gin.Context) bool {
	path := c.Request.URL.Path
	if !strings.HasPrefix(path, "/api") {
		return true
	}
	if c.Request.Method == http.MethodHead {
		return true
	}
	if strings.Contains(path, "/stream") {
		return true
	}
	if strings.Contains(c.GetHeader("Accept"), "text/event-stream") {
		return true
	}
	return false
}

func normalizeErrorPayload(status int, body []byte, requestID string) gin.H {
	var raw map[string]any
	if len(bytes.TrimSpace(body)) > 0 {
		_ = json.Unmarshal(body, &raw)
	}
	if raw == nil {
		raw = map[string]any{}
	}

	code := stringValue(raw["code"])
	if code == "" {
		code = fmt.Sprintf("HTTP_%d", status)
	}

	message := firstNonEmpty(
		stringValue(raw["message"]),
		errorMessage(raw["error"]),
		stringValue(raw["detail"]),
		http.StatusText(status),
	)
	details := raw["details"]
	if details == nil {
		if errObj, ok := raw["error"].(map[string]any); ok {
			details = errObj
		}
	}

	payload := gin.H{
		"ok":         false,
		"status":     status,
		"code":       code,
		"message":    message,
		"error":      message,
		"request_id": requestID,
	}
	if details != nil {
		payload["details"] = details
	}
	for key, value := range raw {
		if isStandardErrorField(key) {
			continue
		}
		payload[key] = value
	}
	return payload
}

func isStandardErrorField(key string) bool {
	switch strings.ToLower(key) {
	case "ok", "status", "code", "message", "error", "detail", "details", "request_id":
		return true
	default:
		return false
	}
}

func errorMessage(value any) string {
	if msg := stringValue(value); msg != "" {
		return msg
	}
	if obj, ok := value.(map[string]any); ok {
		return firstNonEmpty(stringValue(obj["message"]), stringValue(obj["error"]), stringValue(obj["detail"]))
	}
	return ""
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
