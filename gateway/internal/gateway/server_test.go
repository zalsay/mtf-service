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

func TestInferenceTimeEstimatorUsesPredictionType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "benchmarks.json")
	if err := os.WriteFile(path, []byte(`{
		"benchmarks": [
			{
				"generated_at": "2026-05-20T16:10:04+08:00",
				"backend": "rocm",
				"prediction_type": "non_cov",
				"results": [
					{"context_len": 1024, "horizon_len": 14, "estimated_inference_time_sec": 15.7}
				]
			},
			{
				"generated_at": "2026-05-20T16:28:59+08:00",
				"backend": "xpu",
				"prediction_type": "cov",
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
		ContextLen: 1024,
		HorizonLen: 14,
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

func TestNormalizeInferencePayloadCanonicalizesLegacyCovPreset(t *testing.T) {
	body := []byte(`{
		"stock_code": "510300",
		"stock_type": 2,
		"years": 15,
		"horizon_len": 7,
		"context_len": 2048,
		"covariate_preset": "market_cov_v1_timesfm_xreg",
		"covariate_config": {
			"enabled": true,
			"xreg_mode": "timesfm + xreg",
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
		TimesFMVersion:  request["timesfm_version"],
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
	if got := strings.TrimSpace(config["xreg_mode"].(string)); got != "xreg + timesfm" {
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
	if got := strings.TrimSpace(cfg["xreg_mode"].(string)); got != "xreg + timesfm" {
		t.Fatalf("expected normalized body xreg_mode to be canonical, got %q", got)
	}
	if _, exists := normalized["covariates"]; exists {
		t.Fatalf("expected normalized body to drop covariates alias, got %#v", normalized["covariates"])
	}
}

func TestNormalizeInferencePayloadForwardsForceEnqueueToBackendBody(t *testing.T) {
	body := []byte(`{
		"stock_code": "000001",
		"stock_type": 1,
		"years": 15,
		"horizon_len": 7,
		"context_len": 2048,
		"timesfm_version": "2.5",
		"force_enqueue": "true"
	}`)

	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	normalizedBody, normalizedRequest, err := normalizeInferencePayload(body, models.InferenceRequest{
		StockCode:      request["stock_code"].(string),
		StockType:      request["stock_type"],
		Years:          request["years"],
		HorizonLen:     request["horizon_len"],
		ContextLen:     request["context_len"],
		TimesFMVersion: request["timesfm_version"],
		ForceEnqueue:   request["force_enqueue"],
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
