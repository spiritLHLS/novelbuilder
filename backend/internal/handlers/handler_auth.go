package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/novelbuilder/backend/internal/models"
	"github.com/novelbuilder/backend/internal/services"
	"github.com/novelbuilder/backend/internal/sessions"
)

// Authhandler handles login, logout, and session-check for the built-in
// single-user authentication system.
type AuthHandler struct {
	users        *services.UserService
	sessions     sessions.Store
	sessionTTL   time.Duration
	loginLimiter loginLimiter
}

type loginAttempt struct {
	failures    int
	firstFailed time.Time
	lockedUntil time.Time
}

type loginLimiter struct {
	mu          sync.Mutex
	attempts    map[string]loginAttempt
	maxAttempts int
	window      time.Duration
	lockout     time.Duration
}

// NewAuthHandler creates an AuthHandler backed by database users.
func NewAuthHandler(users *services.UserService, sessionStore sessions.Store, sessionTTLHours, loginMaxAttempts, loginWindowSeconds, loginLockoutSeconds int) *AuthHandler {
	if sessionTTLHours <= 0 {
		sessionTTLHours = 24
	}
	if loginMaxAttempts <= 0 {
		loginMaxAttempts = 5
	}
	if loginWindowSeconds <= 0 {
		loginWindowSeconds = 300
	}
	if loginLockoutSeconds <= 0 {
		loginLockoutSeconds = 900
	}
	return &AuthHandler{
		users:      users,
		sessions:   sessionStore,
		sessionTTL: time.Duration(sessionTTLHours) * time.Hour,
		loginLimiter: loginLimiter{
			attempts:    make(map[string]loginAttempt),
			maxAttempts: loginMaxAttempts,
			window:      time.Duration(loginWindowSeconds) * time.Second,
			lockout:     time.Duration(loginLockoutSeconds) * time.Second,
		},
	}
}

func loginKeys(c *gin.Context, username string) []string {
	ip := c.ClientIP()
	normalizedUser := strings.ToLower(strings.TrimSpace(username))
	return []string{"ip:" + ip, "user:" + normalizedUser + "|ip:" + ip}
}

func (a *AuthHandler) retryAfter(keys []string, now time.Time) time.Duration {
	a.loginLimiter.mu.Lock()
	defer a.loginLimiter.mu.Unlock()
	var longest time.Duration
	for _, key := range keys {
		attempt, ok := a.loginLimiter.attempts[key]
		if !ok {
			continue
		}
		if !attempt.lockedUntil.IsZero() && now.Before(attempt.lockedUntil) {
			if wait := time.Until(attempt.lockedUntil); wait > longest {
				longest = wait
			}
			continue
		}
		if now.Sub(attempt.firstFailed) > a.loginLimiter.window+a.loginLimiter.lockout {
			delete(a.loginLimiter.attempts, key)
		}
	}
	return longest
}

func (a *AuthHandler) recordFailure(keys []string, now time.Time) time.Duration {
	a.loginLimiter.mu.Lock()
	defer a.loginLimiter.mu.Unlock()
	var longest time.Duration
	for _, key := range keys {
		attempt := a.loginLimiter.attempts[key]
		if attempt.firstFailed.IsZero() || now.Sub(attempt.firstFailed) > a.loginLimiter.window {
			attempt = loginAttempt{firstFailed: now}
		}
		attempt.failures++
		if attempt.failures >= a.loginLimiter.maxAttempts {
			attempt.lockedUntil = now.Add(a.loginLimiter.lockout)
			if a.loginLimiter.lockout > longest {
				longest = a.loginLimiter.lockout
			}
		}
		a.loginLimiter.attempts[key] = attempt
	}
	if len(a.loginLimiter.attempts) > 4096 {
		for key, attempt := range a.loginLimiter.attempts {
			if now.Sub(attempt.firstFailed) > a.loginLimiter.window+a.loginLimiter.lockout {
				delete(a.loginLimiter.attempts, key)
			}
		}
	}
	return longest
}

func (a *AuthHandler) clearFailures(keys []string) {
	a.loginLimiter.mu.Lock()
	defer a.loginLimiter.mu.Unlock()
	for _, key := range keys {
		delete(a.loginLimiter.attempts, key)
	}
}

func retryAfterSeconds(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	return strconv.Itoa(int((d + time.Second - 1) / time.Second))
}

// Login checks credentials and returns a session token on success.
//
//	POST /api/auth/login
//	Body: { "username": "...", "password": "..." }
func (a *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "username and password are required"})
		return
	}

	now := time.Now()
	keys := loginKeys(c, req.Username)
	if wait := a.retryAfter(keys, now); wait > 0 {
		c.Header("Retry-After", retryAfterSeconds(wait))
		c.JSON(429, gin.H{"error": "too many login attempts; try again later"})
		return
	}

	user, authErr := a.users.Authenticate(c.Request.Context(), req.Username, req.Password)
	if authErr != nil || user == nil {
		if wait := a.recordFailure(keys, now); wait > 0 {
			c.Header("Retry-After", retryAfterSeconds(wait))
			c.JSON(429, gin.H{"error": "too many login attempts; try again later"})
			return
		}
		c.JSON(401, gin.H{"error": "invalid credentials"})
		return
	}
	a.clearFailures(keys)

	// Generate a cryptographically random session token.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		c.JSON(500, gin.H{"error": "failed to generate token"})
		return
	}
	token := hex.EncodeToString(raw)

	// Persist in the configured session store; expiry is slid on each request by middleware.
	session := models.UserSession{UserID: user.ID, Username: user.Username, Role: user.Role}
	if err := a.sessions.Set(context.Background(), token, session, a.sessionTTL); err != nil {
		c.JSON(500, gin.H{"error": "failed to store session"})
		return
	}

	c.JSON(200, gin.H{
		"token":      token,
		"user_id":    user.ID,
		"username":   user.Username,
		"role":       user.Role,
		"expires_in": int(a.sessionTTL.Seconds()),
	})
}

// Logout invalidates the current session token.
//
//	POST /api/auth/logout
func (a *AuthHandler) Logout(c *gin.Context) {
	header := c.GetHeader("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		token := strings.TrimPrefix(header, "Bearer ")
		_ = a.sessions.Delete(context.Background(), token)
	}
	c.JSON(200, gin.H{"message": "logged out"})
}

// Check verifies the current token and returns the logged-in username.
//
//	GET /api/auth/check
func (a *AuthHandler) Check(c *gin.Context) {
	username, _ := c.Get("username")
	userID, _ := c.Get("user_id")
	role, _ := c.Get("user_role")
	c.JSON(200, gin.H{"user_id": userID, "username": username, "role": role, "authenticated": true})
}
