package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const sessionKeyPrefix = "session:"

// RequireAuth validates the Bearer token stored in Redis.
// Returns 401 when the token is missing, invalid, or expired.
func RequireAuth(rdb *redis.Client, sessionTTL time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		var token string
		header := c.GetHeader("Authorization")
		if strings.HasPrefix(header, "Bearer ") {
			token = strings.TrimPrefix(header, "Bearer ")
		}
		// Fallback: accept token from query param (used by EventSource/SSE which cannot set headers).
		if token == "" {
			token = c.Query("token")
		}
		if token == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}

		key := sessionKeyPrefix + token
		username, err := rdb.Get(context.Background(), key).Result()
		if err != nil || username == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}

		// Slide the expiry window on every successful request.
		rdb.Expire(context.Background(), key, sessionTTL)

		c.Set("username", username)
		c.Next()
	}
}
