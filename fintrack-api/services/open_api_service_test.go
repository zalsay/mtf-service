package services

import (
	"context"
	"strings"
	"testing"

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

var _ = pq.Array
