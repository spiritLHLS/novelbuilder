package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/novelbuilder/backend/internal/database"
	"github.com/novelbuilder/backend/internal/models"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type UserService struct {
	db     *database.DB
	orm    *gorm.DB
	logger *zap.Logger
}

func NewUserService(db *database.DB, orm *gorm.DB, logger *zap.Logger) *UserService {
	return &UserService{db: db, orm: orm, logger: logger}
}

func normalizeUserRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case models.UserRoleAdmin:
		return models.UserRoleAdmin
	default:
		return models.UserRoleUser
	}
}

func normalizeUserStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case models.UserStatusDisabled:
		return models.UserStatusDisabled
	default:
		return models.UserStatusActive
	}
}

func normalizeModelPolicy(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}"
	}
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "{}"
	}
	return raw
}

func hashPassword(password string) (string, error) {
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		return "", errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func userSchemaToModel(row database.UserSchema) models.User {
	modelPolicy := "{}"
	if len(row.ModelPolicy) > 0 {
		modelPolicy = string(row.ModelPolicy)
	}
	var createdAt, updatedAt time.Time
	if row.CreatedAt != nil {
		createdAt = *row.CreatedAt
	}
	if row.UpdatedAt != nil {
		updatedAt = *row.UpdatedAt
	}
	return models.User{
		ID:          row.ID,
		Username:    row.Username,
		DisplayName: row.DisplayName,
		Role:        row.Role,
		Status:      row.Status,
		ModelPolicy: modelPolicy,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

func (s *UserService) BootstrapAdmin(ctx context.Context, username, password string, resetPassword bool) (*models.User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	now := time.Now()

	var row database.UserSchema
	err := s.orm.WithContext(ctx).First(&row, "username = ?", username).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		passwordHash, err := hashPassword(password)
		if err != nil {
			return nil, err
		}
		row = database.UserSchema{
			ID:           uuid.New().String(),
			Username:     username,
			PasswordHash: passwordHash,
			DisplayName:  username,
			Role:         models.UserRoleAdmin,
			Status:       models.UserStatusActive,
			ModelPolicy:  database.JSONB(`{"scope":"all"}`),
			CreatedAt:    &now,
			UpdatedAt:    &now,
		}
		if err := s.orm.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, fmt.Errorf("create bootstrap admin: %w", err)
		}
		user := userSchemaToModel(row)
		return &user, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load bootstrap admin: %w", err)
	}
	updates := map[string]interface{}{
		"role":       models.UserRoleAdmin,
		"status":     models.UserStatusActive,
		"updated_at": now,
	}
	if resetPassword {
		passwordHash, err := hashPassword(password)
		if err != nil {
			return nil, err
		}
		updates["password_hash"] = passwordHash
		row.PasswordHash = passwordHash
	}
	if row.DisplayName == "" {
		updates["display_name"] = username
	}
	if err := s.orm.WithContext(ctx).Model(&database.UserSchema{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("update bootstrap admin: %w", err)
	}
	row.Role = models.UserRoleAdmin
	row.Status = models.UserStatusActive
	row.UpdatedAt = &now
	if row.DisplayName == "" {
		row.DisplayName = username
	}
	user := userSchemaToModel(row)
	return &user, nil
}

func (s *UserService) Authenticate(ctx context.Context, username, password string) (*models.User, error) {
	username = strings.TrimSpace(username)
	var row database.UserSchema
	err := s.orm.WithContext(ctx).First(&row, "username = ?", username).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	if row.Status != models.UserStatusActive {
		return nil, nil
	}
	if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(password)); err != nil {
		return nil, nil
	}
	user := userSchemaToModel(row)
	return &user, nil
}

func (s *UserService) List(ctx context.Context) ([]models.User, error) {
	var rows []database.UserSchema
	if err := s.orm.WithContext(ctx).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	users := make([]models.User, 0, len(rows))
	for _, row := range rows {
		users = append(users, userSchemaToModel(row))
	}
	return users, nil
}

func (s *UserService) Get(ctx context.Context, id string) (*models.User, error) {
	var row database.UserSchema
	err := s.orm.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	user := userSchemaToModel(row)
	return &user, nil
}

func (s *UserService) Create(ctx context.Context, req models.CreateUserRequest) (*models.User, error) {
	username := strings.TrimSpace(req.Username)
	if username == "" {
		return nil, errors.New("username is required")
	}
	passwordHash, err := hashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	row := database.UserSchema{
		ID:           uuid.New().String(),
		Username:     username,
		PasswordHash: passwordHash,
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Role:         normalizeUserRole(req.Role),
		Status:       normalizeUserStatus(req.Status),
		ModelPolicy:  database.JSONB(normalizeModelPolicy(req.ModelPolicy)),
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	if row.DisplayName == "" {
		row.DisplayName = username
	}
	if err := s.orm.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	user := userSchemaToModel(row)
	return &user, nil
}

func (s *UserService) Update(ctx context.Context, id string, req models.UpdateUserRequest) (*models.User, error) {
	var row database.UserSchema
	err := s.orm.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	updates := map[string]interface{}{"updated_at": time.Now()}
	if strings.TrimSpace(req.Password) != "" {
		hash, err := hashPassword(req.Password)
		if err != nil {
			return nil, err
		}
		updates["password_hash"] = hash
	}
	if strings.TrimSpace(req.DisplayName) != "" {
		updates["display_name"] = strings.TrimSpace(req.DisplayName)
	}
	if strings.TrimSpace(req.Role) != "" {
		updates["role"] = normalizeUserRole(req.Role)
	}
	if strings.TrimSpace(req.Status) != "" {
		updates["status"] = normalizeUserStatus(req.Status)
	}
	if strings.TrimSpace(req.ModelPolicy) != "" {
		updates["model_policy"] = database.JSONB(normalizeModelPolicy(req.ModelPolicy))
	}
	if err := s.orm.WithContext(ctx).Model(&database.UserSchema{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *UserService) Delete(ctx context.Context, id string) error {
	var count int64
	if err := s.orm.WithContext(ctx).Model(&database.UserSchema{}).Where("role = ? AND status = ?", models.UserRoleAdmin, models.UserStatusActive).Count(&count).Error; err != nil {
		return fmt.Errorf("count active admins: %w", err)
	}
	var row database.UserSchema
	if err := s.orm.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("load user: %w", err)
	}
	if row.Role == models.UserRoleAdmin && row.Status == models.UserStatusActive && count <= 1 {
		return errors.New("cannot delete the last active admin")
	}
	return s.orm.WithContext(ctx).Delete(&database.UserSchema{}, "id = ?", id).Error
}
