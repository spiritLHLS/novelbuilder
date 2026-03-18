package config

import (
	"os"
	"strconv"
)

// Config holds only the infrastructure parameters that must be known before
// the database is available. All application-level settings (AI model config,
// quality thresholds, encryption key, etc.) are stored in the system_settings
// table and managed through the frontend UI.
type Config struct {
	Server        ServerConfig
	Database      DatabaseConfig
	Redis         RedisConfig
	PythonSidecar PythonSidecarConfig
	TaskQueue     TaskQueueConfig
}

type ServerConfig struct {
	Host string
	Port int
	Mode string
}

type DatabaseConfig struct {
	Host         string
	Port         int
	User         string
	Password     string
	DBName       string
	SSLMode      string
	MaxOpenConns int
	MaxIdleConns int
}

type RedisConfig struct {
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

// Load reads configuration exclusively from environment variables with
// safe defaults that match the bundled single-container setup.
// No config files are read; run `docker run -e DB_HOST=... novelbuilder` to override.
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Host: envStr("SERVER_HOST", "0.0.0.0"),
			Port: envInt("SERVER_PORT", 8080),
			Mode: envStr("SERVER_MODE", "release"),
		},
		Database: DatabaseConfig{
			Host:         envStr("DB_HOST", "127.0.0.1"),
			Port:         envInt("DB_PORT", 5432),
			User:         envStr("DB_USER", "novelbuilder"),
			Password:     envStr("DB_PASSWORD", "novelbuilder"),
			DBName:       envStr("DB_NAME", "novelbuilder"),
			SSLMode:      envStr("DB_SSLMODE", "disable"),
			MaxOpenConns: envInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns: envInt("DB_MAX_IDLE_CONNS", 5),
		},
		Redis: RedisConfig{
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
	}
}
