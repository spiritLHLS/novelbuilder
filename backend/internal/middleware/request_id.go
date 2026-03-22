package middleware

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const RequestIDKey = "request_id"

// RequestID reads the X-Request-Id header (or generates one) and stores it in
// the Gin context and echoes it back in the response header.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-Id")
		if id == "" {
			id = uuid.New().String()
		}
		c.Set(RequestIDKey, id)
		c.Header("X-Request-Id", id)
		c.Next()
	}
}

// Logger logs every HTTP request in a compact, human-readable format:
// | 200 |    1.23ms | GET   /api/path
func Logger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		if q := c.Request.URL.RawQuery; q != "" {
			path = path + "?" + q
		}

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		msg := fmt.Sprintf("| %3d | %10s | %-7s %s",
			status,
			fmtLatency(latency),
			c.Request.Method,
			path,
		)

		switch {
		case status >= 500:
			logger.Error(msg)
		case status >= 400:
			logger.Warn(msg)
		default:
			logger.Info(msg)
		}
	}
}

func fmtLatency(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dμs", d.Microseconds())
	}
	return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000)
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
