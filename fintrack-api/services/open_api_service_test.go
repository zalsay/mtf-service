package services

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"fintrack-api/database"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
)

func TestGenerateOpenAPIKeyValueAndHash(t *testing.T) {
	raw, hash, err := GenerateOpenAPIKeyMaterial()
	if err != nil {
		t.Fatalf("GenerateOpenAPIKeyMaterial error: %v", err)
	}
	if !strings.HasPrefix(raw, "ftk_") {
		t.Fatalf("raw key prefix = %q, want ftk_", raw)
	}
	if len(raw) < 40 {
		t.Fatalf("raw key length = %d, want at least 40", len(raw))
	}
	if hash == "" {
		t.Fatal("expected hash")
	}
	if raw == hash {
		t.Fatal("raw key must not equal stored hash")
	}
	if !VerifyOpenAPIKeyHash(raw, hash) {
		t.Fatal("expected generated key to verify against hash")
	}
	if VerifyOpenAPIKeyHash(raw+"x", hash) {
		t.Fatal("expected modified key to fail verification")
	}
}

func TestParseOpenAPIScopes(t *testing.T) {
	scopes := ParseOpenAPIScopes([]string{" etf:read ", "mtf:read", "", "etf:read"})

	if len(scopes) != 2 {
		t.Fatalf("len(scopes) = %d, want 2: %#v", len(scopes), scopes)
	}
	if !HasOpenAPIScope(scopes, "etf:read") {
		t.Fatal("expected etf:read")
	}
	if HasOpenAPIScope(scopes, "mtf:predict") {
		t.Fatal("did not expect mtf:predict")
	}
}

func TestCreateKeyForUserCreatesAndReturnsRawKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("INSERT INTO open_api_keys").
		WithArgs(sqlmock.AnyArg(), "agent-key", 7, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewOpenAPIService(&database.DB{Conn: db})
	response, err := service.CreateKeyForUser(context.Background(), 7, "agent-key")
	if err != nil {
		t.Fatalf("CreateKeyForUser error = %v", err)
	}
	if !strings.HasPrefix(response.APIKey, "ftk_") {
		t.Fatalf("APIKey = %q, want ftk_ prefix", response.APIKey)
	}
	if response.HasExistingKey {
		t.Fatal("HasExistingKey = true, want false")
	}
	if response.Name != "agent-key" {
		t.Fatalf("Name = %q, want agent-key", response.Name)
	}
	if len(response.Scopes) == 0 {
		t.Fatal("expected default scopes")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

type openAPIV2KeyHashArgument struct {
	ciphertext string
}

func (a openAPIV2KeyHashArgument) Match(value driver.Value) bool {
	text, ok := value.(string)
	if !ok || text == a.ciphertext || len(text) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(text)
	return err == nil
}

func newOpenAPIV2TestService(t *testing.T, db *sql.DB, key *rsa.PrivateKey) *OpenAPIService {
	t.Helper()
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return NewOpenAPIServiceWithV2PrivateKey(&database.DB{Conn: db}, string(privateKeyPEM), "", 300)
}

func encryptOpenAPIV2Payload(t *testing.T, publicKey *rsa.PublicKey, payload map[string]interface{}) string {
	t.Helper()
	plaintext, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal v2 payload: %v", err)
	}
	ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, plaintext, nil)
	if err != nil {
		t.Fatalf("encrypt v2 payload: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(ciphertext)
}

func TestCreateV2KeyDecryptsPayloadAndStoresOnlyShortKeyHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	service := newOpenAPIV2TestService(t, db, privateKey)
	encryptedPayload := encryptOpenAPIV2Payload(t, &privateKey.PublicKey, map[string]interface{}{
		"server_name": "mtf-agents",
		"user_id":     "external-user-7",
		"timestamp":   time.Now().Unix(),
	})
	mock.ExpectExec("INSERT INTO open_api_v2_keys").
		WithArgs(openAPIV2KeyHashArgument{ciphertext: encryptedPayload}, "mtf-agents", "external-user-7", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	response, err := service.CreateV2Key(context.Background(), encryptedPayload)
	if err != nil {
		t.Fatalf("CreateV2Key error = %v", err)
	}
	if !strings.HasPrefix(response.APIKey, OpenAPIV2KeyPrefix) {
		t.Fatalf("APIKey = %q, want %s prefix", response.APIKey, OpenAPIV2KeyPrefix)
	}
	if len(response.APIKey) != OpenAPIV2APIKeyLength() {
		t.Fatalf("APIKey length = %d, want %d", len(response.APIKey), OpenAPIV2APIKeyLength())
	}
	if strings.Contains(response.APIKey, encryptedPayload) {
		t.Fatal("short API key must not contain the encrypted payload")
	}
	if response.ServerName != "mtf-agents" || response.ExternalUserID != "external-user-7" {
		t.Fatalf("response identity = %q/%q", response.ServerName, response.ExternalUserID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestPublicV2KeyReportsShortKeyAndRSA2048Metadata(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	service := newOpenAPIV2TestService(t, nil, privateKey)
	response, err := service.PublicV2Key()
	if err != nil {
		t.Fatalf("PublicV2Key error = %v", err)
	}
	if response.Algorithm != OpenAPIV2Algorithm {
		t.Fatalf("algorithm = %q", response.Algorithm)
	}
	if response.CiphertextBytes != 256 {
		t.Fatalf("ciphertext bytes = %d, want 256", response.CiphertextBytes)
	}
	if response.APIKeyLength != 50 {
		t.Fatalf("api key length = %d, want 50", response.APIKeyLength)
	}
	if !strings.Contains(response.PublicKey, "BEGIN PUBLIC KEY") {
		t.Fatalf("public key is not PEM: %q", response.PublicKey)
	}
}

func TestCreateV2KeyRejectsExpiredPayloadBeforeDatabaseInsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	service := newOpenAPIV2TestService(t, db, privateKey)
	encryptedPayload := encryptOpenAPIV2Payload(t, &privateKey.PublicKey, map[string]interface{}{
		"server_name": "mtf-agents",
		"user_id":     "external-user-7",
		"timestamp":   time.Now().Add(-10 * time.Minute).Unix(),
	})
	if _, err := service.CreateV2Key(context.Background(), encryptedPayload); !errors.Is(err, ErrOpenAPIV2TimestampExpired) {
		t.Fatalf("CreateV2Key error = %v, want expired timestamp", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestValidateV2KeyDoesNotLoadLocalUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	service := NewOpenAPIService(&database.DB{Conn: db})
	raw := OpenAPIV2KeyPrefix + "test-short-key"
	mock.ExpectQuery("SELECT id, server_name, external_user_id, scopes, status, expires_at").
		WithArgs(HashOpenAPIKey(raw)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "server_name", "external_user_id", "scopes", "status", "expires_at"}).
			AddRow(11, "mtf-agents", "external-user-7", "{mtf:read,mtf:predict}", "active", nil))
	mock.ExpectExec("UPDATE open_api_v2_keys SET last_used_at").
		WithArgs(11).
		WillReturnResult(sqlmock.NewResult(0, 1))

	record, err := service.ValidateV2Key(context.Background(), raw)
	if err != nil {
		t.Fatalf("ValidateV2Key error = %v", err)
	}
	if record.ID != 11 || record.ServerName != "mtf-agents" || record.ExternalUserID != "external-user-7" {
		t.Fatalf("record = %#v", record)
	}
	if !HasOpenAPIScope(record.Scopes, "mtf:predict") {
		t.Fatalf("scopes = %#v", record.Scopes)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

var _ = pq.Array
