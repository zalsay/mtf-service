package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"fintrack-api/database"
	"fintrack-api/models"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

const DefaultOpenAPIKeyName = "default"

var DefaultOpenAPIScopes = []string{
	"etf:read",
	"mtf:read",
	"mtf:predict",
	"mtf:backtest",
	"strategy:read",
	"strategy:write",
	"watchlist:read",
	"watchlist:write",
	"agent:chat",
	"uzi:read",
}

type OpenAPIService struct {
	db *database.DB
}

func NewOpenAPIService(db *database.DB) *OpenAPIService {
	return &OpenAPIService{db: db}
}

func GenerateOpenAPIKeyMaterial() (string, string, error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", "", err
	}
	raw := "ftk_" + base64.RawURLEncoding.EncodeToString(rawBytes)
	return raw, HashOpenAPIKey(raw), nil
}

func HashOpenAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func VerifyOpenAPIKeyHash(raw string, hash string) bool {
	computed := HashOpenAPIKey(raw)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(strings.TrimSpace(hash))) == 1
}

func ParseOpenAPIScopes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	scopes := make([]string, 0, len(values))
	for _, value := range values {
		scope := strings.TrimSpace(value)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		scopes = append(scopes, scope)
	}
	return scopes
}

func HasOpenAPIScope(scopes []string, required string) bool {
	required = strings.TrimSpace(required)
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == required || strings.TrimSpace(scope) == "admin:*" {
			return true
		}
	}
	return false
}

func (s *OpenAPIService) CreateKey(ctx context.Context, req models.OpenAPIKeyCreateRequest) (*models.OpenAPIKeyCreateResponse, error) {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return nil, errors.New("database is not configured")
	}

	login := strings.TrimSpace(req.Username)
	if login == "" || strings.TrimSpace(req.Password) == "" {
		return nil, errors.New("username and password are required")
	}

	var userID int
	var passwordHash string
	err := s.db.Conn.QueryRowContext(ctx, `
		SELECT id, password_hash
		FROM users
		WHERE email = $1 OR username = $1
	`, login).Scan(&userID, &passwordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("invalid username or password")
		}
		return nil, fmt.Errorf("load user for open api key: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		return nil, errors.New("invalid username or password")
	}

	raw, hash, err := GenerateOpenAPIKeyMaterial()
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = DefaultOpenAPIKeyName
	}
	scopes := ParseOpenAPIScopes(DefaultOpenAPIScopes)

	_, err = s.db.Conn.ExecContext(ctx, `
		INSERT INTO open_api_keys (key_hash, name, owner_user_id, scopes, status)
		VALUES ($1, $2, $3, $4, 'active')
	`, hash, name, userID, pq.Array(scopes))
	if err != nil {
		return nil, fmt.Errorf("create open api key: %w", err)
	}

	return &models.OpenAPIKeyCreateResponse{
		APIKey: raw,
		Name:   name,
		Scopes: scopes,
	}, nil
}

func (s *OpenAPIService) CreateKeyForUser(ctx context.Context, userID int, name string) (*models.OpenAPIKeyFromTokenResponse, error) {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return nil, errors.New("database is not configured")
	}
	if userID <= 0 {
		return nil, errors.New("user_id is required")
	}

	raw, hash, err := GenerateOpenAPIKeyMaterial()
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultOpenAPIKeyName
	}
	scopes := ParseOpenAPIScopes(DefaultOpenAPIScopes)

	_, err = s.db.Conn.ExecContext(ctx, `
		INSERT INTO open_api_keys (key_hash, name, owner_user_id, scopes, status)
		VALUES ($1, $2, $3, $4, 'active')
	`, hash, name, userID, pq.Array(scopes))
	if err != nil {
		return nil, fmt.Errorf("create open api key: %w", err)
	}

	return &models.OpenAPIKeyFromTokenResponse{
		APIKey:         raw,
		Name:           name,
		Scopes:         scopes,
		HasExistingKey: false,
	}, nil
}

func (s *OpenAPIService) GetActiveKeyForUser(ctx context.Context, userID int) (*models.OpenAPIKeyRecord, error) {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return nil, errors.New("database is not configured")
	}
	var record models.OpenAPIKeyRecord
	var scopes []string
	var expiresAt sql.NullTime
	err := s.db.Conn.QueryRowContext(ctx, `
		SELECT id, name, scopes, expires_at
		FROM open_api_keys
		WHERE owner_user_id = $1
		  AND status = 'active'
		  AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		ORDER BY created_at ASC, id ASC
		LIMIT 1
	`, userID).Scan(&record.ID, &record.Name, pq.Array(&scopes), &expiresAt)
	if err != nil {
		return nil, err
	}
	record.UserID = userID
	record.Status = "active"
	record.Scopes = ParseOpenAPIScopes(scopes)
	if expiresAt.Valid {
		t := expiresAt.Time
		record.ExpiresAt = &t
	}
	return &record, nil
}

func (s *OpenAPIService) ValidateKey(ctx context.Context, raw string) (*models.OpenAPIKeyRecord, *models.User, error) {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return nil, nil, errors.New("database is not configured")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil, errors.New("invalid_api_key")
	}

	hash := HashOpenAPIKey(raw)
	var record models.OpenAPIKeyRecord
	var scopes []string
	var expiresAt sql.NullTime
	var user models.User
	err := s.db.Conn.QueryRowContext(ctx, `
		SELECT k.id, k.owner_user_id, k.scopes, k.status, k.expires_at,
		       u.id, u.email, u.username, u.is_premium, u.is_admin, u.membership_level, u.membership_expires_at, u.created_at, u.updated_at
		FROM open_api_keys k
		JOIN users u ON u.id = k.owner_user_id
		WHERE k.key_hash = $1
	`, hash).Scan(
		&record.ID, &record.UserID, pq.Array(&scopes), &record.Status, &expiresAt,
		&user.ID, &user.Email, &user.Username, &user.IsPremium, &user.IsAdmin, &user.MembershipLevel, &user.MembershipExpiresAt, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, errors.New("invalid_api_key")
		}
		return nil, nil, err
	}
	record.Scopes = ParseOpenAPIScopes(scopes)
	if expiresAt.Valid {
		t := expiresAt.Time
		record.ExpiresAt = &t
	}
	if record.Status != "active" {
		return nil, nil, errors.New("api_key_disabled")
	}
	if record.ExpiresAt != nil && record.ExpiresAt.Before(time.Now()) {
		return nil, nil, errors.New("api_key_expired")
	}
	_, _ = s.db.Conn.ExecContext(ctx, `UPDATE open_api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = $1`, record.ID)
	return &record, &user, nil
}
