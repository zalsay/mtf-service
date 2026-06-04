package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"alipay-service/internal/payment"
)

func TestVerifyPaymentEndpointAcceptsValidCredential(t *testing.T) {
	verifier := payment.NewLocalVerifier("test-secret", time.Hour)
	handler := New(Config{
		MerchantID:   "merchant-1",
		MerchantName: "测试商户",
		ResourceID:   "mtf.predict.once",
		ResourceName: "MTF 单次预测",
		AmountCents:  199,
		Currency:     "CNY",
	}, verifier)
	credential, err := verifier.IssueDevCredential("order-1", "mtf.predict.once", time.Now())
	if err != nil {
		t.Fatalf("IssueDevCredential error = %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments/verify", strings.NewReader(`{"credential":"`+credential+`","resource_id":"mtf.predict.once","order_id":"order-1"}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body payment.VerifyResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Valid {
		t.Fatalf("valid = false, want true: %#v", body)
	}
}
