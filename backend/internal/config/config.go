package config

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"strconv"
	"strings"
)

// Config holds only the infrastructure parameters that must be known before
// the database is available. All application-level settings (AI model config,
// quality thresholds, encryption key, etc.) are stored in the system_settings
// table and managed through the frontend UI.
type Config struct {
	App           AppConfig
	Server        ServerConfig
	Database      DatabaseConfig
	Redis         RedisConfig
	PythonSidecar PythonSidecarConfig
	TaskQueue     TaskQueueConfig
	Auth          AuthConfig
}

type AppConfig struct {
	Profile string
}

type ServerConfig struct {
	Host           string
	Port           int
	Mode           string
	AllowedOrigins []string
	TrustedProxies []string
}

type DatabaseConfig struct {
	Driver                 string
	Host                   string
	Port                   int
	User                   string
	Password               string
	DBName                 string
	SSLMode                string
	SQLitePath             string
	MaxOpenConns           int
	MaxIdleConns           int
	ConnMaxLifetimeMinutes int
}

type RedisConfig struct {
	Enabled  bool
	Addr     string
	Password string
	DB       int
}

type PythonSidecarConfig struct {
	URL     string
	Timeout int
}

type TaskQueueConfig struct {
	Workers    int
	MaxRetries int
}

// AuthConfig holds credentials for the built-in single-user authentication.
// Credentials can be overridden via environment variables.
type AuthConfig struct {
	Username            string
	Password            string
	PasswordGenerated   bool
	SessionTTLHours     int
	LoginMaxAttempts    int
	LoginWindowSeconds  int
	LoginLockoutSeconds int
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func envIntMin(key string, def int, min int) int {
	value := envInt(key, def)
	if value < min {
		return min
	}
	return value
}

func envIntRange(key string, def int, min int, max int) int {
	value := envInt(key, def)
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		switch v {
		case "1", "true", "TRUE", "yes", "YES", "on", "ON":
			return true
		case "0", "false", "FALSE", "no", "NO", "off", "OFF":
			return false
		}
	}
	return def
}

func envCSV(key string, def []string) []string {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	items := strings.Split(raw, ",")
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func adminPasswordFromEnv() (string, bool) {
	if password := os.Getenv("ADMIN_PASSWORD"); strings.TrimSpace(password) != "" {
		return password, false
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		panic("config: failed to generate temporary ADMIN_PASSWORD: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(raw), true
}

// Load reads configuration exclusively from environment variables with
// safe defaults that match the bundled single-container setup.
// No config files are read; run `docker run -e DB_HOST=... novelbuilder` to override.
func Load() *Config {
	adminPassword, adminPasswordGenerated := adminPasswordFromEnv()
	return &Config{
		App: AppConfig{
			Profile: envStr("APP_PROFILE", "full"),
		},
		Server: ServerConfig{
			Host: envStr("SERVER_HOST", "0.0.0.0"),
			Port: envInt("SERVER_PORT", 8080),
			Mode: envStr("SERVER_MODE", "release"),
			AllowedOrigins: envCSV("ALLOWED_ORIGINS", []string{
				"http://localhost:5173",
				"http://127.0.0.1:5173",
				"http://localhost:4173",
				"http://127.0.0.1:4173",
				"http://localhost:8080",
				"http://127.0.0.1:8080",
				"http://localhost:3000",
				"http://127.0.0.1:3000",
			}),
			TrustedProxies: envCSV("TRUSTED_PROXIES", nil),
		},
		Database: DatabaseConfig{
			Driver:                 envStr("DB_DRIVER", "postgres"),
			Host:                   envStr("DB_HOST", "127.0.0.1"),
			Port:                   envInt("DB_PORT", 5432),
			User:                   envStr("DB_USER", "novelbuilder"),
			Password:               envStr("DB_PASSWORD", ""),
			DBName:                 envStr("DB_NAME", "novelbuilder"),
			SSLMode:                envStr("DB_SSLMODE", "disable"),
			SQLitePath:             envStr("SQLITE_PATH", "/data/novelbuilder.db"),
			MaxOpenConns:           envIntMin("DB_MAX_OPEN_CONNS", 25, 20),
			MaxIdleConns:           envIntMin("DB_MAX_IDLE_CONNS", 5, 5),
			ConnMaxLifetimeMinutes: envIntRange("DB_CONN_MAX_LIFETIME_MINUTES", 60, 1, 60),
		},
		Redis: RedisConfig{
			Enabled:  envBool("REDIS_ENABLED", true),
			Addr:     envStr("REDIS_ADDR", "127.0.0.1:6379"),
			Password: envStr("REDIS_PASSWORD", ""),
			DB:       envInt("REDIS_DB", 0),
		},
		PythonSidecar: PythonSidecarConfig{
			URL:     envStr("SIDECAR_URL", "http://127.0.0.1:8081"),
			Timeout: envInt("SIDECAR_TIMEOUT", 600),
		},
		TaskQueue: TaskQueueConfig{
			Workers:    envInt("TASK_WORKERS", 4),
			MaxRetries: envInt("TASK_MAX_RETRIES", 3),
		},
		Auth: AuthConfig{
			Username:            envStr("ADMIN_USERNAME", "admin"),
			Password:            adminPassword,
			PasswordGenerated:   adminPasswordGenerated,
			SessionTTLHours:     envInt("SESSION_TTL_HOURS", 24),
			LoginMaxAttempts:    envInt("LOGIN_MAX_ATTEMPTS", 5),
			LoginWindowSeconds:  envInt("LOGIN_WINDOW_SECONDS", 300),
			LoginLockoutSeconds: envInt("LOGIN_LOCKOUT_SECONDS", 900),
		},
	}
}
