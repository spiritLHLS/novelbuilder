package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/novelbuilder/backend/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var ErrNotFound = errors.New("session not found")

type Store interface {
	Get(ctx context.Context, token string) (models.UserSession, error)
	Set(ctx context.Context, token string, session models.UserSession, ttl time.Duration) error
	Extend(ctx context.Context, token string, ttl time.Duration) error
	Delete(ctx context.Context, token string) error
	Mode() string
}

const keyPrefix = "session:"

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

func (s *RedisStore) Get(ctx context.Context, token string) (models.UserSession, error) {
	value, err := s.client.Get(ctx, keyPrefix+token).Result()
	if errors.Is(err, redis.Nil) {
		return models.UserSession{}, ErrNotFound
	}
	if err != nil {
		return models.UserSession{}, err
	}
	var session models.UserSession
	if jsonErr := json.Unmarshal([]byte(value), &session); jsonErr == nil && session.Username != "" {
		return session, nil
	}
	// Backward compatibility with sessions created before role-aware sessions.
	return models.UserSession{Username: value, Role: models.UserRoleAdmin}, nil
}

func (s *RedisStore) Set(ctx context.Context, token string, session models.UserSession, ttl time.Duration) error {
	value, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, keyPrefix+token, value, ttl).Err()
}

func (s *RedisStore) Extend(ctx context.Context, token string, ttl time.Duration) error {
	return s.client.Expire(ctx, keyPrefix+token, ttl).Err()
}

func (s *RedisStore) Delete(ctx context.Context, token string) error {
	return s.client.Del(ctx, keyPrefix+token).Err()
}

func (s *RedisStore) Mode() string {
	return "redis"
}

type memoryEntry struct {
	session   models.UserSession
	expiresAt time.Time
}

type MemoryStore struct {
	mu     sync.RWMutex
	values map[string]memoryEntry
	logger *zap.Logger
}

func NewMemoryStore(logger *zap.Logger) *MemoryStore {
	store := &MemoryStore{
		values: make(map[string]memoryEntry),
		logger: logger,
	}
	go store.cleanupLoop()
	return store
}

func (s *MemoryStore) Get(_ context.Context, token string) (models.UserSession, error) {
	now := time.Now()
	s.mu.RLock()
	entry, ok := s.values[token]
	s.mu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		if ok {
			_ = s.Delete(context.Background(), token)
		}
		return models.UserSession{}, ErrNotFound
	}
	return entry.session, nil
}

func (s *MemoryStore) Set(_ context.Context, token string, session models.UserSession, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[token] = memoryEntry{session: session, expiresAt: time.Now().Add(ttl)}
	return nil
}

func (s *MemoryStore) Extend(_ context.Context, token string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.values[token]
	if !ok {
		return ErrNotFound
	}
	entry.expiresAt = time.Now().Add(ttl)
	s.values[token] = entry
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, token)
	return nil
}

func (s *MemoryStore) Mode() string {
	return "memory"
}

func (s *MemoryStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		removed := 0
		s.mu.Lock()
		for token, entry := range s.values {
			if now.After(entry.expiresAt) {
				delete(s.values, token)
				removed++
			}
		}
		s.mu.Unlock()
		if removed > 0 && s.logger != nil {
			s.logger.Debug("expired in-memory sessions cleaned", zap.Int("removed", removed))
		}
	}
}

func NewStore(client *redis.Client, logger *zap.Logger) Store {
	if client != nil {
		return NewRedisStore(client)
	}
	if logger != nil {
		logger.Warn("using in-memory session store; sessions reset on process restart")
	}
	return NewMemoryStore(logger)
}
