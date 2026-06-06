package models

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type JobStatus string

const (
	JobQueued                    JobStatus = "queued"
	JobRunning                   JobStatus = "running"
	JobSucceeded                 JobStatus = "succeeded"
	JobFailed                    JobStatus = "failed"
	JobKindInference                       = "inference"
	JobKindUZI                             = "uzi"
	JobQueuePriorityBackground             = "background"
	BackendRoleMain                        = "main"
	BackendRoleXReg                        = "xreg"
	BackendRoleUZI                         = "uzi"
	CovariatePresetMarketV1                = "market_cov_v1"
	LegacyCovariatePresetMTFXReg           = "market_cov_v1_mtf_xreg"
	XRegModeMTFPlusXReg                    = "mtf + xreg"
	XRegModeXRegPlusMTF                    = "xreg + mtf"
	PredictionTypeMTFLite                  = "mtf-lite"
	PredictionTypeMTFPro                   = "mtf-pro"
	LegacyPredictionTypeNonCov             = "non_cov"
	LegacyPredictionTypeCov                = "cov"
)

type InferenceRequest struct {
	StockCode             string `json:"stock_code"`
	StockType             any    `json:"stock_type,omitempty"`
	TimeStep              any    `json:"time_step,omitempty"`
	Years                 any    `json:"years,omitempty"`
	StartDate             any    `json:"start_date,omitempty"`
	EndDate               any    `json:"end_date,omitempty"`
	HorizonLen            any    `json:"horizon_len,omitempty"`
	ContextLen            any    `json:"context_len,omitempty"`
	UserID                any    `json:"user_id,omitempty"`
	ForceEnqueue          any    `json:"force_enqueue,omitempty"`
	QueuePriority         string `json:"queue_priority,omitempty"`
	RefreshReason         string `json:"refresh_reason,omitempty"`
	PredictionTypeValue   string `json:"prediction_type,omitempty"`
	CovariatePreset       string `json:"covariate_preset,omitempty"`
	CovariateConfig       any    `json:"covariate_config,omitempty"`
	Covariates            any    `json:"covariates,omitempty"`
	BestMaxAgeDays        any    `json:"best_max_age_days,omitempty"`
	PredictFromBestValEnd any    `json:"predict_from_best_val_end,omitempty"`
	ChunkUntilLatest      any    `json:"chunk_until_latest,omitempty"`
}

type UZIAnalyzeRequest struct {
	Ticker       string         `json:"ticker"`
	Depth        string         `json:"depth,omitempty"`
	NoResume     any            `json:"no_resume,omitempty"`
	ForceEnqueue any            `json:"force_enqueue,omitempty"`
	AIModel      map[string]any `json:"ai_model,omitempty"`
}

type uziRequestKeyPayload struct {
	Ticker       string `json:"ticker"`
	Depth        string `json:"depth"`
	NoResume     bool   `json:"no_resume,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	ModelID      string `json:"model_id,omitempty"`
}

func (r UZIAnalyzeRequest) RequestKey() (string, error) {
	payload := uziRequestKeyPayload{
		Ticker:   strings.ToUpper(strings.TrimSpace(r.Ticker)),
		Depth:    normalizeUZIDepth(r.Depth),
		NoResume: normalizeBoolValue(r.NoResume, false),
	}
	if r.AIModel != nil {
		payload.ProviderName = normalizedMapString(r.AIModel, "provider_name")
		payload.BaseURL = strings.TrimRight(normalizedMapString(r.AIModel, "base_url"), "/")
		payload.ModelID = normalizedMapString(r.AIModel, "model_id")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (r UZIAnalyzeRequest) ForceEnqueueEnabled() bool {
	return normalizeBoolValue(r.ForceEnqueue, false)
}

type requestKeyPayload struct {
	StockCode             string `json:"stock_code"`
	StockType             any    `json:"stock_type"`
	TimeStep              int    `json:"time_step"`
	Years                 int    `json:"years"`
	StartDate             string `json:"start_date,omitempty"`
	EndDate               string `json:"end_date,omitempty"`
	HorizonLen            int    `json:"horizon_len"`
	ContextLen            int    `json:"context_len"`
	PredictionType        string `json:"prediction_type"`
	CovariatePreset       string `json:"covariate_preset,omitempty"`
	CovariateSignature    string `json:"covariate_signature,omitempty"`
	RefreshReason         string `json:"refresh_reason,omitempty"`
	BestMaxAgeDays        int    `json:"best_max_age_days,omitempty"`
	PredictFromBestValEnd bool   `json:"predict_from_best_val_end,omitempty"`
	ChunkUntilLatest      bool   `json:"chunk_until_latest,omitempty"`
}

func (r InferenceRequest) RequestKey() (string, error) {
	covariateConfig, covariatePreset := CanonicalizeCovariateRouting(r.effectiveCovariateConfig(), r.CovariatePreset)
	payload := requestKeyPayload{
		StockCode:             strings.TrimSpace(r.StockCode),
		StockType:             normalizeStockType(r.StockType),
		TimeStep:              normalizeIntValue(r.TimeStep, 0),
		Years:                 normalizeIntValue(r.Years, 15),
		StartDate:             normalizeDateValue(r.StartDate),
		EndDate:               normalizeDateValue(r.EndDate),
		HorizonLen:            normalizeIntValue(r.HorizonLen, 7),
		ContextLen:            normalizeIntValue(r.ContextLen, 2048),
		PredictionType:        r.PredictionType(),
		CovariatePreset:       covariatePreset,
		CovariateSignature:    normalizedCovariateSignature(covariateConfig),
		RefreshReason:         strings.TrimSpace(r.RefreshReason),
		BestMaxAgeDays:        normalizeIntValue(r.BestMaxAgeDays, 0),
		PredictFromBestValEnd: normalizeBoolValue(r.PredictFromBestValEnd, false),
		ChunkUntilLatest:      normalizeBoolValue(r.ChunkUntilLatest, false),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (r InferenceRequest) PredictionType() string {
	if explicit := NormalizePredictionType(r.PredictionTypeValue); explicit != "" {
		return explicit
	}
	covariateConfig, _ := CanonicalizeCovariateRouting(r.effectiveCovariateConfig(), r.CovariatePreset)
	return predictionTypeFromCovariateConfig(covariateConfig)
}

func (r InferenceRequest) CovariateSignature() string {
	covariateConfig, _ := CanonicalizeCovariateRouting(r.effectiveCovariateConfig(), r.CovariatePreset)
	return normalizedCovariateSignature(covariateConfig)
}

func (r InferenceRequest) NormalizedCovariatePreset() string {
	_, covariatePreset := CanonicalizeCovariateRouting(r.effectiveCovariateConfig(), r.CovariatePreset)
	return covariatePreset
}

func (r InferenceRequest) ForceEnqueueEnabled() bool {
	return normalizeBoolValue(r.ForceEnqueue, false)
}

func (r InferenceRequest) BackgroundPriorityEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(r.QueuePriority), JobQueuePriorityBackground)
}

func NormalizeForceEnqueueValue(value any) bool {
	return normalizeBoolValue(value, false)
}

func RequestKeyFromBody(body []byte) (string, error) {
	var request InferenceRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&request); err != nil {
		return "", err
	}
	return request.RequestKey()
}

func (r InferenceRequest) effectiveCovariateConfig() any {
	if r.CovariateConfig != nil {
		return r.CovariateConfig
	}
	return r.Covariates
}

func CovariatePresetSupportsXPUSplit(preset string) bool {
	switch strings.TrimSpace(preset) {
	case "", CovariatePresetMarketV1:
		return true
	default:
		return false
	}
}

func predictionTypeFromCovariateConfig(raw any) string {
	if covariatesEnabled(raw) {
		return PredictionTypeMTFPro
	}
	return PredictionTypeMTFLite
}

func NormalizePredictionType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case PredictionTypeMTFLite, "mtf_lite", LegacyPredictionTypeNonCov, "non-cov", "lite":
		return PredictionTypeMTFLite
	case PredictionTypeMTFPro, "mtf_pro", LegacyPredictionTypeCov, "pro":
		return PredictionTypeMTFPro
	default:
		return strings.TrimSpace(value)
	}
}

func PredictionTypeUsesCovariates(value string) bool {
	return NormalizePredictionType(value) == PredictionTypeMTFPro
}

func normalizedCovariateSignature(raw any) string {
	if !covariatesEnabled(raw) {
		return ""
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	sum := sha1.Sum(body)
	return hex.EncodeToString(sum[:])[:16]
}

func covariatesEnabled(raw any) bool {
	switch typed := raw.(type) {
	case nil:
		return false
	case bool:
		return typed
	case map[string]any:
		enabled, ok := typed["enabled"]
		if !ok {
			return true
		}
		return normalizeBoolValue(enabled, true)
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err != nil {
			return len(strings.TrimSpace(string(typed))) > 0
		}
		return covariatesEnabled(decoded)
	default:
		return true
	}
}

func CanonicalizeCovariateRouting(raw any, preset string) (any, string) {
	normalizedPreset := strings.TrimSpace(preset)
	if normalizedPreset == LegacyCovariatePresetMTFXReg {
		normalizedPreset = CovariatePresetMarketV1
	}

	if !covariatesEnabled(raw) {
		return raw, normalizedPreset
	}
	if normalizedPreset == "" {
		normalizedPreset = CovariatePresetMarketV1
	}

	var config map[string]any
	switch typed := raw.(type) {
	case map[string]any:
		config = make(map[string]any, len(typed)+1)
		for key, value := range typed {
			config[key] = value
		}
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err != nil {
			return raw, normalizedPreset
		}
		return CanonicalizeCovariateRouting(decoded, normalizedPreset)
	default:
		return raw, normalizedPreset
	}

	if normalizedPreset == CovariatePresetMarketV1 {
		if rawMode, ok := config["xreg_mode"]; ok {
			mode := strings.TrimSpace(fmt.Sprintf("%v", rawMode))
			if mode == "" || mode == XRegModeMTFPlusXReg {
				config["xreg_mode"] = XRegModeXRegPlusMTF
			}
		}
	}

	return config, normalizedPreset
}

type Job struct {
	ID                 string          `json:"job_id"`
	JobKind            string          `json:"job_kind,omitempty"`
	Status             JobStatus       `json:"status"`
	StockCode          string          `json:"stock_code"`
	PredictionType     string          `json:"prediction_type,omitempty"`
	CovariateSignature string          `json:"covariate_signature,omitempty"`
	CovariatePreset    string          `json:"covariate_preset,omitempty"`
	ForceEnqueue       bool            `json:"force_enqueue,omitempty"`
	QueuePriority      string          `json:"queue_priority,omitempty"`
	CurrentStage       string          `json:"current_stage,omitempty"`
	TargetPath         string          `json:"target_path,omitempty"`
	RequestKey         string          `json:"request_key,omitempty"`
	RequestBody        json.RawMessage `json:"request_body,omitempty"`
	ResultBody         json.RawMessage `json:"result,omitempty"`
	Backend            string          `json:"backend,omitempty"`
	UpstreamStatus     int             `json:"upstream_status,omitempty"`
	Error              string          `json:"error,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	StartedAt          *time.Time      `json:"started_at,omitempty"`
	FinishedAt         *time.Time      `json:"finished_at,omitempty"`
}

type BackendSnapshot struct {
	Name              string `json:"name"`
	Role              string `json:"role,omitempty"`
	URL               string `json:"url"`
	Capacity          int    `json:"capacity"`
	InFlight          int    `json:"in_flight"`
	Available         int    `json:"available"`
	SupportsCov       bool   `json:"supports_mtf_pro,omitempty"`
	SupportsDirectCov bool   `json:"supports_direct_cov,omitempty"`
	SupportsNonCov    bool   `json:"supports_mtf_lite,omitempty"`
	SupportsUZI       bool   `json:"supports_uzi,omitempty"`
}

type SchedulerSnapshot struct {
	QueueDepth int               `json:"queue_depth"`
	Backends   []BackendSnapshot `json:"backends"`
	Jobs       map[string]int    `json:"jobs"`
}

func normalizeStockType(value any) any {
	if value == nil {
		return 1
	}
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 1
		}
		if intValue, err := strconv.Atoi(trimmed); err == nil {
			return intValue
		}
		return trimmed
	case json.Number:
		if intValue, err := typed.Int64(); err == nil {
			return int(intValue)
		}
		return typed.String()
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	default:
		return fmt.Sprintf("%v", value)
	}
}

func normalizeUZIDepth(value string) string {
	switch strings.TrimSpace(value) {
	case "lite", "medium", "deep":
		return strings.TrimSpace(value)
	default:
		return "medium"
	}
}

func normalizedMapString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func normalizeIntValue(value any, fallback int) int {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case json.Number:
		if intValue, err := typed.Int64(); err == nil {
			return int(intValue)
		}
		if floatValue, err := typed.Float64(); err == nil {
			return int(floatValue)
		}
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case string:
		if intValue, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return intValue
		}
	}
	return fallback
}

func normalizeBoolValue(value any, fallback bool) bool {
	switch typed := value.(type) {
	case nil:
		return fallback
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		default:
			return fallback
		}
	case json.Number:
		if intValue, err := typed.Int64(); err == nil {
			return intValue != 0
		}
		if floatValue, err := typed.Float64(); err == nil {
			return floatValue != 0
		}
	case float64:
		return typed != 0
	case float32:
		return typed != 0
	case int:
		return typed != 0
	case int8:
		return typed != 0
	case int16:
		return typed != 0
	case int32:
		return typed != 0
	case int64:
		return typed != 0
	case uint:
		return typed != 0
	case uint8:
		return typed != 0
	case uint16:
		return typed != 0
	case uint32:
		return typed != 0
	case uint64:
		return typed != 0
	}
	return fallback
}

func normalizeDateValue(value any) string {
	if value == nil {
		return ""
	}

	raw := strings.TrimSpace(fmt.Sprintf("%v", value))
	if raw == "" {
		return ""
	}

	layouts := []string{"20060102", "2006-01-02", time.RFC3339, "2006-01-02 15:04:05"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.Format("20060102")
		}
	}
	return raw
}
