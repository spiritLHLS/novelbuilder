package config

import (
	"os"
	"strings"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	Database      DatabaseConfig      `mapstructure:"database"`
	Redis         RedisConfig         `mapstructure:"redis"`
	PythonSidecar PythonSidecarConfig `mapstructure:"python_sidecar"`
	AIGateway     AIGatewayConfig     `mapstructure:"ai_gateway"`
	Quality       QualityConfig       `mapstructure:"quality"`
	Workflow      WorkflowConfig      `mapstructure:"workflow"`
	Security      SecurityConfig      `mapstructure:"security"`
	TaskQueue     TaskQueueConfig     `mapstructure:"task_queue"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
}

type DatabaseConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	DBName       string `mapstructure:"dbname"`
	SSLMode      string `mapstructure:"sslmode"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type PythonSidecarConfig struct {
	URL     string `mapstructure:"url"`
	Timeout int    `mapstructure:"timeout"`
}

type AIGatewayConfig struct {
	DefaultModel string                   `mapstructure:"default_model"`
	Models       map[string]AIModelConfig `mapstructure:"models"`
	TaskRouting  map[string]string        `mapstructure:"task_routing"`
}

type AIModelConfig struct {
	Provider  string `mapstructure:"provider"`
	APIKey    string `mapstructure:"api_key"`
	Model     string `mapstructure:"model"`
	BaseURL   string `mapstructure:"base_url"`
	MaxTokens int    `mapstructure:"max_tokens"`
}

type QualityConfig struct {
	AIScoreThreshold     float64 `mapstructure:"ai_score_threshold"`
	OriginalityThreshold float64 `mapstructure:"originality_threshold"`
	MinRewardDensity     float64 `mapstructure:"min_reward_density"`
	BurstinessTargetCV   float64 `mapstructure:"burstiness_target_cv"`
}

type WorkflowConfig struct {
	StrictReview bool `mapstructure:"strict_review"`
}

type SecurityConfig struct {
	EncryptionKey string `mapstructure:"encryption_key"`
}

type TaskQueueConfig struct {
	Workers    int `mapstructure:"workers"`
	MaxRetries int `mapstructure:"max_retries"`
}

func Load(logger *zap.Logger) (*Config, error) {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "../configs"
	}

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(configPath)
	viper.AddConfigPath("./configs")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		logger.Warn("Config file not found, using defaults", zap.Error(err))
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Resolve encryption key from environment
	if k := cfg.Security.EncryptionKey; strings.HasPrefix(k, "${") && strings.HasSuffix(k, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(k, "${"), "}")
		cfg.Security.EncryptionKey = os.Getenv(envVar)
	}
	if cfg.Security.EncryptionKey == "" {
		cfg.Security.EncryptionKey = os.Getenv("ENCRYPTION_KEY")
	}

	// Resolve environment variables in API keys
	for name, model := range cfg.AIGateway.Models {
		if strings.HasPrefix(model.APIKey, "${") && strings.HasSuffix(model.APIKey, "}") {
			envVar := strings.TrimSuffix(strings.TrimPrefix(model.APIKey, "${"), "}")
			model.APIKey = os.Getenv(envVar)
			cfg.AIGateway.Models[name] = model
		}
	}

	// Apply defaults
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Database.Port == 0 {
		cfg.Database.Port = 5432
	}
	if cfg.Database.MaxOpenConns == 0 {
		cfg.Database.MaxOpenConns = 25
	}
	if cfg.Quality.BurstinessTargetCV == 0 {
		cfg.Quality.BurstinessTargetCV = 0.8
	}
	if cfg.TaskQueue.Workers == 0 {
		cfg.TaskQueue.Workers = 4
	}
	if cfg.TaskQueue.MaxRetries == 0 {
		cfg.TaskQueue.MaxRetries = 3
	}

	return &cfg, nil
}
