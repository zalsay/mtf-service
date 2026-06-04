package services

import (
	"strings"
	"testing"
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
