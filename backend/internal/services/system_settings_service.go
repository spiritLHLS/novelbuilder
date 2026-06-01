package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/novelbuilder/backend/internal/database"
	"go.uber.org/zap"
)

// SystemSettingsService manages key-value application settings persisted in the
// system_settings table. On first boot it auto-generates and stores an AES-256
// encryption key so that no ENCRYPTION_KEY env-var is ever needed.
type SystemSettingsService struct {
	db     *database.DB
	logger *zap.Logger
}

func NewSystemSettingsService(db *database.DB, logger *zap.Logger) *SystemSettingsService {
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

// SyncDefaults mirrors code/env defaults into system_settings without
// overwriting values edited from the UI. The batch keeps startup to one
// round-trip and avoids an N+1 chain of individual upserts.
func (s *SystemSettingsService) SyncDefaults(ctx context.Context, defaults map[string]string) error {
	if len(defaults) == 0 {
		return nil
	}
	batch := &database.Batch{}
	for key, value := range defaults {
		if key == "encryption_key" {
			continue
		}
		batch.Queue(
			`INSERT INTO system_settings (key, value)
			 VALUES ($1, $2)
			 ON CONFLICT (key) DO NOTHING`,
			key, value,
		)
	}
	if batch.Len() == 0 {
		return nil
	}
	br := s.db.SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("sync default setting: %w", err)
		}
	}
	return nil
}

// SyncRuntimeSnapshot stores non-secret infrastructure facts that help the UI
// and diagnostics explain which deployment profile is active. These keys are
// intentionally overwritten on every boot because they describe the current
// process, not user preferences.
func (s *SystemSettingsService) SyncRuntimeSnapshot(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	batch := &database.Batch{}
	for key, value := range values {
		if key == "encryption_key" {
			continue
		}
		batch.Queue(
			`INSERT INTO system_settings (key, value)
			 VALUES ($1, $2)
			 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
			key, value,
		)
	}
	br := s.db.SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("sync runtime setting: %w", err)
		}
	}
	return nil
}

// Delete removes a setting. The encryption_key setting is delete-protected.
func (s *SystemSettingsService) Delete(ctx context.Context, key string) error {
	if key == "encryption_key" {
		return fmt.Errorf("encryption_key is managed internally and cannot be deleted")
	}
	_, err := s.db.Exec(ctx, `DELETE FROM system_settings WHERE key = $1`, key)
	return err
}
