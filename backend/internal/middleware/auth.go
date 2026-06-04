package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/sessions"
)

// RequireAuth validates the Bearer token stored in the configured session store.
// Returns 401 when the token is missing, invalid, or expired.
func RequireAuth(store sessions.Store, sessionTTL time.Duration) gin.HandlerFunc {
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

		session, err := store.Get(context.Background(), token)
		if err != nil || session.Username == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		if session.Role == "" {
			session.Role = models.UserRoleUser
		}

		// Slide the expiry window on every successful request.
		_ = store.Extend(context.Background(), token, sessionTTL)

		c.Set("user_id", session.UserID)
		c.Set("username", session.Username)
		c.Set("user_role", session.Role)
		c.Next()
	}
}
