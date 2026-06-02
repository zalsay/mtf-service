package services

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"fintrack-api/database"
	"fintrack-api/models"
	"fintrack-api/utils"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	db *database.DB
}

var (
	ErrDSABindingConflict  = errors.New("current fintrack account is already bound to another daily_stock_analysis user")
	ErrDSAUserAlreadyBound = errors.New("daily_stock_analysis user is already bound to another fintrack account")
)

func NewAuthService(db *database.DB) *AuthService {
	return &AuthService{db: db}
}

func (s *AuthService) Register(req *models.RegisterRequest) (*models.AuthResponse, error) {
	// Check if user already exists
	var exists bool
	err := s.db.Conn.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", req.Email).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("user with email %s already exists", req.Email)
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	tx, err := s.db.Conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Create user
	var user models.User
	query := `
		INSERT INTO users (email, password_hash, username, membership_level, is_admin)
		VALUES ($1, $2, $3, 0, FALSE)
		RETURNING id, email, username, is_premium, is_admin, membership_level, membership_expires_at, created_at, updated_at
	`
	err = tx.QueryRow(query, req.Email, string(hashedPassword), req.Username).Scan(
		&user.ID, &user.Email, &user.Username, &user.IsPremium, &user.IsAdmin, &user.MembershipLevel, &user.MembershipExpiresAt, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	if normalizeInviteCode(req.ActivationCode) != "" {
		redeemed, err := redeemMembershipInviteInTx(tx, user.ID, req.ActivationCode, time.Now())
		if err != nil {
			return nil, err
		}
		user.MembershipLevel = redeemed.MembershipLevel
		user.MembershipExpiresAt = &redeemed.MembershipExpiresAt
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit registration: %w", err)
	}
	committed = true

	// Generate JWT token
	token, err := utils.GenerateJWT(user.ID, user.Email, user.IsAdmin, user.MembershipLevel, user.MembershipExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	// Store session
	if err := s.storeSession(user.ID, token); err != nil {
		return nil, fmt.Errorf("failed to store session: %w", err)
	}

	return &models.AuthResponse{
		Token: token,
		User:  user,
	}, nil
}

func (s *AuthService) Login(req *models.LoginRequest) (*models.AuthResponse, error) {
	var user models.User
	var passwordHash string

	query := `
		SELECT id, email, password_hash, username, is_premium, is_admin, membership_level, membership_expires_at, created_at, updated_at
		FROM users
		WHERE email = $1
	`
	err := s.db.Conn.QueryRow(query, req.Email).Scan(
		&user.ID, &user.Email, &passwordHash, &user.Username, &user.IsPremium, &user.IsAdmin, &user.MembershipLevel, &user.MembershipExpiresAt, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid email or password")
		}
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		return nil, fmt.Errorf("invalid email or password")
	}

	// Generate JWT token
	token, err := utils.GenerateJWT(user.ID, user.Email, user.IsAdmin, user.MembershipLevel, user.MembershipExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	// Store session
	if err := s.storeSession(user.ID, token); err != nil {
		return nil, fmt.Errorf("failed to store session: %w", err)
	}

	return &models.AuthResponse{
		Token: token,
		User:  user,
	}, nil
}

func (s *AuthService) GetUserProfile(userID int) (*models.UserProfile, error) {
	var profile models.UserProfile
	query := `
		SELECT id, email, username, first_name, last_name, is_premium, is_admin, membership_level, membership_expires_at, daily_stock_analysis_user_id, created_at
		FROM users
		WHERE id = $1
	`
	err := s.db.Conn.QueryRow(query, userID).Scan(
		&profile.ID, &profile.Email, &profile.Username, &profile.FirstName, &profile.LastName, &profile.IsPremium, &profile.IsAdmin, &profile.MembershipLevel, &profile.MembershipExpiresAt, &profile.DailyStockAnalysisUserID, &profile.CreatedAt,
	)
	if isUndefinedColumnError(err, "daily_stock_analysis_user_id") {
		fallbackQuery := `
			SELECT id, email, username, first_name, last_name, is_premium, is_admin, membership_level, membership_expires_at, created_at
			FROM users
			WHERE id = $1
		`
		err = s.db.Conn.QueryRow(fallbackQuery, userID).Scan(
			&profile.ID, &profile.Email, &profile.Username, &profile.FirstName, &profile.LastName, &profile.IsPremium, &profile.IsAdmin, &profile.MembershipLevel, &profile.MembershipExpiresAt, &profile.CreatedAt,
		)
		if err == nil {
			profile.DailyStockAnalysisUserID = nil
		}
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user profile: %w", err)
	}

	return &profile, nil
}

func isUndefinedColumnError(err error, column string) bool {
	if err == nil {
		return false
	}

	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "42703" && strings.Contains(strings.ToLower(pqErr.Message), strings.ToLower(column))
	}

	return false
}

func (s *AuthService) ValidateSession(token string) (*models.User, error) {
	// First validate JWT token
	claims, err := utils.ValidateJWT(token)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	// Check if session exists in database
	tokenHash := s.hashToken(token)
	var sessionExists bool
	err = s.db.Conn.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM user_sessions
			WHERE user_id = $1 AND token_hash = $2 AND expires_at > NOW()
		)
	`, claims.UserID, tokenHash).Scan(&sessionExists)
	if err != nil {
		return nil, fmt.Errorf("failed to validate session: %w", err)
	}
	if !sessionExists {
		return nil, fmt.Errorf("session not found or expired")
	}

	// Get user details
	var user models.User
	query := `
		SELECT id, email, username, is_premium, is_admin, membership_level, membership_expires_at, created_at, updated_at
		FROM users
		WHERE id = $1
	`
	err = s.db.Conn.QueryRow(query, claims.UserID).Scan(
		&user.ID, &user.Email, &user.Username, &user.IsPremium, &user.IsAdmin, &user.MembershipLevel, &user.MembershipExpiresAt, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

func (s *AuthService) Logout(userID int, token string) error {
	tokenHash := s.hashToken(token)
	_, err := s.db.Conn.Exec(`
		DELETE FROM user_sessions
		WHERE user_id = $1 AND token_hash = $2
	`, userID, tokenHash)
	return err
}

func (s *AuthService) storeSession(userID int, token string) error {
	tokenHash := s.hashToken(token)
	expiresAt := time.Now().Add(24 * time.Hour) // 24 hours from now

	// Use UPSERT to update existing session or insert new one
	// This ensures each user has only one active session
	_, err := s.db.Conn.Exec(`
		INSERT INTO user_sessions (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id)
		DO UPDATE SET
			token_hash = EXCLUDED.token_hash,
			expires_at = EXCLUDED.expires_at,
			created_at = CURRENT_TIMESTAMP
	`, userID, tokenHash, expiresAt)

	return err
}

func (s *AuthService) hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", hash)
}

func (s *AuthService) CleanupExpiredSessions() error {
	_, err := s.db.Conn.Exec("DELETE FROM user_sessions WHERE expires_at < NOW()")
	return err
}

func (s *AuthService) UpdateMembershipLevel(userID int, level int, expiresAt *time.Time) error {
	if level < 0 || level > 3 {
		return fmt.Errorf("invalid membership level")
	}
	_, err := s.db.Conn.Exec(`
		UPDATE users
		SET membership_level = $1,
			membership_expires_at = $2,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $3
	`, level, expiresAt, userID)
	return err
}

func (s *AuthService) RedeemMembershipInvite(userID int, code string) (*models.RedeemInviteResponse, error) {
	if err := ensureMembershipInviteMaxUsesColumn(s.db); err != nil {
		return nil, err
	}
	tx, err := s.db.Conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	response, err := redeemMembershipInviteInTx(tx, userID, code, time.Now())
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit invite redemption: %w", err)
	}
	committed = true

	return response, nil
}

func redeemMembershipInviteInTx(tx *sql.Tx, userID int, code string, now time.Time) (*models.RedeemInviteResponse, error) {
	normalizedCode := normalizeInviteCode(code)
	if normalizedCode == "" {
		return nil, fmt.Errorf("invite code is required")
	}

	var invite models.MembershipInviteCode
	err := tx.QueryRow(`
		SELECT id, code, membership_level, duration_days, is_active, used_count, max_uses, note, created_by, created_at, updated_at
		FROM membership_invite_codes
		WHERE code = $1
		FOR UPDATE
	`, normalizedCode).Scan(
		&invite.ID, &invite.Code, &invite.MembershipLevel, &invite.DurationDays,
		&invite.IsActive, &invite.UsedCount, &invite.MaxUses, &invite.Note, &invite.CreatedBy,
		&invite.CreatedAt, &invite.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invite code not found")
		}
		return nil, fmt.Errorf("failed to load invite code: %w", err)
	}
	if !invite.IsActive {
		return nil, fmt.Errorf("invite code is disabled")
	}
	if invite.MaxUses <= 0 {
		invite.MaxUses = DefaultInviteMaxUses
	}
	if invite.UsedCount >= invite.MaxUses {
		return nil, fmt.Errorf("invite code usage limit reached")
	}

	var currentLevel int
	var currentExpiresAt *time.Time
	err = tx.QueryRow(`
		SELECT membership_level, membership_expires_at
		FROM users
		WHERE id = $1
		FOR UPDATE
	`, userID).Scan(&currentLevel, &currentExpiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to load user membership: %w", err)
	}

	baseTime := now
	activeLevel := currentLevel
	if currentExpiresAt != nil {
		if currentExpiresAt.After(now) {
			baseTime = *currentExpiresAt
		} else {
			activeLevel = 0
		}
	}
	nextLevel := invite.MembershipLevel
	if activeLevel > nextLevel {
		nextLevel = activeLevel
	}
	nextExpiresAt := baseTime.AddDate(0, 0, invite.DurationDays)

	if _, err := tx.Exec(`
		UPDATE users
		SET membership_level = $1,
			membership_expires_at = $2,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $3
	`, nextLevel, nextExpiresAt, userID); err != nil {
		return nil, fmt.Errorf("failed to update membership: %w", err)
	}

	result, err := tx.Exec(`
		UPDATE membership_invite_codes
		SET used_count = used_count + 1,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
			AND used_count < max_uses
	`, invite.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update invite usage: %w", err)
	}
	if rowsAffected, err := result.RowsAffected(); err != nil {
		return nil, fmt.Errorf("failed to verify invite usage update: %w", err)
	} else if rowsAffected == 0 {
		return nil, fmt.Errorf("invite code usage limit reached")
	}

	return &models.RedeemInviteResponse{
		Message:             "membership invite redeemed",
		MembershipLevel:     nextLevel,
		MembershipExpiresAt: nextExpiresAt,
	}, nil
}

func (s *AuthService) BindDSAUser(userID int, dsaUserID string) error {
	normalizedDSAUserID := strings.TrimSpace(dsaUserID)
	if normalizedDSAUserID == "" {
		return fmt.Errorf("daily_stock_analysis user id is required")
	}

	tx, err := s.db.Conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var existingBinding sql.NullString
	if err := tx.QueryRow(`
		SELECT daily_stock_analysis_user_id
		FROM users
		WHERE id = $1
		FOR UPDATE
	`, userID).Scan(&existingBinding); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("user not found")
		}
		return fmt.Errorf("failed to load current dsa binding: %w", err)
	}

	if existingBinding.Valid {
		if existingBinding.String == normalizedDSAUserID {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("failed to commit idempotent dsa binding: %w", err)
			}
			committed = true
			return nil
		}
		return ErrDSABindingConflict
	}

	if _, err := tx.Exec(`
		UPDATE users
		SET daily_stock_analysis_user_id = $1,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`, normalizedDSAUserID, userID); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return ErrDSAUserAlreadyBound
		}
		return fmt.Errorf("failed to update dsa binding: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit dsa binding: %w", err)
	}
	committed = true
	return nil
}

func (s *AuthService) UnbindDSAUser(userID int) error {
	result, err := s.db.Conn.Exec(`
		UPDATE users
		SET daily_stock_analysis_user_id = NULL,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, userID)
	if err != nil {
		return fmt.Errorf("failed to clear dsa binding: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect dsa unbind result: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}
