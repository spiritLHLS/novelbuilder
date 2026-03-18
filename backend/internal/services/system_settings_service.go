package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// SystemSettingsService manages key-value application settings persisted in the
// system_settings table. On first boot it auto-generates and stores an AES-256
// encryption key so that no ENCRYPTION_KEY env-var is ever needed.
type SystemSettingsService struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewSystemSettingsService(db *pgxpool.Pool, logger *zap.Logger) *SystemSettingsService {
	return &SystemSettingsService{db: db, logger: logger}
}

// BootstrapEncryptionKey ensures the encryption key exists in system_settings.
// If not found, a cryptographically random 32-byte key is generated and stored.
// Returns the key so it can be passed to LLMProfileService.
func (s *SystemSettingsService) BootstrapEncryptionKey(ctx context.Context) (string, error) {
	var key string
	err := s.db.QueryRow(ctx,
		`SELECT value FROM system_settings WHERE key = 'encryption_key'`).Scan(&key)
	if err == nil && key != "" {
		return key, nil
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate encryption key: %w", err)
	}
	key = hex.EncodeToString(raw) // 64-char hex string, usable as AES-256 material

	if _, err := s.db.Exec(ctx,
		`INSERT INTO system_settings (key, value)
		 VALUES ('encryption_key', $1)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		key); err != nil {
		return "", fmt.Errorf("persist encryption_key: %w", err)
	}

	s.logger.Info("generated and stored new encryption key in system_settings")
	return key, nil
}

// GetAll returns all settings except the encryption key (which is internal-only).
func (s *SystemSettingsService) GetAll(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.Query(ctx,
		`SELECT key, value FROM system_settings WHERE key != 'encryption_key' ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("query system_settings: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
}

// Set upserts a single setting. The encryption_key setting is write-protected.
func (s *SystemSettingsService) Set(ctx context.Context, key, value string) error {
	if key == "encryption_key" {
		return fmt.Errorf("encryption_key is managed internally and cannot be set via API")
	}
	_, err := s.db.Exec(ctx,
		`INSERT INTO system_settings (key, value)
		 VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		key, value)
	return err
}

// Delete removes a setting. The encryption_key setting is delete-protected.
func (s *SystemSettingsService) Delete(ctx context.Context, key string) error {
	if key == "encryption_key" {
		return fmt.Errorf("encryption_key is managed internally and cannot be deleted")
	}
	_, err := s.db.Exec(ctx, `DELETE FROM system_settings WHERE key = $1`, key)
	return err
}
