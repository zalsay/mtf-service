package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-functions/internal/models"
)

func TestPredictOnceCachedReadsOnlyPredictionCache(t *testing.T) {
	var requestedPaths []string
	handler := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		if r.Header.Get("X-Token") != "test-token" {
			t.Fatalf("X-Token = %q, want test-token", r.Header.Get("X-Token"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/save-predictions/mtf-direct/by-request":
			if got := r.URL.Query().Get("covariate_signature"); got != "sig123" {
				t.Fatalf("prediction cache covariate_signature = %q, want sig123", got)
			}
			_, _ = w.Write([]byte(`{
				"code":200,
				"message":"Success",
				"data":{
					"unique_key":"300442_direct_st_1_hlen_7_clen_2048_fd_test_cov_sig123",
					"stock_code":"300442",
					"stock_type":1,
					"prediction_type":"mtf-pro",
					"mtf_version":"2.5",
					"context_len":2048,
					"horizon_len":7,
					"latest_data_date":"2026-06-08",
					"latest_close":74.11,
					"future_dates":["2999-01-01","2999-01-02"],
					"best_prediction_item":"mtf-0.7",
					"best_prediction_values":[80.1,80.2],
					"predictions":{"mtf-0.7":[80.1,80.2]},
					"covariate_signature":"sig123",
					"covariate_analysis":{"future_covariate_source":{"strategy":"prediction_once"}}
				}
			}`))
		default:
			t.Fatalf("unexpected postgres handler path %s", r.URL.Path)
		}
	}))
	defer handler.Close()

	server := NewServerWithOptions(nil, ServerOptions{
		PostgresHandlerURL:   handler.URL,
		PostgresHandlerToken: "test-token",
	})
	body := strings.NewReader(`{
		"stock_code":"300442",
		"stock_type":"stock",
		"years":15,
		"horizon_len":7,
		"context_len":2048,
		"prediction_type":"mtf-pro",
		"predict_date":"2999-01-01",
		"covariate_preset":"market_cov_v1",
		"covariate_signature":"sig123"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/predict_once_cached", body)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data := response["data"].(map[string]any)
	if data["cache_hit"] != true {
		t.Fatalf("cache_hit = %#v, want true", data["cache_hit"])
	}
	if data["covariate_signature"] != "sig123" {
		t.Fatalf("covariate_signature = %#v", data["covariate_signature"])
	}
	if data["latest_close"] != float64(74.11) {
		t.Fatalf("latest_close = %#v, want prediction cache latest close", data["latest_close"])
	}
	values := data["best_prediction_values"].([]any)
	if len(values) != 2 || values[0] != float64(80.1) || values[1] != float64(80.2) {
		t.Fatalf("best_prediction_values = %#v", values)
	}
	if len(requestedPaths) != 1 {
		t.Fatalf("postgres handler paths = %#v, want only prediction cache request", requestedPaths)
	}
}

func TestPredictOnceCachedUsesPredictDateForFreshness(t *testing.T) {
	handler := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/save-predictions/mtf-direct/by-request" {
			t.Fatalf("unexpected postgres handler path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("predict_date"); got != "2026-06-02" {
			t.Fatalf("prediction cache predict_date = %q, want 2026-06-02", got)
		}
		_, _ = w.Write([]byte(`{
			"code":200,
			"message":"Success",
			"data":{
				"unique_key":"300442_direct_st_1_hlen_7_clen_2048_fd_test",
				"stock_code":"300442",
				"stock_type":1,
				"prediction_type":"mtf-lite",
				"mtf_version":"2.5",
				"context_len":2048,
				"horizon_len":7,
				"latest_data_date":"2026-06-01",
				"future_dates":["2026-06-02","2026-06-03"],
				"best_prediction_item":"mtf-0.7",
				"best_prediction_values":[80.1,80.2],
				"predictions":{"mtf-0.7":[80.1,80.2]}
			}
		}`))
	}))
	defer handler.Close()

	server := NewServerWithOptions(nil, ServerOptions{PostgresHandlerURL: handler.URL})
	body := strings.NewReader(`{
		"stock_code":"300442",
		"stock_type":"stock",
		"horizon_len":7,
		"context_len":2048,
		"prediction_type":"mtf-lite",
		"predict_date":"2026-06-02"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/predict_once_cached", body)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPredictOnceCachedRequiresPredictDateInFutureWindow(t *testing.T) {
	handler := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("predict_date") != "2026-06-02" {
			t.Fatalf("prediction cache predict_date = %q, want 2026-06-02", r.URL.Query().Get("predict_date"))
		}
		_, _ = w.Write([]byte(`{
			"code":200,
			"message":"Success",
			"data":{
				"future_dates":["2026-06-03","2026-06-04"],
				"latest_data_date":"2026-06-01"
			}
		}`))
	}))
	defer handler.Close()

	server := NewServerWithOptions(nil, ServerOptions{PostgresHandlerURL: handler.URL})
	body := strings.NewReader(`{
		"stock_code":"300442",
		"stock_type":"stock",
		"horizon_len":7,
		"context_len":2048,
		"prediction_type":"mtf-lite",
		"predict_date":"2026-06-02"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/predict_once_cached", body)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "prediction cache not found") {
		t.Fatalf("body = %s, want prediction cache not found", rec.Body.String())
	}
}

func TestPredictOnceCachedDoesNotFallbackToValidationChunk(t *testing.T) {
	var requestedPaths []string
	handler := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/save-predictions/mtf-direct/by-request" {
			t.Fatalf("unexpected fallback path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer handler.Close()

	server := NewServerWithOptions(nil, ServerOptions{
		PostgresHandlerURL:   handler.URL,
		PostgresHandlerToken: "test-token",
	})
	body := strings.NewReader(`{
		"stock_code":"300442",
		"stock_type":"stock",
		"horizon_len":7,
		"context_len":2048,
		"prediction_type":"mtf-pro",
		"covariate_signature":"sig123"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/predict_once_cached", body)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(requestedPaths) != 1 {
		t.Fatalf("postgres handler paths = %#v, want only prediction cache request", requestedPaths)
	}
}

func TestInferenceTimeEstimatorUsesPredictionType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "benchmarks.json")
	if err := os.WriteFile(path, []byte(`{
		"benchmarks": [
			{
				"generated_at": "2026-05-20T16:10:04+08:00",
				"backend": "rocm",
				"prediction_type": "mtf-lite",
				"results": [
					{"context_len": 1024, "horizon_len": 14, "estimated_inference_time_sec": 15.7}
				]
			},
			{
				"generated_at": "2026-05-20T16:28:59+08:00",
				"backend": "xpu",
				"prediction_type": "mtf-pro",
				"results": [
					{"context_len": 1024, "horizon_len": 14, "estimated_inference_time_sec": 8.0}
				]
			}
		]
	}`), 0o644); err != nil {
		t.Fatalf("write benchmark fixture: %v", err)
	}

	estimator, err := loadInferenceTimeEstimator(path)
	if err != nil {
		t.Fatalf("loadInferenceTimeEstimator() error: %v", err)
	}
	server := &Server{timeEstimator: estimator}

	nonCovEstimate, ok := server.estimateInferenceTime(models.InferenceRequest{
		ContextLen:          1024,
		HorizonLen:          14,
		PredictionTypeValue: "mtf-lite",
	}, "/internal/predict_for_best_sync")
	if !ok || nonCovEstimate.EstimatedInferenceTimeSec != 15.7 {
		t.Fatalf("expected non_cov estimate 15.7, got %#v ok=%t", nonCovEstimate, ok)
	}

	covEstimate, ok := server.estimateInferenceTime(models.InferenceRequest{
		ContextLen:      1024,
		HorizonLen:      14,
		CovariateConfig: map[string]any{"enabled": true},
	}, "/internal/predict_for_best_sync")
	if !ok || covEstimate.EstimatedInferenceTimeSec != 8.0 {
		t.Fatalf("expected cov estimate 8.0, got %#v ok=%t", covEstimate, ok)
	}
}

func TestNormalizeInferencePayloadDefaultsToMTFPro(t *testing.T) {
	body := []byte(`{
		"stock_code": "510300",
		"stock_type": 2,
		"years": 15,
		"horizon_len": 7,
		"context_len": 2048
	}`)

	var request models.InferenceRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	normalizedBody, normalizedRequest, err := normalizeInferencePayload(body, request, "/internal/predict_for_best_sync")
	if err != nil {
		t.Fatalf("normalizeInferencePayload() error: %v", err)
	}
	if normalizedRequest.PredictionType() != "mtf-pro" {
		t.Fatalf("expected default prediction type mtf-pro, got %q", normalizedRequest.PredictionType())
	}

	var normalized map[string]any
	if err := json.Unmarshal(normalizedBody, &normalized); err != nil {
		t.Fatalf("decode normalized body: %v", err)
	}
	if normalized["prediction_type"] != "mtf-pro" {
		t.Fatalf("expected normalized body prediction_type=mtf-pro, got %#v", normalized["prediction_type"])
	}
}

func TestNormalizeInferencePayloadUsesPythonDefaultsForBestRuntimeOptions(t *testing.T) {
	body := []byte(`{
		"stock_code": "510300",
		"prediction_type": "mtf-pro",
		"pos_json_path": "/tmp/custom-pos.json",
		"model_path": "/tmp/custom-model",
		"validation_chunks_target": 9,
		"max_selection_chunks": 10,
		"max_validation_chunks": 11,
		"per_core_batch_size": 12,
		"chunk_batch_size": 13,
		"torch_compile": false,
		"use_tdx_start_date": false
	}`)

	var request models.InferenceRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	normalizedBody, _, err := normalizeInferencePayload(body, request, "/internal/predict_for_best_sync")
	if err != nil {
		t.Fatalf("normalizeInferencePayload() error: %v", err)
	}

	var normalized map[string]any
	if err := json.Unmarshal(normalizedBody, &normalized); err != nil {
		t.Fatalf("decode normalized body: %v", err)
	}
	for _, field := range []string{
		"pos_json_path",
		"model_path",
		"validation_chunks_target",
		"max_selection_chunks",
		"max_validation_chunks",
		"per_core_batch_size",
		"chunk_batch_size",
		"torch_compile",
		"use_tdx_start_date",
	} {
		if _, exists := normalized[field]; exists {
			t.Fatalf("expected best payload to omit Python runtime override %q", field)
		}
	}
}

func TestNormalizeInferencePayloadCanonicalizesLegacyCovPreset(t *testing.T) {
	body := []byte(`{
		"stock_code": "510300",
		"stock_type": 2,
		"years": 15,
		"horizon_len": 7,
		"context_len": 2048,
		"covariate_preset": "market_cov_v1_mtf_xreg",
		"covariate_config": {
			"enabled": true,
			"xreg_mode": "mtf + xreg",
			"run_tag": "legacy"
		}
	}`)

	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	normalizedBody, normalizedRequest, err := normalizeInferencePayload(body, models.InferenceRequest{
		StockCode:       request["stock_code"].(string),
		StockType:       request["stock_type"],
		TimeStep:        request["time_step"],
		Years:           request["years"],
		StartDate:       request["start_date"],
		EndDate:         request["end_date"],
		HorizonLen:      request["horizon_len"],
		ContextLen:      request["context_len"],
		UserID:          request["user_id"],
		CovariateConfig: request["covariate_config"],
		Covariates:      request["covariates"],
		CovariatePreset: func() string {
			if v, ok := request["covariate_preset"].(string); ok {
				return v
			}
			return ""
		}(),
	}, "/internal/predict_for_best_sync")
	if err != nil {
		t.Fatalf("normalizeInferencePayload() error: %v", err)
	}

	if normalizedRequest.CovariatePreset != "market_cov_v1" {
		t.Fatalf("expected canonical covariate_preset, got %q", normalizedRequest.CovariatePreset)
	}
	config, ok := normalizedRequest.CovariateConfig.(map[string]any)
	if !ok {
		t.Fatalf("expected canonical covariate_config map, got %#v", normalizedRequest.CovariateConfig)
	}
	if got := strings.TrimSpace(config["xreg_mode"].(string)); got != "xreg + mtf" {
		t.Fatalf("expected canonical xreg_mode, got %q", got)
	}
	if _, hasAlias := normalizedRequest.Covariates.(map[string]any); hasAlias {
		t.Fatalf("expected covariates alias to be cleared after normalization")
	}

	var normalized map[string]any
	if err := json.Unmarshal(normalizedBody, &normalized); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	if normalized["covariate_preset"] != "market_cov_v1" {
		t.Fatalf("expected normalized body preset to be canonical, got %#v", normalized["covariate_preset"])
	}
	if got := normalized["covariate_signature"]; got != normalizedRequest.CovariateSignature() {
		t.Fatalf("expected normalized body covariate_signature=%q, got %#v", normalizedRequest.CovariateSignature(), got)
	}
	cfg := normalized["covariate_config"].(map[string]any)
	if got := strings.TrimSpace(cfg["xreg_mode"].(string)); got != "xreg + mtf" {
		t.Fatalf("expected normalized body xreg_mode to be canonical, got %q", got)
	}
	if _, exists := normalized["covariates"]; exists {
		t.Fatalf("expected normalized body to drop covariates alias, got %#v", normalized["covariates"])
	}
	if normalized["prediction_type"] != "mtf-pro" {
		t.Fatalf("expected normalized body prediction_type=mtf-pro, got %#v", normalized["prediction_type"])
	}
	if normalizedRequest.PredictionType() != "mtf-pro" {
		t.Fatalf("expected normalized request prediction type mtf-pro, got %q", normalizedRequest.PredictionType())
	}
}

func TestNormalizeInferencePayloadMapsLegacyPredictionTypeToMtfLite(t *testing.T) {
	body := []byte(`{
		"stock_code": "510300",
		"stock_type": 2,
		"horizon_len": 7,
		"context_len": 2048,
		"prediction_type": "mtf-lite"
	}`)

	var request models.InferenceRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	normalizedBody, normalizedRequest, err := normalizeInferencePayload(body, request, "/internal/predict_once_sync")
	if err != nil {
		t.Fatalf("normalizeInferencePayload() error: %v", err)
	}

	if normalizedRequest.PredictionType() != "mtf-lite" {
		t.Fatalf("expected normalized request prediction type mtf-lite, got %q", normalizedRequest.PredictionType())
	}
	var normalized map[string]any
	if err := json.Unmarshal(normalizedBody, &normalized); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	if normalized["prediction_type"] != "mtf-lite" {
		t.Fatalf("expected normalized body prediction_type=mtf-lite, got %#v", normalized["prediction_type"])
	}
}

func TestNormalizeInferencePayloadDoesNotInjectBestSyncDates(t *testing.T) {
	body := []byte(`{
		"stock_code": "510050",
		"stock_type": 2,
		"years": 15,
		"horizon_len": 7,
		"context_len": 2048,
		"prediction_type": "mtf-pro"
	}`)

	var request models.InferenceRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	normalizedBody, normalizedRequest, err := normalizeInferencePayload(body, request, "/internal/predict_for_best_sync")
	if err != nil {
		t.Fatalf("normalizeInferencePayload() error: %v", err)
	}

	if normalizedRequest.StartDate != nil {
		t.Fatalf("expected best sync start_date to remain empty, got %#v", normalizedRequest.StartDate)
	}
	if normalizedRequest.EndDate != nil {
		t.Fatalf("expected best sync end_date to remain empty, got %#v", normalizedRequest.EndDate)
	}

	var normalized map[string]any
	if err := json.Unmarshal(normalizedBody, &normalized); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	if _, exists := normalized["start_date"]; exists {
		t.Fatalf("expected normalized best sync body to omit start_date, got %#v", normalized["start_date"])
	}
	if _, exists := normalized["end_date"]; exists {
		t.Fatalf("expected normalized best sync body to omit end_date, got %#v", normalized["end_date"])
	}
}

func TestNormalizeInferencePayloadPreservesExplicitBestSyncDates(t *testing.T) {
	body := []byte(`{
		"stock_code": "510050",
		"stock_type": 2,
		"years": 15,
		"start_date": "2011-06-01",
		"end_date": "2026-06-01",
		"horizon_len": 7,
		"context_len": 2048,
		"prediction_type": "mtf-pro"
	}`)

	var request models.InferenceRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	normalizedBody, normalizedRequest, err := normalizeInferencePayload(body, request, "/internal/predict_for_best_sync")
	if err != nil {
		t.Fatalf("normalizeInferencePayload() error: %v", err)
	}

	if normalizedRequest.StartDate != "20110601" {
		t.Fatalf("expected explicit start_date to be normalized, got %#v", normalizedRequest.StartDate)
	}
	if normalizedRequest.EndDate != "20260601" {
		t.Fatalf("expected explicit end_date to be normalized, got %#v", normalizedRequest.EndDate)
	}

	var normalized map[string]any
	if err := json.Unmarshal(normalizedBody, &normalized); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	if normalized["start_date"] != "20110601" {
		t.Fatalf("expected normalized body start_date=20110601, got %#v", normalized["start_date"])
	}
	if normalized["end_date"] != "20260601" {
		t.Fatalf("expected normalized body end_date=20260601, got %#v", normalized["end_date"])
	}
}

func TestNormalizeInferencePayloadPreservesPredictDateAsFutureTarget(t *testing.T) {
	body := []byte(`{
		"stock_code": "510050",
		"stock_type": 2,
		"years": 15,
		"predict_date": "2026-06-02",
		"horizon_len": 7,
		"context_len": 2048,
		"prediction_type": "mtf-lite"
	}`)

	var request models.InferenceRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	normalizedBody, normalizedRequest, err := normalizeInferencePayload(body, request, "/internal/predict_once_sync")
	if err != nil {
		t.Fatalf("normalizeInferencePayload() error: %v", err)
	}

	if normalizedRequest.PredictDate != "20260602" {
		t.Fatalf("expected predict_date to be normalized, got %#v", normalizedRequest.PredictDate)
	}
	if normalizedRequest.EndDate != nil {
		t.Fatalf("expected predict_date not to become end_date, got %#v", normalizedRequest.EndDate)
	}
	if normalizedRequest.StartDate != nil {
		t.Fatalf("expected start_date to remain empty for predict_once, got %#v", normalizedRequest.StartDate)
	}

	var normalized map[string]any
	if err := json.Unmarshal(normalizedBody, &normalized); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	if normalized["predict_date"] != "20260602" {
		t.Fatalf("expected normalized body predict_date=20260602, got %#v", normalized["predict_date"])
	}
	if _, exists := normalized["end_date"]; exists {
		t.Fatalf("expected normalized body to omit auto-injected end_date, got %#v", normalized["end_date"])
	}
	if _, exists := normalized["start_date"]; exists {
		t.Fatalf("expected normalized body to omit auto-injected start_date, got %#v", normalized["start_date"])
	}
}

func TestNormalizeInferencePayloadExplicitEndDateOverridesPredictDate(t *testing.T) {
	body := []byte(`{
		"stock_code": "510050",
		"stock_type": 2,
		"years": 15,
		"predict_date": "2026-06-02",
		"end_date": "2026-06-01",
		"horizon_len": 7,
		"context_len": 2048,
		"prediction_type": "mtf-lite"
	}`)

	var request models.InferenceRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	normalizedBody, normalizedRequest, err := normalizeInferencePayload(body, request, "/internal/predict_once_sync")
	if err != nil {
		t.Fatalf("normalizeInferencePayload() error: %v", err)
	}

	if normalizedRequest.EndDate != "20260601" {
		t.Fatalf("expected explicit end_date to win, got %#v", normalizedRequest.EndDate)
	}
	if normalizedRequest.StartDate != nil {
		t.Fatalf("expected start_date to remain empty for predict_once, got %#v", normalizedRequest.StartDate)
	}

	var normalized map[string]any
	if err := json.Unmarshal(normalizedBody, &normalized); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	if normalized["predict_date"] != "20260602" {
		t.Fatalf("expected normalized body predict_date=20260602, got %#v", normalized["predict_date"])
	}
	if normalized["end_date"] != "20260601" {
		t.Fatalf("expected normalized body end_date=20260601, got %#v", normalized["end_date"])
	}
	if _, exists := normalized["start_date"]; exists {
		t.Fatalf("expected normalized body to omit auto-injected start_date, got %#v", normalized["start_date"])
	}
}

func TestNormalizeInferencePayloadDropsPredictOnceContinuationFields(t *testing.T) {
	body := []byte(`{
		"stock_code": "000001",
		"stock_type": 1,
		"years": 15,
		"horizon_len": 28,
		"context_len": 2048,
		"prediction_type": "mtf-lite",
		"best_max_age_days": 180,
		"predict_from_best_val_end": true,
		"chunk_until_latest": true
	}`)

	var request models.InferenceRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	normalizedBody, _, err := normalizeInferencePayload(body, request, "/internal/predict_once_sync")
	if err != nil {
		t.Fatalf("normalizeInferencePayload() error: %v", err)
	}

	var normalized map[string]any
	if err := json.Unmarshal(normalizedBody, &normalized); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	for _, key := range []string{"best_max_age_days", "predict_from_best_val_end", "chunk_until_latest"} {
		if _, ok := normalized[key]; ok {
			t.Fatalf("expected legacy continuation field %s to be dropped, got %#v", key, normalized[key])
		}
	}
}

func TestNormalizeInferencePayloadForwardsForceEnqueueToBackendBody(t *testing.T) {
	body := []byte(`{
		"stock_code": "000001",
		"stock_type": 1,
		"years": 15,
		"horizon_len": 7,
		"context_len": 2048,
		"mtf_version": "legacy-client-value",
		"force_enqueue": "true"
	}`)

	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	normalizedBody, normalizedRequest, err := normalizeInferencePayload(body, models.InferenceRequest{
		StockCode:    request["stock_code"].(string),
		StockType:    request["stock_type"],
		Years:        request["years"],
		HorizonLen:   request["horizon_len"],
		ContextLen:   request["context_len"],
		ForceEnqueue: request["force_enqueue"],
	}, "/internal/predict_for_best_sync")
	if err != nil {
		t.Fatalf("normalizeInferencePayload() error: %v", err)
	}

	if !normalizedRequest.ForceEnqueueEnabled() {
		t.Fatalf("expected normalized request to enable force_enqueue")
	}

	var normalized map[string]any
	if err := json.Unmarshal(normalizedBody, &normalized); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	if normalized["force_enqueue"] != true {
		t.Fatalf("expected normalized body to forward force_enqueue=true, got %#v", normalized["force_enqueue"])
	}
	if _, exists := normalized["mtf_version"]; exists {
		t.Fatalf("expected normalized body to omit mtf_version, got %#v", normalized["mtf_version"])
	}
}

func TestNormalizeUZIAnalyzePayloadDefaultsAndForwardsQueueFields(t *testing.T) {
	body := []byte(`{
		"ticker": " 601166.sh ",
		"no_resume": "true",
		"force_enqueue": "true",
		"ai_model": {
			"provider_name": "DeepSeek",
			"base_url": "https://api.deepseek.com/",
			"api_key": "secret",
			"model_id": "deepseek-chat"
		}
	}`)

	var request models.UZIAnalyzeRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	normalizedBody, normalizedRequest, err := normalizeUZIAnalyzePayload(body, request)
	if err != nil {
		t.Fatalf("normalizeUZIAnalyzePayload() error: %v", err)
	}

	if normalizedRequest.Ticker != "601166.SH" {
		t.Fatalf("expected ticker normalized to uppercase, got %q", normalizedRequest.Ticker)
	}
	if normalizedRequest.Depth != "medium" {
		t.Fatalf("expected missing depth to default to medium, got %q", normalizedRequest.Depth)
	}
	if !normalizedRequest.ForceEnqueueEnabled() {
		t.Fatalf("expected force_enqueue to be enabled")
	}

	var normalized map[string]any
	if err := json.Unmarshal(normalizedBody, &normalized); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	if normalized["ticker"] != "601166.SH" {
		t.Fatalf("expected normalized body ticker, got %#v", normalized["ticker"])
	}
	if normalized["depth"] != "medium" {
		t.Fatalf("expected normalized body depth=medium, got %#v", normalized["depth"])
	}
	if normalized["no_resume"] != true {
		t.Fatalf("expected normalized body no_resume=true, got %#v", normalized["no_resume"])
	}
	if normalized["force_enqueue"] != true {
		t.Fatalf("expected normalized body force_enqueue=true, got %#v", normalized["force_enqueue"])
	}
}

func TestEffectiveJobCovariateSignaturePrefersJobSignature(t *testing.T) {
	job := &models.Job{
		CovariateSignature: "gatewaysig123456",
		ResultBody:         []byte(`{"data":{"overall_metrics":{"covariate_signature":"pythonsig654321"}}}`),
	}

	if got := effectiveJobCovariateSignature(job); got != "gatewaysig123456" {
		t.Fatalf("expected job covariate signature to win, got %q", got)
	}
}

func TestGatewayRequiresTokenForProtectedRoutes(t *testing.T) {
	handler := NewServerWithOptions(nil, ServerOptions{APIToken: "gateway-secret"})

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing token to return 401, got %d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	wrongToken := httptest.NewRecorder()
	wrongRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	wrongRequest.Header.Set("X-API-Token", "wrong-token")
	handler.ServeHTTP(wrongToken, wrongRequest)
	if wrongToken.Code != http.StatusUnauthorized {
		t.Fatalf("expected wrong token to return 401, got %d body=%s", wrongToken.Code, wrongToken.Body.String())
	}

	authorized := httptest.NewRecorder()
	authorizedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	authorizedRequest.Header.Set("X-API-Token", "gateway-secret")
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("expected correct X-API-Token to return 200, got %d body=%s", authorized.Code, authorized.Body.String())
	}

	legacyAuthorized := httptest.NewRecorder()
	legacyRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	legacyRequest.Header.Set("X-Gateway-API-Token", "gateway-secret")
	handler.ServeHTTP(legacyAuthorized, legacyRequest)
	if legacyAuthorized.Code != http.StatusOK {
		t.Fatalf("expected correct X-Gateway-API-Token to return 200, got %d body=%s", legacyAuthorized.Code, legacyAuthorized.Body.String())
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("expected health check without token to return 200, got %d body=%s", health.Code, health.Body.String())
	}
}

func TestDeepSeekTUIProxyRequiresTokenAndStripsPrefix(t *testing.T) {
	var sawForwarded bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawForwarded = true
		if r.URL.Path != "/health" {
			t.Fatalf("expected stripped upstream path /health, got %q", r.URL.Path)
		}
		if r.URL.RawQuery != "x=1" {
			t.Fatalf("expected query to be preserved, got %q", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer user-deepseek-key" {
			t.Fatalf("expected user API key to be forwarded as Authorization, got %q", got)
		}
		if got := r.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("expected X-API-Key header to be stripped, got %q", got)
		}
		if got := r.Header.Get("X-API-Token"); got != "" {
			t.Fatalf("expected X-API-Token header to be stripped, got %q", got)
		}
		if got := r.Header.Get("X-Gateway-API-Token"); got != "" {
			t.Fatalf("expected X-Gateway-API-Token header to be stripped, got %q", got)
		}
		if got := r.Header.Get("X-DeepSeek-API-Key"); got != "" {
			t.Fatalf("expected X-DeepSeek-API-Key header to be stripped, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	handler := NewServerWithOptions(nil, ServerOptions{
		DeepSeekTUIBackendURL: upstream.URL,
		DeepSeekTUIProxyToken: "secret-token",
		DeepSeekTUIProxyPath:  "/deepseek-tui",
	})

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/deepseek-tui/health", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized request to return 401, got %d", unauthorized.Code)
	}
	if sawForwarded {
		t.Fatalf("unauthorized request should not reach upstream")
	}

	authorized := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/deepseek-tui/health?x=1", nil)
	req.Header.Set("X-Gateway-API-Token", "secret-token")
	req.Header.Set("X-API-Token", "secret-token")
	req.Header.Set("X-DeepSeek-API-Key", "user-deepseek-key")
	handler.ServeHTTP(authorized, req)
	if authorized.Code != http.StatusOK {
		t.Fatalf("expected authorized proxy request to return 200, got %d body=%s", authorized.Code, authorized.Body.String())
	}
	if !sawForwarded {
		t.Fatalf("expected authorized request to reach upstream")
	}
}

func TestDeepSeekTUIProxyWritesUserAPIKeyConfigAndWaitsForTurn(t *testing.T) {
	getThreadCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/threads/thr_123/turns":
			if got := r.Header.Get("Authorization"); got != "Bearer user-deepseek-key" {
				t.Fatalf("expected turn request Authorization header, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"turn":{"id":"turn_123","status":"in_progress"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/threads/thr_123":
			if got := r.Header.Get("Authorization"); got != "Bearer user-deepseek-key" {
				t.Fatalf("expected poll request Authorization header, got %q", got)
			}
			getThreadCount++
			w.Header().Set("Content-Type", "application/json")
			if getThreadCount == 1 {
				_, _ = w.Write([]byte(`{"turns":[{"id":"turn_123","status":"in_progress"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"turns":[{"id":"turn_123","status":"completed"}]}`))
		default:
			t.Fatalf("unexpected upstream request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer upstream.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	handler := NewServerWithOptions(nil, ServerOptions{
		DeepSeekTUIBackendURL:     upstream.URL,
		DeepSeekTUIProxyToken:     "secret-token",
		DeepSeekTUIProxyPath:      "/deepseek-tui",
		DeepSeekTUIAuthConfigPath: configPath,
	})

	req := httptest.NewRequest(http.MethodPost, "/deepseek-tui/v1/threads/thr_123/turns", strings.NewReader(`{"prompt":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gateway-API-Token", "secret-token")
	req.Header.Set("X-DeepSeek-API-Key", "user-deepseek-key")
	req.Header.Set("X-DeepSeek-Base-URL", "https://api.deepseek.com/v1")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected turn request to return 201, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if getThreadCount < 2 {
		t.Fatalf("expected proxy to poll thread until terminal status, got %d polls", getThreadCount)
	}
	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read auth config: %v", err)
	}
	configText := string(rawConfig)
	for _, want := range []string{`api_key = "user-deepseek-key"`, `base_url = "https://api.deepseek.com/v1"`} {
		if !strings.Contains(configText, want) {
			t.Fatalf("auth config missing %q: %s", want, configText)
		}
	}
}
