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

func TestNormalizeTimesfmBestTrainRequestLevel0AllowsOnlyFixedNonCov(t *testing.T) {
	req := &models.TimesfmBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "non_cov",
		HorizonLen:     7,
		ContextLen:     256,
	}

	normalized, err := NormalizeTimesfmBestTrainRequest(req, 0, 12, false)
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

func TestTriggerStaleTimesfmBestRefreshSubmitsBackgroundGatewayJob(t *testing.T) {
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
		PythonService: config.PythonServiceConfig{
			BaseURL: server.URL,
			Timeout: 1,
		},
	})
	item := models.TimesfmBestPrediction{
		Symbol:         "510050",
		StockType:      2,
		TimesfmVersion: "2.5",
		PredictionType: "non_cov",
		ContextLen:     2048,
		HorizonLen:     7,
		UpdatedAt:      time.Now().AddDate(0, 0, -181),
	}

	if err := service.triggerStaleTimesfmBestRefresh(item); err != nil {
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

func TestNormalizeTimesfmBestTrainRequestLevel0RejectsCov(t *testing.T) {
	req := &models.TimesfmBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "cov",
		HorizonLen:     7,
		ContextLen:     256,
	}

	if _, err := NormalizeTimesfmBestTrainRequest(req, 0, 12, false); err == nil {
		t.Fatal("expected cov request to be rejected for membership level 0")
	}
}

func TestNormalizeTimesfmBestTrainRequestTemporarilyAllowsAnyHorizon(t *testing.T) {
	req := &models.TimesfmBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "non_cov",
		HorizonLen:     3,
		ContextLen:     256,
	}

	if _, err := NormalizeTimesfmBestTrainRequest(req, 1, 12, false); err != nil {
		t.Fatalf("expected horizon_len to bypass membership validation temporarily, got error: %v", err)
	}
}

func TestNormalizeTimesfmBestTrainRequestLevel1AcceptsCovAndBuildsCovariates(t *testing.T) {
	req := &models.TimesfmBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "cov",
		HorizonLen:     14,
		ContextLen:     512,
	}

	normalized, err := NormalizeTimesfmBestTrainRequest(req, 1, 23, true)
	if err != nil {
		t.Fatalf("expected level 1 cov request to pass, got error: %v", err)
	}
	if normalized.CovariateConfig == nil {
		t.Fatal("expected cov request to produce covariate_config")
	}
	if enabled, ok := normalized.CovariateConfig["enabled"].(bool); !ok || !enabled {
		t.Fatalf("expected covariate_config.enabled=true, got %#v", normalized.CovariateConfig["enabled"])
	}
	if mode, ok := normalized.CovariateConfig["xreg_mode"].(string); !ok || mode != "timesfm + xreg" {
		t.Fatalf("expected xreg_mode to be injected, got %#v", normalized.CovariateConfig["xreg_mode"])
	}
	if normalized.CovariatePreset == nil || *normalized.CovariatePreset != "market_cov_v1" {
		t.Fatalf("expected covariate_preset to be injected, got %#v", normalized.CovariatePreset)
	}
	if normalized.ForceEnqueue == nil || !*normalized.ForceEnqueue {
		t.Fatalf("expected admin request to inject force_enqueue=true")
	}
}

func TestNormalizeTimesfmBestTrainRequestRejectsContextOutsideMembershipLimit(t *testing.T) {
	req := &models.TimesfmBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "cov",
		HorizonLen:     7,
		ContextLen:     2048,
	}

	if _, err := NormalizeTimesfmBestTrainRequest(req, 2, 45, false); err == nil {
		t.Fatal("expected context length above level 2 limit to be rejected")
	}
}

func TestNormalizeTimesfmPredictOnceRequestAdminInjectsForceRequeue(t *testing.T) {
	req := &models.TimesfmPredictRequest{
		StockCode: "000001",
		StockType: 1,
	}

	normalized, err := NormalizeTimesfmPredictOnceRequest(req, 3, 99, true)
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

func TestNormalizeTimesfmPredictOnceRequestNonAdminDoesNotForceRequeue(t *testing.T) {
	force := true
	req := &models.TimesfmPredictRequest{
		StockCode:    "000001",
		StockType:    1,
		ForceEnqueue: &force,
		ForceRequeue: &force,
	}

	normalized, err := NormalizeTimesfmPredictOnceRequest(req, 0, 88, false)
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

func TestNormalizeTimesfmPredictOnceRequestTemporarilyAllowsAnyHorizon(t *testing.T) {
	horizonLen := 3
	contextLen := 512
	req := &models.TimesfmPredictRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "cov",
		HorizonLen:     &horizonLen,
		ContextLen:     &contextLen,
	}

	if _, err := NormalizeTimesfmPredictOnceRequest(req, 1, 88, false); err != nil {
		t.Fatalf("expected horizon_len to bypass membership validation temporarily, got error: %v", err)
	}
}

func TestTriggerTimesfmPredictOnceSendsForceRequeueAlias(t *testing.T) {
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
		PythonService: config.PythonServiceConfig{
			BaseURL: server.URL,
			Timeout: 1,
		},
	})
	force := true
	req := &models.TimesfmPredictRequest{
		StockCode:    "000001",
		ForceEnqueue: &force,
		ForceRequeue: &force,
	}

	status, _, err := service.TriggerTimesfmPredictOnce(req)
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

func TestGetTimesfmPredictOnceCachedCallsGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict_once_cached" {
			t.Fatalf("path = %s, want /predict_once_cached", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload["stock_code"] != "sh510050" {
			t.Fatalf("stock_code payload = %#v, want sh510050", payload["stock_code"])
		}
		if payload["stock_type"] != "etf" {
			t.Fatalf("stock_type payload = %#v, want etf", payload["stock_type"])
		}
		if payload["prediction_type"] != "cov" {
			t.Fatalf("prediction_type payload = %#v, want cov", payload["prediction_type"])
		}
		if payload["covariate_signature"] != "sig123" {
			t.Fatalf("covariate_signature payload = %#v, want sig123", payload["covariate_signature"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"stock_code": "sh510050",
			"message": "单次预测缓存命中",
			"data": {
				"stock_code": "sh510050",
				"future_dates": ["2026-01-01"],
				"best_prediction_item": "mtf-0.5",
				"best_prediction_values": [1.23],
				"predictions": {"mtf-0.5": [1.23]},
				"covariate_signature": "sig123",
				"cache_hit": true
			}
		}`))
	}))
	defer server.Close()

	service := NewWatchlistService(nil, &config.Config{
		PythonService: config.PythonServiceConfig{
			BaseURL: server.URL,
			Timeout: 2,
		},
	})

	horizonLen := 7
	contextLen := 2048
	version := "2.5"
	status, body, err := service.GetTimesfmPredictOnceCached(&models.TimesfmPredictRequest{
		StockCode:          "sh510050",
		StockType:          "etf",
		PredictionType:     "cov",
		HorizonLen:         &horizonLen,
		ContextLen:         &contextLen,
		TimesfmVersion:     &version,
		CovariateSignature: "sig123",
	})
	if err != nil {
		t.Fatalf("GetTimesfmPredictOnceCached returned error: %v", err)
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
