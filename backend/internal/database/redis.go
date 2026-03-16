package database

import (
	"context"
	"fmt"

	"github.com/novelbuilder/backend/internal/config"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func NewRedis(cfg config.RedisConfig, logger *zap.Logger) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	logger.Info("Redis connected", zap.String("addr", cfg.Addr))
	return client, nil
}
