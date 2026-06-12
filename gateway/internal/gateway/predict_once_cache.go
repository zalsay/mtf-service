package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ai-functions/internal/models"
)

const predictionCacheDateLayout = "2006-01-02"

type postgresHandlerResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

func (s *Server) lookupPredictOnceCache(ctx context.Context, request models.InferenceRequest) (int, map[string]any, error) {
	if strings.TrimSpace(s.postgresHandlerURL) == "" {
		return http.StatusServiceUnavailable, nil, fmt.Errorf("postgres handler is not configured")
	}

	if status, result, err := s.lookupPredictionCache(ctx, request); err != nil || status != http.StatusNotFound {
		return status, result, err
	}

	return http.StatusNotFound, predictOnceCacheNotFoundBody(request.StockCode, "未找到单次预测缓存", "prediction cache not found"), nil
}

func (s *Server) lookupPredictionCache(ctx context.Context, request models.InferenceRequest) (int, map[string]any, error) {
	query := url.Values{}
	query.Set("symbol", storageSymbol(request.StockCode))
	query.Set("stock_type", strconv.Itoa(stockTypeInt(request.StockType)))
	query.Set("horizon_len", strconv.Itoa(intValue(request.HorizonLen, 7)))
	query.Set("context_len", strconv.Itoa(intValue(request.ContextLen, 2048)))
	query.Set("prediction_type", request.PredictionType())
	query.Set("mtf_version", "2.5")
	if signature := strings.TrimSpace(request.CovariateSignature()); signature != "" {
		query.Set("covariate_signature", signature)
	}

	status, data, err := s.getPostgresHandlerData(ctx, "/api/v1/save-predictions/mtf-direct/by-request", query)
	if err != nil || status == http.StatusNotFound {
		return status, nil, err
	}
	if status < 200 || status >= 300 {
		return status, nil, fmt.Errorf("postgres prediction cache returned status %d", status)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return http.StatusBadGateway, nil, fmt.Errorf("decode postgres prediction cache data: %w", err)
	}
	freshAt := time.Now().UTC()
	if predictDate := modelsNormalizeDateOrDefault(request.PredictDate, time.Time{}); predictDate != "" {
		if parsed, err := time.ParseInLocation("20060102", predictDate, time.UTC); err == nil {
			freshAt = parsed
		}
	}
	if !isPredictionCacheFresh(payload, freshAt) {
		return http.StatusNotFound, predictOnceCacheNotFoundBody(request.StockCode, "单次预测缓存已过期", "prediction cache stale"), nil
	}
	payload["cache_hit"] = true
	return http.StatusOK, map[string]any{
		"success":    true,
		"stock_code": request.StockCode,
		"message":    "单次预测缓存命中",
		"data":       payload,
	}, nil
}

func (s *Server) getPostgresHandlerData(ctx context.Context, path string, query url.Values) (int, json.RawMessage, error) {
	requestURL := s.postgresHandlerURL + path
	if encoded := query.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, nil, err
	}
	if s.postgresHandlerToken != "" {
		httpReq.Header.Set("X-Token", s.postgresHandlerToken)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return resp.StatusCode, nil, nil
	}
	var decoded postgresHandlerResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp.StatusCode, nil, fmt.Errorf("decode postgres handler response: %w", err)
		}
		return resp.StatusCode, nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(decoded.Error)
		if message == "" {
			message = strings.TrimSpace(decoded.Message)
		}
		if message == "" {
			message = strings.TrimSpace(string(body))
		}
		return resp.StatusCode, nil, errors.New(message)
	}
	return resp.StatusCode, decoded.Data, nil
}

func predictOnceCacheNotFoundBody(stockCode, message, errorCode string) map[string]any {
	return map[string]any{
		"success":    false,
		"stock_code": stockCode,
		"message":    message,
		"error":      errorCode,
	}
}

func isPredictionCacheFresh(data map[string]any, now time.Time) bool {
	lastDate, ok := latestPredictionFutureDate(data["future_dates"])
	if !ok {
		return false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return !lastDate.Before(today)
}

func latestPredictionFutureDate(value any) (time.Time, bool) {
	var rawDates []any
	switch typed := value.(type) {
	case []any:
		rawDates = typed
	case []string:
		rawDates = make([]any, 0, len(typed))
		for _, item := range typed {
			rawDates = append(rawDates, item)
		}
	default:
		return time.Time{}, false
	}
	var latest time.Time
	for _, item := range rawDates {
		dateText := strings.TrimSpace(fmt.Sprint(item))
		if dateText == "" || dateText == "<nil>" {
			continue
		}
		parsed, err := time.Parse(predictionCacheDateLayout, dateText)
		if err != nil {
			continue
		}
		if latest.IsZero() || parsed.After(latest) {
			latest = parsed
		}
	}
	return latest, !latest.IsZero()
}

func storageSymbol(value string) string {
	symbol := strings.TrimSpace(value)
	if len(symbol) == 8 {
		prefix := strings.ToLower(symbol[:2])
		if prefix == "sh" || prefix == "sz" || prefix == "bj" {
			return symbol[2:]
		}
	}
	return symbol
}

func stockTypeInt(value any) int {
	switch typed := value.(type) {
	case nil:
		return 1
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(typed))
		switch trimmed {
		case "stock", "a_stock", "a-stock":
			return 1
		case "etf", "fund":
			return 2
		case "index":
			return 3
		default:
			if parsed, err := strconv.Atoi(trimmed); err == nil {
				return parsed
			}
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case int32:
		return int(typed)
	}
	return 1
}

func intValue(value any, fallback int) int {
	switch typed := value.(type) {
	case nil:
		return fallback
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	case float64:
		if typed != 0 {
			return int(typed)
		}
	case float32:
		if typed != 0 {
			return int(typed)
		}
	case int:
		if typed != 0 {
			return typed
		}
	case int64:
		if typed != 0 {
			return int(typed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil && parsed != 0 {
			return parsed
		}
	}
	return fallback
}
