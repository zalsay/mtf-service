package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

type LocalVerifier struct {
	secret []byte
	ttl    time.Duration
}

type localCredential struct {
	OrderID    string `json:"order_id"`
	ResourceID string `json:"resource_id"`
	IssuedAt   int64  `json:"iat"`
	ExpiresAt  int64  `json:"exp"`
	Signature  string `json:"sig"`
}

func NewLocalVerifier(secret string, ttl time.Duration) *LocalVerifier {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &LocalVerifier{secret: []byte(secret), ttl: ttl}
}

func (v *LocalVerifier) IssueDevCredential(orderID string, resourceID string, now time.Time) (string, error) {
	cred := localCredential{
		OrderID:    strings.TrimSpace(orderID),
		ResourceID: strings.TrimSpace(resourceID),
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(v.ttl).Unix(),
	}
	cred.Signature = v.sign(cred.OrderID, cred.ResourceID, cred.IssuedAt, cred.ExpiresAt)
	raw, err := json.Marshal(cred)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (v *LocalVerifier) Verify(ctx context.Context, credential string, req VerifyRequest) (VerifyResult, error) {
	_ = ctx
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(credential))
	if err != nil {
		return VerifyResult{Valid: false, Message: "credential is not valid base64"}, nil
	}
	var cred localCredential
	if err := json.Unmarshal(raw, &cred); err != nil {
		return VerifyResult{Valid: false, Message: "credential is not valid JSON"}, nil
	}
	if strings.TrimSpace(req.OrderID) != "" && cred.OrderID != strings.TrimSpace(req.OrderID) {
		return VerifyResult{Valid: false, Message: "order_id mismatch"}, nil
	}
	if cred.ResourceID != strings.TrimSpace(req.ResourceID) {
		return VerifyResult{Valid: false, Message: "resource_id mismatch"}, nil
	}
	if time.Now().Unix() > cred.ExpiresAt {
		return VerifyResult{Valid: false, Message: "credential expired"}, nil
	}
	expected := v.sign(cred.OrderID, cred.ResourceID, cred.IssuedAt, cred.ExpiresAt)
	if !hmac.Equal([]byte(expected), []byte(cred.Signature)) {
		return VerifyResult{Valid: false, Message: "signature mismatch"}, nil
	}
	return VerifyResult{
		Valid:      true,
		OrderID:    cred.OrderID,
		ResourceID: cred.ResourceID,
	}, nil
}

func (v *LocalVerifier) sign(orderID string, resourceID string, issuedAt int64, expiresAt int64) string {
	mac := hmac.New(sha256.New, v.secret)
	_, _ = mac.Write([]byte(orderID))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(resourceID))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(time.Unix(issuedAt, 0).UTC().Format(time.RFC3339)))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(time.Unix(expiresAt, 0).UTC().Format(time.RFC3339)))
	return hex.EncodeToString(mac.Sum(nil))
}
