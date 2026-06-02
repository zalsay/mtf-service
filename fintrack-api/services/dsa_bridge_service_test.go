package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"fintrack-api/config"
)

func TestVerifyBridgeTokenSuccess(t *testing.T) {
	service := NewDSABridgeService(config.DSABridgeConfig{
		SharedSecret: "test-secret",
		Issuer:       "daily_stock_analysis",
	})

	now := time.Now().Unix()
	token := signBridgeToken(t, "test-secret", map[string]any{
		"iss":       "daily_stock_analysis",
		"sub":       "dsa-admin-123",
		"iat":       now,
		"exp":       now + 300,
		"nonce":     "nonce-123",
		"return_to": "/portfolio",
	})

	claims, err := service.VerifyBridgeToken(token)
	if err != nil {
		t.Fatalf("expected token to verify, got error: %v", err)
	}

	if claims.Subject != "dsa-admin-123" {
		t.Fatalf("expected subject to round-trip, got %q", claims.Subject)
	}
	if claims.ReturnTo != "/portfolio" {
		t.Fatalf("expected return_to to round-trip, got %q", claims.ReturnTo)
	}
}

func TestVerifyBridgeTokenRejectsExpiredToken(t *testing.T) {
	service := NewDSABridgeService(config.DSABridgeConfig{
		SharedSecret: "test-secret",
		Issuer:       "daily_stock_analysis",
	})

	now := time.Now().Unix()
	token := signBridgeToken(t, "test-secret", map[string]any{
		"iss":       "daily_stock_analysis",
		"sub":       "dsa-admin-123",
		"iat":       now - 600,
		"exp":       now - 1,
		"nonce":     "nonce-123",
		"return_to": "/",
	})

	_, err := service.VerifyBridgeToken(token)
	if err == nil {
		t.Fatal("expected expired token to fail verification")
	}
	if err != ErrExpiredBridgeToken {
		t.Fatalf("expected ErrExpiredBridgeToken, got %v", err)
	}
}

func TestVerifyBridgeTokenRejectsIssuerMismatch(t *testing.T) {
	service := NewDSABridgeService(config.DSABridgeConfig{
		SharedSecret: "test-secret",
		Issuer:       "daily_stock_analysis",
	})

	now := time.Now().Unix()
	token := signBridgeToken(t, "test-secret", map[string]any{
		"iss":       "another-dsa",
		"sub":       "dsa-admin-123",
		"iat":       now,
		"exp":       now + 300,
		"nonce":     "nonce-123",
		"return_to": "/",
	})

	_, err := service.VerifyBridgeToken(token)
	if err == nil {
		t.Fatal("expected issuer mismatch to fail verification")
	}
	if err != ErrBridgeIssuerMismatch {
		t.Fatalf("expected ErrBridgeIssuerMismatch, got %v", err)
	}
}

func TestVerifyBridgeTokenRejectsBadSignature(t *testing.T) {
	service := NewDSABridgeService(config.DSABridgeConfig{
		SharedSecret: "test-secret",
		Issuer:       "daily_stock_analysis",
	})

	now := time.Now().Unix()
	token := signBridgeToken(t, "wrong-secret", map[string]any{
		"iss":       "daily_stock_analysis",
		"sub":       "dsa-admin-123",
		"iat":       now,
		"exp":       now + 300,
		"nonce":     "nonce-123",
		"return_to": "/",
	})

	_, err := service.VerifyBridgeToken(token)
	if err == nil {
		t.Fatal("expected bad signature to fail verification")
	}
	if err != ErrInvalidBridgeToken {
		t.Fatalf("expected ErrInvalidBridgeToken, got %v", err)
	}
}

func signBridgeToken(t *testing.T, secret string, payload map[string]any) string {
	t.Helper()

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(jsonPayload)
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(encodedPayload)); err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedPayload + "." + signature
}
