package models

import "time"

const (
	UserRoleAdmin = "admin"
	UserRoleUser  = "user"

	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

type User struct {
	ID          string    `json:"id" db:"id"`
	Username    string    `json:"username" db:"username"`
	DisplayName string    `json:"display_name" db:"display_name"`
	Role        string    `json:"role" db:"role"`
	Status      string    `json:"status" db:"status"`
	ModelPolicy string    `json:"model_policy" db:"model_policy"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type UserSession struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type CreateUserRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	ModelPolicy string `json:"model_policy"`
}

type UpdateUserRequest struct {
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	ModelPolicy string `json:"model_policy"`
}
