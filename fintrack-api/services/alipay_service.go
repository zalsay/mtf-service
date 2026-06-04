package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fintrack-api/config"
)

type AlipayVerifyRequest struct {
	Credential string `json:"credential"`
	ResourceID string `json:"resource_id"`
	OrderID    string `json:"order_id"`
}

type AlipayVerifyResult struct {
	Valid      bool   `json:"valid"`
	OrderID    string `json:"order_id"`
	ResourceID string `json:"resource_id"`
	TradeNo    string `json:"trade_no"`
	Message    string `json:"message"`
}

type AlipayServiceClient struct {
	cfg        config.AlipayServiceConfig
	httpClient *http.Client
}

func NewAlipayServiceClient(cfg config.AlipayServiceConfig) *AlipayServiceClient {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10
	}
	return &AlipayServiceClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: time.Duration(timeout) * time.Second},
	}
}

func (c *AlipayServiceClient) Verify(ctx context.Context, req AlipayVerifyRequest) (AlipayVerifyResult, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.cfg.BaseURL), "/")
	if baseURL == "" {
		return AlipayVerifyResult{}, fmt.Errorf("ALIPAY_SERVICE_URL is required")
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return AlipayVerifyResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/payments/verify", bytes.NewReader(raw))
	if err != nil {
		return AlipayVerifyResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(c.cfg.APIToken); token != "" {
		httpReq.Header.Set("X-Internal-Token", token)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return AlipayVerifyResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AlipayVerifyResult{Valid: false, Message: fmt.Sprintf("alipay-service status %d", resp.StatusCode)}, nil
	}
	var result AlipayVerifyResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return AlipayVerifyResult{}, err
	}
	return result, nil
}
