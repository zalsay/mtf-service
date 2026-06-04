package payment

import (
	"context"
	"time"
)

type Config struct {
	Mode              string
	LocalSecret       string
	LocalTTL          time.Duration
	CredentialAPIURL  string
	FulfillmentAPIURL string
	AppID             string
	PrivateKey        string
	AlipayPublicKey   string
}

type VerifyRequest struct {
	ResourceID string `json:"resource_id"`
	OrderID    string `json:"order_id"`
}

type VerifyResult struct {
	Valid      bool   `json:"valid"`
	OrderID    string `json:"order_id,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`
	TradeNo    string `json:"trade_no,omitempty"`
	Message    string `json:"message,omitempty"`
}

type Verifier interface {
	Verify(ctx context.Context, credential string, req VerifyRequest) (VerifyResult, error)
}
