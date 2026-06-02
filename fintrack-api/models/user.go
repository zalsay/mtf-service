package models

import (
	"time"
)

type User struct {
	ID                       int        `json:"id" db:"id"`
	Email                    string     `json:"email" db:"email"`
	PasswordHash             string     `json:"-" db:"password_hash"`
	Username                 string     `json:"username" db:"username"`
	FirstName                *string    `json:"first_name" db:"first_name"`
	LastName                 *string    `json:"last_name" db:"last_name"`
	IsActive                 bool       `json:"is_active" db:"is_active"`
	IsPremium                bool       `json:"is_premium" db:"is_premium"`
	IsAdmin                  bool       `json:"is_admin" db:"is_admin"`
	MembershipLevel          int        `json:"membership_level" db:"membership_level"`
	MembershipExpiresAt      *time.Time `json:"membership_expires_at,omitempty" db:"membership_expires_at"`
	DailyStockAnalysisUserID *string    `json:"daily_stock_analysis_user_id,omitempty" db:"daily_stock_analysis_user_id"`
	CreatedAt                time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at" db:"updated_at"`
}

type UserSession struct {
	ID        int       `json:"id" db:"id"`
	UserID    int       `json:"user_id" db:"user_id"`
	TokenHash string    `json:"-" db:"token_hash"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Request/Response DTOs
type RegisterRequest struct {
	Email          string `json:"email" binding:"required,email"`
	Password       string `json:"password" binding:"required,min=6"`
	Username       string `json:"username" binding:"required,min=3"`
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	ActivationCode string `json:"activation_code"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type AuthResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type UserProfile struct {
	ID                       int        `json:"id"`
	Email                    string     `json:"email"`
	Username                 string     `json:"username"`
	FirstName                *string    `json:"first_name"`
	LastName                 *string    `json:"last_name"`
	IsPremium                bool       `json:"is_premium"`
	IsAdmin                  bool       `json:"is_admin"`
	MembershipLevel          int        `json:"membership_level"`
	MembershipExpiresAt      *time.Time `json:"membership_expires_at,omitempty"`
	DailyStockAnalysisUserID *string    `json:"daily_stock_analysis_user_id,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
}

type MembershipInviteCode struct {
	ID              int       `json:"id" db:"id"`
	Code            string    `json:"code" db:"code"`
	MembershipLevel int       `json:"membership_level" db:"membership_level"`
	DurationDays    int       `json:"duration_days" db:"duration_days"`
	IsActive        bool      `json:"is_active" db:"is_active"`
	UsedCount       int       `json:"used_count" db:"used_count"`
	MaxUses         int       `json:"max_uses" db:"max_uses"`
	Note            *string   `json:"note,omitempty" db:"note"`
	CreatedBy       *int      `json:"created_by,omitempty" db:"created_by"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

type CreateMembershipInviteRequest struct {
	Code            string  `json:"code"`
	MembershipLevel int     `json:"membership_level" binding:"required"`
	DurationDays    int     `json:"duration_days" binding:"required"`
	MaxUses         *int    `json:"max_uses"`
	IsActive        *bool   `json:"is_active"`
	Note            *string `json:"note"`
}

type UpdateInviteActiveRequest struct {
	IsActive bool `json:"is_active"`
}

type RedeemInviteRequest struct {
	Code string `json:"code" binding:"required"`
}

type RedeemInviteResponse struct {
	Message             string    `json:"message"`
	MembershipLevel     int       `json:"membership_level"`
	MembershipExpiresAt time.Time `json:"membership_expires_at"`
}
