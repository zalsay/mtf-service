package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"fintrack-api/config"
)

var (
	ErrBridgeNotConfigured  = errors.New("dsa bridge is not configured")
	ErrInvalidBridgeToken   = errors.New("invalid dsa bridge token")
	ErrExpiredBridgeToken   = errors.New("expired dsa bridge token")
	ErrBridgeIssuerMismatch = errors.New("dsa bridge token issuer mismatch")
)

type BridgeClaims struct {
	Issuer    string
	Subject   string
	IssuedAt  int64
	ExpiresAt int64
	Nonce     string
	ReturnTo  string
}

type bridgeTokenPayload struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Nonce     string `json:"nonce"`
	ReturnTo  string `json:"return_to"`
}

type DSABridgeService struct {
	sharedSecret []byte
	issuer       string
}

func NewDSABridgeService(cfg config.DSABridgeConfig) *DSABridgeService {
	return &DSABridgeService{
		sharedSecret: []byte(strings.TrimSpace(cfg.SharedSecret)),
		issuer:       strings.TrimSpace(cfg.Issuer),
	}
}

func (s *DSABridgeService) IsEnabled() bool {
	return len(s.sharedSecret) > 0 && s.issuer != ""
}

func (s *DSABridgeService) Issuer() string {
	return s.issuer
}

func (s *DSABridgeService) VerifyBridgeToken(raw string) (*BridgeClaims, error) {
	if !s.IsEnabled() {
		return nil, ErrBridgeNotConfigured
	}

	parts := strings.Split(raw, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrInvalidBridgeToken
	}

	payloadSegment := parts[0]
	signatureSegment := parts[1]

	providedSignature, err := base64.RawURLEncoding.DecodeString(signatureSegment)
	if err != nil {
		return nil, ErrInvalidBridgeToken
	}

	mac := hmac.New(sha256.New, s.sharedSecret)
	if _, err := mac.Write([]byte(payloadSegment)); err != nil {
		return nil, ErrInvalidBridgeToken
	}
	expectedSignature := mac.Sum(nil)
	if !hmac.Equal(providedSignature, expectedSignature) {
		return nil, ErrInvalidBridgeToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadSegment)
	if err != nil {
		return nil, ErrInvalidBridgeToken
	}

	var payload bridgeTokenPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, ErrInvalidBridgeToken
	}

	if payload.Issuer != s.issuer {
		return nil, ErrBridgeIssuerMismatch
	}
	if payload.Subject == "" || payload.Nonce == "" || payload.IssuedAt <= 0 || payload.ExpiresAt <= 0 {
		return nil, ErrInvalidBridgeToken
	}
	if time.Now().Unix() >= payload.ExpiresAt {
		return nil, ErrExpiredBridgeToken
	}

	return &BridgeClaims{
		Issuer:    payload.Issuer,
		Subject:   payload.Subject,
		IssuedAt:  payload.IssuedAt,
		ExpiresAt: payload.ExpiresAt,
		Nonce:     payload.Nonce,
		ReturnTo:  NormalizeBridgeReturnTo(payload.ReturnTo),
	}, nil
}

func NormalizeBridgeReturnTo(returnTo string) string {
	normalized := strings.TrimSpace(returnTo)
	if normalized == "" {
		return "/"
	}
	if !strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "//") {
		return "/"
	}
	return normalized
}
