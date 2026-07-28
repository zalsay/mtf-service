package services

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"fintrack-api/database"
	"fintrack-api/models"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

const DefaultOpenAPIKeyName = "default"

const (
	OpenAPIV2KeyPrefix     = "ftk_v2_"
	OpenAPIV2Algorithm     = "RSA-OAEP-SHA256"
	OpenAPIV2RandomBytes   = 32
	OpenAPIV2TimestampSkew = 5 * time.Minute
)

var (
	ErrOpenAPIV2PrivateKeyUnavailable = errors.New("v2 api private key is unavailable")
	ErrOpenAPIV2InvalidPayload        = errors.New("invalid v2 api key payload")
	ErrOpenAPIV2TimestampExpired      = errors.New("v2 api key payload timestamp is expired")
)

var DefaultOpenAPIScopes = []string{
	"etf:read",
	"mtf:read",
	"mtf:predict",
	"mtf:backtest",
	"strategy:read",
	"strategy:write",
	"watchlist:read",
	"watchlist:write",
	"uzi:read",
}

var DefaultOpenAPIV2Scopes = []string{
	"etf:read",
	"mtf:read",
	"mtf:predict",
}

type OpenAPIService struct {
	db                *database.DB
	v2PrivateKey      *rsa.PrivateKey
	v2PrivateKeyError error
	v2TimestampSkew   time.Duration
}

func NewOpenAPIService(db *database.DB) *OpenAPIService {
	return NewOpenAPIServiceWithV2PrivateKey(
		db,
		os.Getenv("MTF_V2_API_PRIVATE_KEY"),
		os.Getenv("MTF_V2_API_PRIVATE_KEY_FILE"),
		int(OpenAPIV2TimestampSkew.Seconds()),
	)
}

func NewOpenAPIServiceWithV2PrivateKey(db *database.DB, privateKeyPEM string, privateKeyFile string, timestampSkewSeconds int) *OpenAPIService {
	service := &OpenAPIService{
		db:              db,
		v2TimestampSkew: OpenAPIV2TimestampSkew,
	}
	if timestampSkewSeconds > 0 {
		service.v2TimestampSkew = time.Duration(timestampSkewSeconds) * time.Second
	}
	if strings.TrimSpace(privateKeyFile) != "" {
		keyBytes, err := os.ReadFile(strings.TrimSpace(privateKeyFile))
		if err != nil {
			service.v2PrivateKeyError = fmt.Errorf("read v2 api private key file: %w", err)
			return service
		}
		privateKeyPEM = string(keyBytes)
	}
	if strings.TrimSpace(privateKeyPEM) == "" {
		service.v2PrivateKeyError = ErrOpenAPIV2PrivateKeyUnavailable
		return service
	}
	service.v2PrivateKey, service.v2PrivateKeyError = parseOpenAPIV2PrivateKey([]byte(privateKeyPEM))
	return service
}

func GenerateOpenAPIKeyMaterial() (string, string, error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", "", err
	}
	raw := "ftk_" + base64.RawURLEncoding.EncodeToString(rawBytes)
	return raw, HashOpenAPIKey(raw), nil
}

func GenerateOpenAPIV2KeyMaterial() (string, string, error) {
	rawBytes := make([]byte, OpenAPIV2RandomBytes)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", "", err
	}
	raw := OpenAPIV2KeyPrefix + base64.RawURLEncoding.EncodeToString(rawBytes)
	return raw, HashOpenAPIKey(raw), nil
}

func OpenAPIV2APIKeyLength() int {
	return len(OpenAPIV2KeyPrefix) + base64.RawURLEncoding.EncodedLen(OpenAPIV2RandomBytes)
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

func (s *OpenAPIService) PublicV2Key() (*models.OpenAPIV2PublicKeyResponse, error) {
	if s == nil || s.v2PrivateKey == nil {
		if s != nil && s.v2PrivateKeyError != nil {
			return nil, s.v2PrivateKeyError
		}
		return nil, ErrOpenAPIV2PrivateKeyUnavailable
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&s.v2PrivateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal v2 api public key: %w", err)
	}
	return &models.OpenAPIV2PublicKeyResponse{
		Algorithm:       OpenAPIV2Algorithm,
		PublicKey:       string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyBytes})),
		CiphertextBytes: s.v2PrivateKey.Size(),
		APIKeyLength:    OpenAPIV2APIKeyLength(),
	}, nil
}

type openAPIV2Payload struct {
	ServerName string `json:"server_name"`
	UserID     string `json:"user_id"`
	Timestamp  int64  `json:"timestamp"`
}

func parseOpenAPIV2PrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%w: PEM block not found", ErrOpenAPIV2PrivateKeyUnavailable)
	}
	var key *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		key = parsed
	} else {
		parsedKey, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: parse RSA private key: %v", ErrOpenAPIV2PrivateKeyUnavailable, parseErr)
		}
		var ok bool
		key, ok = parsedKey.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%w: private key is not RSA", ErrOpenAPIV2PrivateKeyUnavailable)
		}
	}
	if key.N.BitLen() != 2048 {
		return nil, fmt.Errorf("%w: RSA private key must be 2048 bits", ErrOpenAPIV2PrivateKeyUnavailable)
	}
	return key, nil
}

func decodeOpenAPIV2Ciphertext(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("%w: encrypted_payload is required", ErrOpenAPIV2InvalidPayload)
	}
	encodings := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("%w: encrypted_payload is not valid base64", ErrOpenAPIV2InvalidPayload)
}

func (s *OpenAPIService) decryptV2Payload(encryptedPayload string) (*openAPIV2Payload, error) {
	if s == nil || s.v2PrivateKey == nil {
		if s != nil && s.v2PrivateKeyError != nil {
			return nil, s.v2PrivateKeyError
		}
		return nil, ErrOpenAPIV2PrivateKeyUnavailable
	}
	ciphertext, err := decodeOpenAPIV2Ciphertext(encryptedPayload)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) != s.v2PrivateKey.Size() {
		return nil, fmt.Errorf("%w: ciphertext must be %d bytes", ErrOpenAPIV2InvalidPayload, s.v2PrivateKey.Size())
	}
	plaintext, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, s.v2PrivateKey, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: RSA-OAEP decryption failed", ErrOpenAPIV2InvalidPayload)
	}
	var payload openAPIV2Payload
	decoder := json.NewDecoder(strings.NewReader(string(plaintext)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: payload JSON is invalid", ErrOpenAPIV2InvalidPayload)
	}
	if strings.TrimSpace(payload.ServerName) == "" || strings.TrimSpace(payload.UserID) == "" || payload.Timestamp <= 0 {
		return nil, fmt.Errorf("%w: server_name, user_id and timestamp are required", ErrOpenAPIV2InvalidPayload)
	}
	skew := s.v2TimestampSkew
	if skew <= 0 {
		skew = OpenAPIV2TimestampSkew
	}
	if time.Since(time.Unix(payload.Timestamp, 0)) > skew || time.Until(time.Unix(payload.Timestamp, 0)) > skew {
		return nil, ErrOpenAPIV2TimestampExpired
	}
	return &payload, nil
}

func (s *OpenAPIService) CreateV2Key(ctx context.Context, encryptedPayload string) (*models.OpenAPIV2KeyCreateResponse, error) {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return nil, errors.New("database is not configured")
	}
	payload, err := s.decryptV2Payload(encryptedPayload)
	if err != nil {
		return nil, err
	}
	raw, hash, err := GenerateOpenAPIV2KeyMaterial()
	if err != nil {
		return nil, fmt.Errorf("generate v2 api key: %w", err)
	}
	scopes := ParseOpenAPIScopes(DefaultOpenAPIV2Scopes)
	_, err = s.db.Conn.ExecContext(ctx, `
		INSERT INTO open_api_v2_keys (key_hash, server_name, external_user_id, scopes, status)
		VALUES ($1, $2, $3, $4, 'active')
	`, hash, strings.TrimSpace(payload.ServerName), strings.TrimSpace(payload.UserID), pq.Array(scopes))
	if err != nil {
		return nil, fmt.Errorf("create v2 api key: %w", err)
	}
	return &models.OpenAPIV2KeyCreateResponse{
		APIKey:         raw,
		ServerName:     strings.TrimSpace(payload.ServerName),
		ExternalUserID: strings.TrimSpace(payload.UserID),
		Timestamp:      payload.Timestamp,
	}, nil
}

func (s *OpenAPIService) ValidateV2Key(ctx context.Context, raw string) (*models.OpenAPIV2KeyRecord, error) {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return nil, errors.New("database is not configured")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, OpenAPIV2KeyPrefix) {
		return nil, errors.New("invalid_api_key")
	}
	var record models.OpenAPIV2KeyRecord
	var scopes []string
	var expiresAt sql.NullTime
	err := s.db.Conn.QueryRowContext(ctx, `
		SELECT id, server_name, external_user_id, scopes, status, expires_at
		FROM open_api_v2_keys
		WHERE key_hash = $1
	`, HashOpenAPIKey(raw)).Scan(
		&record.ID, &record.ServerName, &record.ExternalUserID, pq.Array(&scopes), &record.Status, &expiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("invalid_api_key")
		}
		return nil, err
	}
	record.Scopes = ParseOpenAPIScopes(scopes)
	if expiresAt.Valid {
		t := expiresAt.Time
		record.ExpiresAt = &t
	}
	if record.Status != "active" {
		return nil, errors.New("api_key_disabled")
	}
	if record.ExpiresAt != nil && record.ExpiresAt.Before(time.Now()) {
		return nil, errors.New("api_key_expired")
	}
	_, _ = s.db.Conn.ExecContext(ctx, `UPDATE open_api_v2_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = $1`, record.ID)
	return &record, nil
}
