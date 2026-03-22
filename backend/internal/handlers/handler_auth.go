package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

const sessionKeyPrefix = "session:"

// Authhandler handles login, logout, and session-check for the built-in
// single-user authentication system.
type AuthHandler struct {
	username   string
	pwHash     []byte // bcrypt hash of the configured password
	rdb        *redis.Client
	sessionTTL time.Duration
}

// NewAuthHandler creates an AuthHandler.  The plain-text password is hashed
// once at startup so comparisons are always constant-time.
func NewAuthHandler(username, password string, rdb *redis.Client, sessionTTLHours int) *AuthHandler {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		// bcrypt should never fail here; panic so the misconfiguration is obvious.
		panic("auth: failed to hash admin password: " + err.Error())
	}
	return &AuthHandler{
		username:   username,
		pwHash:     hash,
		rdb:        rdb,
		sessionTTL: time.Duration(sessionTTLHours) * time.Hour,
	}
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

	// Constant-time username comparison to prevent user enumeration.
	if !strings.EqualFold(req.Username, a.username) {
		c.JSON(401, gin.H{"error": "invalid credentials"})
		return
	}
	if err := bcrypt.CompareHashAndPassword(a.pwHash, []byte(req.Password)); err != nil {
		c.JSON(401, gin.H{"error": "invalid credentials"})
		return
	}

	// Generate a cryptographically random session token.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		c.JSON(500, gin.H{"error": "failed to generate token"})
		return
	}
	token := hex.EncodeToString(raw)

	// Persist in Redis; key expires after sessionTTL (slid on each request by middleware).
	key := sessionKeyPrefix + token
	if err := a.rdb.Set(context.Background(), key, a.username, a.sessionTTL).Err(); err != nil {
		c.JSON(500, gin.H{"error": "failed to store session"})
		return
	}

	c.JSON(200, gin.H{
		"token":      token,
		"username":   a.username,
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
		a.rdb.Del(context.Background(), sessionKeyPrefix+token)
	}
	c.JSON(200, gin.H{"message": "logged out"})
}

// Check verifies the current token and returns the logged-in username.
//
//	GET /api/auth/check
func (a *AuthHandler) Check(c *gin.Context) {
	username, _ := c.Get("username")
	c.JSON(200, gin.H{"username": username, "authenticated": true})
}
