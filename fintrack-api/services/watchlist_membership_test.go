package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fintrack-api/config"
	"fintrack-api/models"
)

func TestNormalizeMTFBestTrainRequestLevel0AllowsOnlyFixedNonCov(t *testing.T) {
	req := &models.MTFBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "non_cov",
		HorizonLen:     7,
		ContextLen:     256,
	}

	normalized, err := NormalizeMTFBestTrainRequest(req, 0, 12, false)
	if err != nil {
		t.Fatalf("expected level 0 request to pass, got error: %v", err)
	}
	if normalized.UserID == nil || *normalized.UserID != 12 {
		t.Fatalf("expected user_id to be injected")
	}
	if normalized.CovariateConfig != nil {
		t.Fatalf("expected non_cov request to keep covariate_config nil")
	}
	if normalized.ForceEnqueue != nil {
		t.Fatalf("expected non-admin request to leave force_enqueue empty")
	}
}

func TestNormalizeTrainPredictionTypeUsesMtfNames(t *testing.T) {
	tests := map[string]string{
		"":         "mtf-lite",
		"non_cov":  "mtf-lite",
		"cov":      "mtf-pro",
		"mtf_lite": "mtf-lite",
		"mtf-lite": "mtf-lite",
		"mtf_pro":  "mtf-pro",
		"mtf-pro":  "mtf-pro",
	}

	for input, want := range tests {
		if got := normalizeTrainPredictionType(input); got != want {
			t.Fatalf("normalizeTrainPredictionType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeMTFBestTrainRequestMtfProBuildsCovariates(t *testing.T) {
	req := &models.MTFBestTrainRequest{
		StockCode:      "510300",
		StockType:      2,
		PredictionType: "mtf-pro",
		HorizonLen:     7,
		ContextLen:     256,
	}

	normalized, err := NormalizeMTFBestTrainRequest(req, 1, 88, false)
	if err != nil {
		t.Fatalf("NormalizeMTFBestTrainRequest() error = %v", err)
	}
	if normalized.PredictionType != "mtf-pro" {
		t.Fatalf("PredictionType = %q, want mtf-pro", normalized.PredictionType)
	}
	if normalized.CovariateConfig == nil {
		t.Fatal("expected mtf-pro request to produce covariate_config")
	}
}

func TestTriggerMTFPredictDoesNotForwardMTFVersion(t *testing.T) {
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict_for_best" {
			t.Fatalf("expected /predict_for_best path, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	service := NewWatchlistService(nil, &config.Config{
		InferenceGateway: config.InferenceGatewayConfig{
			BaseURL: server.URL,
			Timeout: 1,
		},
	})
	horizonLen := 7
	contextLen := 256
	status, _, err := service.TriggerMTFPredict(&models.MTFPredictRequest{
		StockCode:      "510300",
		StockType:      2,
		PredictionType: "mtf-lite",
		HorizonLen:     &horizonLen,
		ContextLen:     &contextLen,
	})
	if err != nil {
		t.Fatalf("TriggerMTFPredict error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if _, exists := payload["mtf_version"]; exists {
		t.Fatalf("payload must not include mtf_version: %#v", payload["mtf_version"])
	}
	if payload["prediction_type"] != "mtf-lite" {
		t.Fatalf("prediction_type = %#v, want mtf-lite", payload["prediction_type"])
	}
}

func TestTriggerStaleMTFBestRefreshSubmitsBackgroundGatewayJob(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict_for_best" {
			t.Fatalf("expected /predict_for_best path, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"job_id":"job-refresh","status":"queued"}`))
	}))
	defer server.Close()

	service := NewWatchlistService(nil, &config.Config{
		InferenceGateway: config.InferenceGatewayConfig{
			BaseURL: server.URL,
			Timeout: 1,
		},
	})
	item := models.MTFBestPrediction{
		Symbol:         "510050",
		StockType:      2,
		MTFVersion:     "2.5",
		PredictionType: "non_cov",
		ContextLen:     2048,
		HorizonLen:     7,
		UpdatedAt:      time.Now().AddDate(0, 0, -181),
	}

	if err := service.triggerStaleMTFBestRefresh(item); err != nil {
		t.Fatalf("expected stale refresh submission to pass, got error: %v", err)
	}

	if received["stock_code"] != "510050" {
		t.Fatalf("stock_code = %#v, want 510050", received["stock_code"])
	}
	if received["stock_type"] != float64(2) {
		t.Fatalf("stock_type = %#v, want 2", received["stock_type"])
	}
	if received["queue_priority"] != "background" {
		t.Fatalf("queue_priority = %#v, want background", received["queue_priority"])
	}
	if received["refresh_reason"] != "stale_180d" {
		t.Fatalf("refresh_reason = %#v, want stale_180d", received["refresh_reason"])
	}
}

func TestNormalizeMTFBestTrainRequestLevel0RejectsCov(t *testing.T) {
	req := &models.MTFBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "cov",
		HorizonLen:     7,
		ContextLen:     256,
	}

	if _, err := NormalizeMTFBestTrainRequest(req, 0, 12, false); err == nil {
		t.Fatal("expected cov request to be rejected for membership level 0")
	}
}

func TestNormalizeMTFBestTrainRequestTemporarilyAllowsAnyHorizon(t *testing.T) {
	req := &models.MTFBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "non_cov",
		HorizonLen:     3,
		ContextLen:     256,
	}

	if _, err := NormalizeMTFBestTrainRequest(req, 1, 12, false); err != nil {
		t.Fatalf("expected horizon_len to bypass membership validation temporarily, got error: %v", err)
	}
}

func TestNormalizeMTFBestTrainRequestLevel1AcceptsCovAndBuildsCovariates(t *testing.T) {
	req := &models.MTFBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "cov",
		HorizonLen:     14,
		ContextLen:     512,
	}

	normalized, err := NormalizeMTFBestTrainRequest(req, 1, 23, true)
	if err != nil {
		t.Fatalf("expected level 1 cov request to pass, got error: %v", err)
	}
	if normalized.CovariateConfig == nil {
		t.Fatal("expected cov request to produce covariate_config")
	}
	if enabled, ok := normalized.CovariateConfig["enabled"].(bool); !ok || !enabled {
		t.Fatalf("expected covariate_config.enabled=true, got %#v", normalized.CovariateConfig["enabled"])
	}
	if mode, ok := normalized.CovariateConfig["xreg_mode"].(string); !ok || mode != "mtf + xreg" {
		t.Fatalf("expected xreg_mode to be injected, got %#v", normalized.CovariateConfig["xreg_mode"])
	}
	if normalized.CovariatePreset == nil || *normalized.CovariatePreset != "market_cov_v1" {
		t.Fatalf("expected covariate_preset to be injected, got %#v", normalized.CovariatePreset)
	}
	if normalized.ForceEnqueue == nil || !*normalized.ForceEnqueue {
		t.Fatalf("expected admin request to inject force_enqueue=true")
	}
}

func TestNormalizeMTFBestTrainRequestRejectsContextOutsideMembershipLimit(t *testing.T) {
	req := &models.MTFBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "cov",
		HorizonLen:     7,
		ContextLen:     2048,
	}

	if _, err := NormalizeMTFBestTrainRequest(req, 2, 45, false); err == nil {
		t.Fatal("expected context length above level 2 limit to be rejected")
	}
}

func TestNormalizeMTFPredictOnceRequestAdminInjectsForceRequeue(t *testing.T) {
	req := &models.MTFPredictRequest{
		StockCode: "000001",
		StockType: 1,
	}

	normalized, err := NormalizeMTFPredictOnceRequest(req, 3, 99, true)
	if err != nil {
		t.Fatalf("expected admin predict once request to pass, got error: %v", err)
	}
	if normalized.UserID == nil || *normalized.UserID != 99 {
		t.Fatalf("expected user_id to be injected")
	}
	if normalized.ForceEnqueue == nil || !*normalized.ForceEnqueue {
		t.Fatalf("expected admin request to inject force_enqueue=true")
	}
	if normalized.ForceRequeue == nil || !*normalized.ForceRequeue {
		t.Fatalf("expected admin request to inject force_requeue=true")
	}
}

func TestNormalizeMTFPredictOnceRequestNonAdminDoesNotForceRequeue(t *testing.T) {
	force := true
	req := &models.MTFPredictRequest{
		StockCode:    "000001",
		StockType:    1,
		ForceEnqueue: &force,
		ForceRequeue: &force,
	}

	normalized, err := NormalizeMTFPredictOnceRequest(req, 0, 88, false)
	if err != nil {
		t.Fatalf("expected non-admin predict once request to pass, got error: %v", err)
	}
	if normalized.UserID == nil || *normalized.UserID != 88 {
		t.Fatalf("expected user_id to be injected")
	}
	if normalized.ForceEnqueue != nil {
		t.Fatalf("expected non-admin request to clear force_enqueue")
	}
	if normalized.ForceRequeue != nil {
		t.Fatalf("expected non-admin request to clear force_requeue")
	}
}

func TestNormalizeMTFPredictOnceRequestTemporarilyAllowsAnyHorizon(t *testing.T) {
	horizonLen := 3
	contextLen := 512
	req := &models.MTFPredictRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "cov",
		HorizonLen:     &horizonLen,
		ContextLen:     &contextLen,
	}

	if _, err := NormalizeMTFPredictOnceRequest(req, 1, 88, false); err != nil {
		t.Fatalf("expected horizon_len to bypass membership validation temporarily, got error: %v", err)
	}
}

func TestTriggerMTFPredictOnceSendsForceRequeueAlias(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict_once" {
			t.Fatalf("expected /predict_once path, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"job_id":"job-test","status":"queued"}`))
	}))
	defer server.Close()

	service := NewWatchlistService(nil, &config.Config{
		InferenceGateway: config.InferenceGatewayConfig{
			BaseURL: server.URL,
			Timeout: 1,
		},
	})
	force := true
	req := &models.MTFPredictRequest{
		StockCode:    "000001",
		ForceEnqueue: &force,
		ForceRequeue: &force,
	}

	status, _, err := service.TriggerMTFPredictOnce(req)
	if err != nil {
		t.Fatalf("expected predict once proxy to pass, got error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if received["force_enqueue"] != true {
		t.Fatalf("expected force_enqueue=true in payload, got %#v", received["force_enqueue"])
	}
	if received["force_requeue"] != true {
		t.Fatalf("expected force_requeue=true in payload, got %#v", received["force_requeue"])
	}
}

func TestGetMTFPredictOnceCachedQueriesPostgresHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/save-predictions/mtf-direct/by-request" {
			t.Fatalf("path = %s, want /api/v1/save-predictions/mtf-direct/by-request", r.URL.Path)
		}
		if r.Header.Get("X-Token") != "test-token" {
			t.Fatalf("X-Token = %q, want test-token", r.Header.Get("X-Token"))
		}
		query := r.URL.Query()
		if query.Get("symbol") != "510050" {
			t.Fatalf("symbol query = %q, want 510050", query.Get("symbol"))
		}
		if query.Get("stock_type") != "2" {
			t.Fatalf("stock_type query = %q, want 2", query.Get("stock_type"))
		}
		if query.Get("horizon_len") != "7" || query.Get("context_len") != "2048" {
			t.Fatalf("unexpected horizon/context query: %s", r.URL.RawQuery)
		}
		if query.Get("prediction_type") != "mtf-pro" {
			t.Fatalf("prediction_type query = %q, want mtf-pro", query.Get("prediction_type"))
		}
		if query.Has("mtf_version") {
			t.Fatalf("query must not include mtf_version: %s", r.URL.RawQuery)
		}
		if query.Get("covariate_signature") != "sig123" {
			t.Fatalf("covariate_signature query = %q, want sig123", query.Get("covariate_signature"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"message": "Success",
			"data": {
				"stock_code": "510050",
				"future_dates": ["2026-01-01"],
				"best_prediction_item": "mtf-0.5",
				"best_prediction_values": [1.23],
				"predictions": {"mtf-0.5": [1.23]},
				"covariate_signature": "sig123"
			}
		}`))
	}))
	defer server.Close()

	service := NewWatchlistService(nil, &config.Config{
		PostgresHandler: config.PostgresHandlerConfig{
			BaseURL:  server.URL,
			APIToken: "test-token",
			Timeout:  2,
		},
	})

	horizonLen := 7
	contextLen := 2048
	status, body, err := service.GetMTFPredictOnceCached(&models.MTFPredictRequest{
		StockCode:          "sh510050",
		StockType:          "etf",
		PredictionType:     "mtf-pro",
		HorizonLen:         &horizonLen,
		ContextLen:         &contextLen,
		CovariateSignature: "sig123",
	})
	if err != nil {
		t.Fatalf("GetMTFPredictOnceCached returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["success"] != true {
		t.Fatalf("success = %#v, want true", body["success"])
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data = %#v, want object", body["data"])
	}
	if data["cache_hit"] != true {
		t.Fatalf("cache_hit = %#v, want true", data["cache_hit"])
	}
}
