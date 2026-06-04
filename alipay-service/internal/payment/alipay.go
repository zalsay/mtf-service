package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type AlipayVerifier struct {
	credentialAPIURL  string
	fulfillmentAPIURL string
	appID             string
	httpClient        *http.Client
}

func NewAlipayVerifier(cfg Config) *AlipayVerifier {
	return &AlipayVerifier{
		credentialAPIURL:  strings.TrimSpace(cfg.CredentialAPIURL),
		fulfillmentAPIURL: strings.TrimSpace(cfg.FulfillmentAPIURL),
		appID:             strings.TrimSpace(cfg.AppID),
		httpClient:        &http.Client{Timeout: 10 * time.Second},
	}
}

func (v *AlipayVerifier) Verify(ctx context.Context, credential string, req VerifyRequest) (VerifyResult, error) {
	if v.credentialAPIURL == "" {
		return VerifyResult{}, fmt.Errorf("ALIPAY_CREDENTIAL_API_URL is required in alipay mode")
	}
	payload := map[string]string{
		"app_id":      v.appID,
		"credential":  strings.TrimSpace(credential),
		"resource_id": req.ResourceID,
		"order_id":    req.OrderID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return VerifyResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, v.credentialAPIURL, bytes.NewReader(raw))
	if err != nil {
		return VerifyResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := v.httpClient.Do(httpReq)
	if err != nil {
		return VerifyResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return VerifyResult{Valid: false, Message: fmt.Sprintf("credential API status %d", resp.StatusCode)}, nil
	}
	var result VerifyResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return VerifyResult{}, err
	}
	if result.Valid && v.fulfillmentAPIURL != "" {
		if err := v.confirmFulfillment(ctx, result); err != nil {
			return VerifyResult{}, err
		}
	}
	return result, nil
}

func (v *AlipayVerifier) confirmFulfillment(ctx context.Context, result VerifyResult) error {
	payload := map[string]string{
		"app_id":      v.appID,
		"order_id":    result.OrderID,
		"resource_id": result.ResourceID,
		"trade_no":    result.TradeNo,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.fulfillmentAPIURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("fulfillment API status %d", resp.StatusCode)
	}
	return nil
}
