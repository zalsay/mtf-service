package models

import (
	"strings"
	"testing"
)

func TestRequestKeyIncludesPredictionTypeAndCovariateSignature(t *testing.T) {
	nonCov := InferenceRequest{
		StockCode:  "sh510050",
		StockType:  2,
		Years:      1,
		HorizonLen: 7,
		ContextLen: 2048,
	}
	cov := InferenceRequest{
		StockCode:       "sh510050",
		StockType:       2,
		Years:           1,
		HorizonLen:      7,
		ContextLen:      2048,
		CovariateConfig: map[string]any{"enabled": true, "dynamic_numerical_columns": []any{"volume"}},
	}

	nonCovKey, err := nonCov.RequestKey()
	if err != nil {
		t.Fatalf("non-cov RequestKey() error: %v", err)
	}
	covKey, err := cov.RequestKey()
	if err != nil {
		t.Fatalf("cov RequestKey() error: %v", err)
	}

	if nonCovKey == covKey {
		t.Fatalf("expected different request keys for cov and non-cov requests, got identical key %q", covKey)
	}
	if want := `"prediction_type":"non_cov"`; !strings.Contains(nonCovKey, want) {
		t.Fatalf("expected non-cov request key to include %s, got %s", want, nonCovKey)
	}
	if want := `"prediction_type":"cov"`; !strings.Contains(covKey, want) {
		t.Fatalf("expected cov request key to include %s, got %s", want, covKey)
	}
	if !strings.Contains(covKey, `"covariate_signature":"`) {
		t.Fatalf("expected cov request key to include covariate_signature, got %s", covKey)
	}
}

func TestRequestKeyTreatsCovariatesAliasAsEquivalent(t *testing.T) {
	bodyWithConfig := []byte(`{
		"stock_code": "sh510050",
		"stock_type": 2,
		"years": 1,
		"horizon_len": 7,
		"context_len": 2048,
		"covariate_config": {"enabled": true, "dynamic_numerical_columns": ["volume"]}
	}`)
	bodyWithAlias := []byte(`{
		"stock_code": "sh510050",
		"stock_type": 2,
		"years": 1,
		"horizon_len": 7,
		"context_len": 2048,
		"covariates": {"enabled": true, "dynamic_numerical_columns": ["volume"]}
	}`)

	configKey, err := RequestKeyFromBody(bodyWithConfig)
	if err != nil {
		t.Fatalf("RequestKeyFromBody(bodyWithConfig) error: %v", err)
	}
	aliasKey, err := RequestKeyFromBody(bodyWithAlias)
	if err != nil {
		t.Fatalf("RequestKeyFromBody(bodyWithAlias) error: %v", err)
	}

	if configKey != aliasKey {
		t.Fatalf("expected identical request keys for covariate_config and covariates alias, got %s != %s", configKey, aliasKey)
	}
}

func TestRequestKeyIncludesCovariatePreset(t *testing.T) {
	withPreset := InferenceRequest{
		StockCode:       "510050",
		StockType:       2,
		Years:           15,
		HorizonLen:      7,
		ContextLen:      2048,
		CovariateConfig: map[string]any{"enabled": true},
		CovariatePreset: "market_cov_v1",
	}
	withoutPreset := InferenceRequest{
		StockCode:       "510050",
		StockType:       2,
		Years:           15,
		HorizonLen:      7,
		ContextLen:      2048,
		CovariateConfig: map[string]any{"enabled": true},
	}

	withPresetKey, err := withPreset.RequestKey()
	if err != nil {
		t.Fatalf("withPreset RequestKey() error: %v", err)
	}
	withoutPresetKey, err := withoutPreset.RequestKey()
	if err != nil {
		t.Fatalf("withoutPreset RequestKey() error: %v", err)
	}

	if withPresetKey != withoutPresetKey {
		t.Fatalf("expected explicit standard preset and implicit default preset to share request key, got %s != %s", withPresetKey, withoutPresetKey)
	}
	if want := `"covariate_preset":"market_cov_v1"`; !strings.Contains(withPresetKey, want) {
		t.Fatalf("expected request key to include %s, got %s", want, withPresetKey)
	}
}

func TestRequestKeyCanonicalizesLegacyTimesFMXRegPreset(t *testing.T) {
	legacy := InferenceRequest{
		StockCode:       "510050",
		StockType:       2,
		Years:           15,
		HorizonLen:      7,
		ContextLen:      2048,
		CovariateConfig: map[string]any{"enabled": true, "xreg_mode": "timesfm + xreg"},
		CovariatePreset: "market_cov_v1_timesfm_xreg",
	}
	canonical := InferenceRequest{
		StockCode:       "510050",
		StockType:       2,
		Years:           15,
		HorizonLen:      7,
		ContextLen:      2048,
		CovariateConfig: map[string]any{"enabled": true, "xreg_mode": "xreg + timesfm"},
		CovariatePreset: "market_cov_v1",
	}

	legacyKey, err := legacy.RequestKey()
	if err != nil {
		t.Fatalf("legacy RequestKey() error: %v", err)
	}
	canonicalKey, err := canonical.RequestKey()
	if err != nil {
		t.Fatalf("canonical RequestKey() error: %v", err)
	}
	if legacyKey != canonicalKey {
		t.Fatalf("expected legacy and canonical cov requests to share request key, got %s != %s", legacyKey, canonicalKey)
	}
	if want := `"covariate_preset":"market_cov_v1"`; !strings.Contains(legacyKey, want) {
		t.Fatalf("expected canonicalized request key to include %s, got %s", want, legacyKey)
	}
	if strings.Contains(legacyKey, "market_cov_v1_timesfm_xreg") {
		t.Fatalf("expected legacy preset name to be removed from request key, got %s", legacyKey)
	}
	if strings.Contains(legacyKey, `"timesfm + xreg"`) {
		t.Fatalf("expected legacy xreg_mode to be removed from request key, got %s", legacyKey)
	}
}

func TestForceEnqueueEnabledNormalizesTruthyValues(t *testing.T) {
	tests := []InferenceRequest{
		{ForceEnqueue: true},
		{ForceEnqueue: "true"},
		{ForceEnqueue: "1"},
	}

	for index, request := range tests {
		if !request.ForceEnqueueEnabled() {
			t.Fatalf("expected truthy force_enqueue to normalize to true for case %d", index)
		}
	}

	if (InferenceRequest{}).ForceEnqueueEnabled() {
		t.Fatalf("expected empty force_enqueue to default to false")
	}
}

func TestInferenceRequestBackgroundPriorityAndRefreshKey(t *testing.T) {
	userRequest := InferenceRequest{
		StockCode:  "600186",
		StockType:  "stock",
		HorizonLen: 7,
		ContextLen: 2048,
	}
	refreshRequest := InferenceRequest{
		StockCode:     "600186",
		StockType:     "stock",
		HorizonLen:    7,
		ContextLen:    2048,
		QueuePriority: "background",
		RefreshReason: "stale_180d",
	}

	userKey, err := userRequest.RequestKey()
	if err != nil {
		t.Fatalf("user request key error: %v", err)
	}
	refreshKey, err := refreshRequest.RequestKey()
	if err != nil {
		t.Fatalf("refresh request key error: %v", err)
	}
	if userKey == refreshKey {
		t.Fatalf("expected stale refresh request key to differ from user request key")
	}
	if !refreshRequest.BackgroundPriorityEnabled() {
		t.Fatalf("expected background queue priority to be enabled")
	}
}
