package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/novelbuilder/backend/internal/crypto"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
)

// LLMProfileService provides CRUD operations for LLM profiles stored in the database.
// All AI services resolve their model configuration through this service at runtime,
// meaning a single profile with is_default=true is used for everything unless overridden.
type LLMProfileService struct {
	db            *pgxpool.Pool
	encryptionKey string
	logger        *zap.Logger
}

func NewLLMProfileService(db *pgxpool.Pool, encryptionKey string, logger *zap.Logger) *LLMProfileService {
	return &LLMProfileService{db: db, encryptionKey: encryptionKey, logger: logger}
}

func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func (s *LLMProfileService) List(ctx context.Context) ([]models.LLMProfile, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, name, provider, base_url, api_key, model_name, max_tokens, temperature,
		        is_default, created_at, updated_at
		 FROM llm_profiles ORDER BY is_default DESC, created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list llm_profiles: %w", err)
	}
	defer rows.Close()

	var profiles []models.LLMProfile
	for rows.Next() {
		var p models.LLMProfile
		var rawKey string
		if err := rows.Scan(&p.ID, &p.Name, &p.Provider, &p.BaseURL, &rawKey,
			&p.ModelName, &p.MaxTokens, &p.Temperature, &p.IsDefault,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.HasAPIKey = rawKey != ""
		p.MaskedAPIKey = maskAPIKey(rawKey)
		profiles = append(profiles, p)
	}
	return profiles, nil
}

func (s *LLMProfileService) Get(ctx context.Context, id string) (*models.LLMProfile, error) {
	var p models.LLMProfile
	var rawKey string
	err := s.db.QueryRow(ctx,
		`SELECT id, name, provider, base_url, api_key, model_name, max_tokens, temperature,
		        is_default, created_at, updated_at
		 FROM llm_profiles WHERE id = $1`, id).Scan(
		&p.ID, &p.Name, &p.Provider, &p.BaseURL, &rawKey,
		&p.ModelName, &p.MaxTokens, &p.Temperature, &p.IsDefault,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get llm_profile: %w", err)
	}
	p.HasAPIKey = rawKey != ""
	p.MaskedAPIKey = maskAPIKey(rawKey)
	return &p, nil
}

// GetFull returns the profile including the raw API key (for internal use by gateway only).
func (s *LLMProfileService) GetFull(ctx context.Context, id string) (*models.LLMProfileFull, error) {
	var p models.LLMProfileFull
	var storedKey string
	err := s.db.QueryRow(ctx,
		`SELECT id, name, provider, base_url, api_key, model_name, max_tokens, temperature,
		        is_default, created_at, updated_at
		 FROM llm_profiles WHERE id = $1`, id).Scan(
		&p.ID, &p.Name, &p.Provider, &p.BaseURL, &storedKey,
		&p.ModelName, &p.MaxTokens, &p.Temperature, &p.IsDefault,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get llm_profile full: %w", err)
	}
	rawKey, _ := crypto.Decrypt(storedKey, s.encryptionKey)
	p.APIKey = rawKey
	p.HasAPIKey = storedKey != ""
	p.MaskedAPIKey = maskAPIKey(rawKey)
	return &p, nil
}

// GetDefault returns the default profile (is_default = true) with its raw API key.
// Returns nil, nil when no default profile is configured.
func (s *LLMProfileService) GetDefault(ctx context.Context) (*models.LLMProfileFull, error) {
	var p models.LLMProfileFull
	var storedKey string
	err := s.db.QueryRow(ctx,
		`SELECT id, name, provider, base_url, api_key, model_name, max_tokens, temperature,
		        is_default, created_at, updated_at
		 FROM llm_profiles WHERE is_default = TRUE LIMIT 1`).Scan(
		&p.ID, &p.Name, &p.Provider, &p.BaseURL, &storedKey,
		&p.ModelName, &p.MaxTokens, &p.Temperature, &p.IsDefault,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, nil // no default configured
	}
	rawKey, _ := crypto.Decrypt(storedKey, s.encryptionKey)
	p.APIKey = rawKey
	p.HasAPIKey = storedKey != ""
	p.MaskedAPIKey = maskAPIKey(rawKey)
	return &p, nil
}

func (s *LLMProfileService) Create(ctx context.Context, req models.CreateLLMProfileRequest) (*models.LLMProfile, error) {
	if req.MaxTokens == 0 {
		req.MaxTokens = 8192
	}
	if req.Temperature == 0 {
		req.Temperature = 0.7
	}

	id := uuid.New().String()
	now := time.Now()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// If this is to be the default, clear existing default first
	if req.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE llm_profiles SET is_default = FALSE WHERE is_default = TRUE`); err != nil {
			return nil, fmt.Errorf("clear existing default: %w", err)
		}
	}

	encryptedKey, err := crypto.Encrypt(req.APIKey, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO llm_profiles (id, name, provider, base_url, api_key, model_name, max_tokens, temperature, is_default, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)`,
		id, req.Name, req.Provider, req.BaseURL, encryptedKey, req.ModelName,
		req.MaxTokens, req.Temperature, req.IsDefault, now); err != nil {
		return nil, fmt.Errorf("insert llm_profile: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &models.LLMProfile{
		ID:           id,
		Name:         req.Name,
		Provider:     req.Provider,
		BaseURL:      req.BaseURL,
		ModelName:    req.ModelName,
		MaxTokens:    req.MaxTokens,
		Temperature:  req.Temperature,
		IsDefault:    req.IsDefault,
		HasAPIKey:    req.APIKey != "",
		MaskedAPIKey: maskAPIKey(req.APIKey),
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (s *LLMProfileService) Update(ctx context.Context, id string, req models.UpdateLLMProfileRequest) (*models.LLMProfile, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Read existing to handle partial updates
	var existing models.LLMProfileFull
	if err := tx.QueryRow(ctx,
		`SELECT id, name, provider, base_url, api_key, model_name, max_tokens, temperature, is_default, created_at, updated_at
		 FROM llm_profiles WHERE id = $1 FOR UPDATE`, id).Scan(
		&existing.ID, &existing.Name, &existing.Provider, &existing.BaseURL, &existing.APIKey,
		&existing.ModelName, &existing.MaxTokens, &existing.Temperature, &existing.IsDefault,
		&existing.CreatedAt, &existing.UpdatedAt); err != nil {
		return nil, fmt.Errorf("profile not found: %w", err)
	}

	// Apply partial updates
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Provider != "" {
		existing.Provider = req.Provider
	}
	if req.BaseURL != "" {
		existing.BaseURL = req.BaseURL
	}
	// maskedKey tracks the plain-text key for safe display in the returned struct.
	maskedKey := ""
	if req.APIKey != "" {
		maskedKey = req.APIKey
		encKey, err := crypto.Encrypt(req.APIKey, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt api key: %w", err)
		}
		existing.APIKey = encKey
	}
	if req.ModelName != "" {
		existing.ModelName = req.ModelName
	}
	if req.MaxTokens > 0 {
		existing.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		existing.Temperature = req.Temperature
	}
	existing.IsDefault = req.IsDefault

	// If becoming default, clear others
	if req.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE llm_profiles SET is_default = FALSE WHERE is_default = TRUE AND id != $1`, id); err != nil {
			return nil, fmt.Errorf("clear existing default: %w", err)
		}
	}

	now := time.Now()
	if _, err := tx.Exec(ctx,
		`UPDATE llm_profiles SET name=$1, provider=$2, base_url=$3, api_key=$4, model_name=$5,
		 max_tokens=$6, temperature=$7, is_default=$8, updated_at=$9 WHERE id=$10`,
		existing.Name, existing.Provider, existing.BaseURL, existing.APIKey, existing.ModelName,
		existing.MaxTokens, existing.Temperature, existing.IsDefault, now, id); err != nil {
		return nil, fmt.Errorf("update llm_profile: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &models.LLMProfile{
		ID:           existing.ID,
		Name:         existing.Name,
		Provider:     existing.Provider,
		BaseURL:      existing.BaseURL,
		ModelName:    existing.ModelName,
		MaxTokens:    existing.MaxTokens,
		Temperature:  existing.Temperature,
		IsDefault:    existing.IsDefault,
		HasAPIKey:    existing.APIKey != "",
		MaskedAPIKey: maskAPIKey(maskedKey),
		CreatedAt:    existing.CreatedAt,
		UpdatedAt:    now,
	}, nil
}

func (s *LLMProfileService) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM llm_profiles WHERE id = $1`, id)
	return err
}
