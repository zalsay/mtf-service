package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fintrack-api/config"
	"fintrack-api/database"
	"fintrack-api/services"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func TestPaidPredictOnceReturns402WithoutCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewWatchlistHandler(services.NewWatchlistService(nil, &config.Config{
		AlipayService: config.AlipayServiceConfig{
			MerchantID:   "merchant-1",
			MerchantName: "FinTrack",
		},
	}))
	router := gin.New()
	router.POST("/paid", handler.TriggerPaidMTFPredictOnce)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/paid", strings.NewReader(`{"stock_code":"000002"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "payment required" {
		t.Fatalf("error = %#v", body["error"])
	}
}

func TestPaidPredictOnceVerifiesCredentialAndForwards(t *testing.T) {
	var alipayCredential string
	alipay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/payments/verify" {
			t.Fatalf("alipay path = %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode alipay request: %v", err)
		}
		alipayCredential, _ = req["credential"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":true,"order_id":"order-1","resource_id":"mtf.predict.once"}`))
	}))
	defer alipay.Close()

	var gatewayPayload map[string]any
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict_once" {
			t.Fatalf("gateway path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gatewayPayload); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"job_id":"job-paid-once"}`))
	}))
	defer gateway.Close()

	cfg := &config.Config{
		InferenceGateway: config.InferenceGatewayConfig{
			BaseURL: gateway.URL,
			Timeout: 1,
		},
		AlipayService: config.AlipayServiceConfig{
			BaseURL:      alipay.URL,
			ResourceID:   "mtf.predict.once",
			ResourceName: "MTF 单次预测",
			AmountCents:  199,
			Currency:     "CNY",
			MerchantID:   "merchant-1",
			MerchantName: "FinTrack",
		},
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectExec("INSERT INTO ai_payment_records").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT fulfillment_status, response_status, response_body").
		WillReturnRows(sqlmock.NewRows([]string{"fulfillment_status", "response_status", "response_body"}).
			AddRow("processing", nil, nil))
	mock.ExpectExec("UPDATE ai_payment_records").
		WillReturnResult(sqlmock.NewResult(0, 1))

	handler := NewWatchlistHandler(services.NewWatchlistService(&database.DB{Conn: db}, cfg))
	router := gin.New()
	router.POST("/paid", handler.TriggerPaidMTFPredictOnce)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/paid", strings.NewReader(`{"stock_code":"000002","horizon_len":7,"context_len":256}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Alipay-AI-Pay paid-token")
	req.Header.Set("X-Alipay-Order-Id", "order-1")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if alipayCredential != "paid-token" {
		t.Fatalf("credential = %q", alipayCredential)
	}
	if gatewayPayload["stock_code"] != "000002" {
		t.Fatalf("stock_code = %#v", gatewayPayload["stock_code"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestPaidPredictOnceReturns402ForInvalidCredential(t *testing.T) {
	alipay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":false,"message":"unpaid"}`))
	}))
	defer alipay.Close()

	calledGateway := false
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledGateway = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	cfg := &config.Config{
		InferenceGateway: config.InferenceGatewayConfig{
			BaseURL: gateway.URL,
			Timeout: 1,
		},
		AlipayService: config.AlipayServiceConfig{
			BaseURL:      alipay.URL,
			ResourceID:   "mtf.predict.once",
			ResourceName: "MTF 单次预测",
			AmountCents:  199,
			Currency:     "CNY",
			MerchantID:   "merchant-1",
			MerchantName: "FinTrack",
		},
	}
	handler := NewWatchlistHandler(services.NewWatchlistService(nil, cfg))
	router := gin.New()
	router.POST("/paid", handler.TriggerPaidMTFPredictOnce)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/paid", strings.NewReader(`{"stock_code":"000002"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Alipay-AI-Pay invalid-token")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402; body=%s", rec.Code, rec.Body.String())
	}
	if calledGateway {
		t.Fatal("gateway service was called for invalid payment credential")
	}
}

func TestPaidPredictOnceReturnsStoredResultWithinSamePeriod(t *testing.T) {
	alipay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":true,"order_id":"order-1","resource_id":"mtf.predict.once"}`))
	}))
	defer alipay.Close()

	gatewayCalled := false
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectExec("INSERT INTO ai_payment_records").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT fulfillment_status, response_status, response_body").
		WillReturnRows(sqlmock.NewRows([]string{"fulfillment_status", "response_status", "response_body"}).
			AddRow("fulfilled", 200, `{"success":true,"job_id":"stored-job"}`))

	cfg := &config.Config{
		InferenceGateway: config.InferenceGatewayConfig{
			BaseURL: gateway.URL,
			Timeout: 1,
		},
		AlipayService: config.AlipayServiceConfig{
			BaseURL:      alipay.URL,
			ResourceID:   "mtf.predict.once",
			ResourceName: "MTF 单次预测",
			AmountCents:  199,
			Currency:     "CNY",
			MerchantID:   "merchant-1",
			MerchantName: "FinTrack",
		},
	}
	handler := NewWatchlistHandler(services.NewWatchlistService(&database.DB{Conn: db}, cfg))
	router := gin.New()
	router.POST("/paid", handler.TriggerPaidMTFPredictOnce)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/paid", strings.NewReader(`{"stock_code":"000002","horizon_len":7,"context_len":256}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Alipay-AI-Pay paid-token")
	req.Header.Set("X-Alipay-Order-Id", "order-1")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "stored-job") {
		t.Fatalf("body = %s, want stored job response", rec.Body.String())
	}
	if gatewayCalled {
		t.Fatal("gateway service was called for stored paid prediction")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestPaidPredictOnceReturns409WhenSamePeriodIsProcessing(t *testing.T) {
	alipay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":true,"order_id":"order-1","resource_id":"mtf.predict.once"}`))
	}))
	defer alipay.Close()

	gatewayCalled := false
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectExec("INSERT INTO ai_payment_records").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT fulfillment_status, response_status, response_body").
		WillReturnRows(sqlmock.NewRows([]string{"fulfillment_status", "response_status", "response_body"}).
			AddRow("processing", nil, nil))

	cfg := &config.Config{
		InferenceGateway: config.InferenceGatewayConfig{
			BaseURL: gateway.URL,
			Timeout: 1,
		},
		AlipayService: config.AlipayServiceConfig{
			BaseURL:      alipay.URL,
			ResourceID:   "mtf.predict.once",
			ResourceName: "MTF 单次预测",
			AmountCents:  199,
			Currency:     "CNY",
			MerchantID:   "merchant-1",
			MerchantName: "FinTrack",
		},
	}
	handler := NewWatchlistHandler(services.NewWatchlistService(&database.DB{Conn: db}, cfg))
	router := gin.New()
	router.POST("/paid", handler.TriggerPaidMTFPredictOnce)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/paid", strings.NewReader(`{"stock_code":"000002","horizon_len":7,"context_len":256}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Alipay-AI-Pay paid-token")
	req.Header.Set("X-Alipay-Order-Id", "order-1")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if gatewayCalled {
		t.Fatal("gateway service was called for processing paid prediction")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
