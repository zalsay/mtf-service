package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"fintrack-api/config"
	"fintrack-api/database"
	"fintrack-api/models"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSaveMTFBestPersistsPredictionValues(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("INSERT INTO mtf_best_predictions").
		WithArgs(
			"best-key", "510300", "2.5", "mtf-0.5", `{"mae":1.2}`,
			"mtf-pro", "{}", nil, "{}",
			1,
			"2025-01-01", "2025-12-31", "2026-01-01", "2026-02-01", "2026-02-02", "2026-03-01",
			2048, 7,
			`[1.23,1.45]`, `["2026-03-02","2026-03-03"]`, `[1.11,1.22]`,
			0.5,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewWatchlistService(&database.DB{Conn: db}, &config.Config{})
	err = service.SaveMTFBest(&models.SaveMTFBestRequest{
		UniqueKey:          "best-key",
		Symbol:             "510300",
		MTFVersion:         "2.5",
		BestPredictionItem: "mtf-0.5",
		BestMetrics:        map[string]interface{}{"mae": 1.2},
		PredictionType:     "mtf-pro",
		TrainStartDate:     "2025-01-01",
		TrainEndDate:       "2025-12-31",
		TestStartDate:      "2026-01-01",
		TestEndDate:        "2026-02-01",
		ValStartDate:       "2026-02-02",
		ValEndDate:         "2026-03-01",
		ContextLen:         2048,
		HorizonLen:         7,
		BestPredictionValues: []float64{
			1.23,
			1.45,
		},
		FutureDates: []string{
			"2026-03-02",
			"2026-03-03",
		},
		AdjustRawBestPredictionValues: []float64{
			1.11,
			1.22,
		},
	})
	if err != nil {
		t.Fatalf("SaveMTFBest error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGetMTFBestValueByUniqueKeyReturnsSavedValues(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT unique_key, best_prediction_item").
		WithArgs("best-key").
		WillReturnRows(sqlmock.NewRows([]string{
			"unique_key", "best_prediction_item", "best_prediction_quantile", "best_prediction_values", "future_dates", "adjust_raw_best_prediction_values",
		}).AddRow(
			"best-key", "mtf-0.5", 0.5, []byte(`[1.23,1.45]`), []byte(`["2026-03-02","2026-03-03"]`), []byte(`[1.11,1.22]`),
		))

	service := NewWatchlistService(&database.DB{Conn: db}, &config.Config{})
	value, err := service.GetMTFBestValueByUniqueKey("best-key")
	if err != nil {
		t.Fatalf("GetMTFBestValueByUniqueKey error = %v", err)
	}
	if value.UniqueKey != "best-key" || value.BestPredictionItem != "mtf-0.5" {
		t.Fatalf("value identity = %#v", value)
	}
	if value.BestPredictionQuantile == nil || *value.BestPredictionQuantile != 0.5 {
		t.Fatalf("BestPredictionQuantile = %#v, want 0.5", value.BestPredictionQuantile)
	}
	if len(value.BestPredictionValues) != 2 || value.BestPredictionValues[0] != 1.23 || value.BestPredictionValues[1] != 1.45 {
		t.Fatalf("BestPredictionValues = %#v, want [1.23 1.45]", value.BestPredictionValues)
	}
	if len(value.FutureDates) != 2 || value.FutureDates[1] != "2026-03-03" {
		t.Fatalf("FutureDates = %#v, want two dates", value.FutureDates)
	}
	if len(value.AdjustRawBestPredictionValues) != 2 || value.AdjustRawBestPredictionValues[0] != 1.11 {
		t.Fatalf("AdjustRawBestPredictionValues = %#v, want [1.11 1.22]", value.AdjustRawBestPredictionValues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGetMTFBestValueByUniqueKeyFallsBackToFutureChunks(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT unique_key, best_prediction_item").
		WithArgs("best-key").
		WillReturnRows(sqlmock.NewRows([]string{
			"unique_key", "best_prediction_item", "best_prediction_quantile", "best_prediction_values", "future_dates", "adjust_raw_best_prediction_values",
		}).AddRow(
			"best-key", "mtf-0.5", 0.5, []byte(`null`), []byte(`null`), []byte(`null`),
		))
	mock.ExpectQuery("SELECT best_prediction_item FROM mtf_best_predictions").
		WithArgs("best-key").
		WillReturnRows(sqlmock.NewRows([]string{"best_prediction_item"}).AddRow("mtf-0.5"))
	mock.ExpectQuery("SELECT dates, predictions").
		WithArgs("best-key").
		WillReturnRows(sqlmock.NewRows([]string{"dates", "predictions"}).AddRow(
			[]byte(`["2026-03-02","2026-03-03"]`),
			[]byte(`{"mtf-0.5":[1.23,1.45]}`),
		))
	mock.ExpectQuery("SELECT actual_values").
		WithArgs("best-key").
		WillReturnRows(sqlmock.NewRows([]string{"actual_values"}).AddRow([]byte(`[1.00]`)))

	service := NewWatchlistService(&database.DB{Conn: db}, &config.Config{})
	value, err := service.GetMTFBestValueByUniqueKey("best-key")
	if err != nil {
		t.Fatalf("GetMTFBestValueByUniqueKey error = %v", err)
	}
	if value.Source != "future_chunks" {
		t.Fatalf("Source = %q, want future_chunks", value.Source)
	}
	if len(value.BestPredictionValues) != 2 || value.BestPredictionValues[1] != 1.45 {
		t.Fatalf("BestPredictionValues = %#v, want fallback values", value.BestPredictionValues)
	}
	if value.PredictedLatest != 1.45 || value.ActualLatest != 1.00 {
		t.Fatalf("latest values = %v/%v, want 1.45/1.00", value.PredictedLatest, value.ActualLatest)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestNormalizeMTFBestTrainRequestLevel0AllowsOnlyFixedNonCov(t *testing.T) {
	req := &models.MTFBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "non_cov",
		HorizonLen:     7,
		ContextLen:     512,
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
		ContextLen:     512,
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

func TestListPublicMTFBestPageAppliesLimitOffset(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "unique_key", "symbol", "mtf_version", "best_prediction_item", "best_metrics",
		"prediction_type", "covariate_config", "covariate_signature", "covariate_analysis",
		"is_public", "train_start_date", "train_end_date", "test_start_date", "test_end_date",
		"val_start_date", "val_end_date", "context_len", "horizon_len", "created_at", "updated_at",
		"short_name", "stock_type", "watchlist_count", "total_count",
	}).AddRow(
		2, "key-2", "600186", "2.5", "mtf-0.5", `{"score":1}`,
		"mtf-lite", `{}`, "", `{}`,
		1, now, now, now, now,
		now, now, 2048, 7, now, now,
		"莲花控股", 1, 8, 12,
	)
	mock.ExpectQuery(regexp.QuoteMeta("ORDER BY watchlist_count DESC, created_at DESC, id DESC")).
		WithArgs(5, 5).
		WillReturnRows(rows)

	service := NewWatchlistService(&database.DB{Conn: db}, &config.Config{})
	page, err := service.ListPublicMTFBestPage(0, "", false, 5, 5)
	if err != nil {
		t.Fatalf("ListPublicMTFBestPage error = %v", err)
	}
	if page.Total != 12 {
		t.Fatalf("Total = %d, want 12", page.Total)
	}
	if page.Limit != 5 || page.Offset != 5 {
		t.Fatalf("Limit/Offset = %d/%d, want 5/5", page.Limit, page.Offset)
	}
	if len(page.Items) != 1 || page.Items[0].UniqueKey != "key-2" {
		t.Fatalf("Items = %#v, want key-2", page.Items)
	}
	if page.Items[0].WatchlistCount != 8 || page.Items[0].StockType != 1 {
		t.Fatalf("WatchlistCount/StockType = %d/%d, want 8/1", page.Items[0].WatchlistCount, page.Items[0].StockType)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListPublicMTFBestPageByStockTypeFiltersBeforeLimitOffset(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "unique_key", "symbol", "mtf_version", "best_prediction_item", "best_metrics",
		"prediction_type", "covariate_config", "covariate_signature", "covariate_analysis",
		"is_public", "train_start_date", "train_end_date", "test_start_date", "test_end_date",
		"val_start_date", "val_end_date", "context_len", "horizon_len", "created_at", "updated_at",
		"short_name", "stock_type", "watchlist_count", "total_count",
	}).AddRow(
		3, "etf-key", "510300", "2.5", "mtf-0.5", `{"score":1}`,
		"mtf-lite", `{}`, "", `{}`,
		1, now, now, now, now,
		now, now, 2048, 7, now, now,
		"沪深300ETF", 2, 5, 9,
	)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE rn = 1 AND stock_type = $1")).
		WithArgs(2, 5, 10).
		WillReturnRows(rows)

	service := NewWatchlistService(&database.DB{Conn: db}, &config.Config{})
	page, err := service.ListPublicMTFBestPageByStockType(0, "", false, 5, 10, 2)
	if err != nil {
		t.Fatalf("ListPublicMTFBestPageByStockType error = %v", err)
	}
	if page.Total != 9 || page.Limit != 5 || page.Offset != 10 {
		t.Fatalf("page = %#v, want total=9 limit=5 offset=10", page)
	}
	if len(page.Items) != 1 || page.Items[0].StockType != 2 {
		t.Fatalf("Items = %#v, want stock_type=2", page.Items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListPublicMTFBestPageByStockTypeUsesValidationChunkType(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "unique_key", "symbol", "mtf_version", "best_prediction_item", "best_metrics",
		"prediction_type", "covariate_config", "covariate_signature", "covariate_analysis",
		"is_public", "train_start_date", "train_end_date", "test_start_date", "test_end_date",
		"val_start_date", "val_end_date", "context_len", "horizon_len", "created_at", "updated_at",
		"short_name", "stock_type", "watchlist_count", "total_count",
	}).AddRow(
		4, "etf-no-watch-key", "159919", "2.5", "mtf-0.5", `{"score":1}`,
		"mtf-lite", `{}`, "", `{}`,
		1, now, now, now, now,
		now, now, 2048, 7, now, now,
		"沪深300ETF", 2, 0, 1,
	)
	mock.ExpectQuery(regexp.QuoteMeta("MIN(NULLIF(stock_type, 0))::int AS stock_type")).
		WithArgs(2).
		WillReturnRows(rows)

	service := NewWatchlistService(&database.DB{Conn: db}, &config.Config{})
	page, err := service.ListPublicMTFBestPageByStockType(0, "", false, 0, 0, 2)
	if err != nil {
		t.Fatalf("ListPublicMTFBestPageByStockType error = %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Symbol != "159919" || page.Items[0].StockType != 2 {
		t.Fatalf("Items = %#v, want validation chunk ETF item", page.Items)
	}
	if page.Items[0].WatchlistCount != 0 {
		t.Fatalf("WatchlistCount = %d, want 0", page.Items[0].WatchlistCount)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListPublicMTFBestPageByStockTypeDoesNotUseWatchlistType(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "unique_key", "symbol", "mtf_version", "best_prediction_item", "best_metrics",
		"prediction_type", "covariate_config", "covariate_signature", "covariate_analysis",
		"is_public", "train_start_date", "train_end_date", "test_start_date", "test_end_date",
		"val_start_date", "val_end_date", "context_len", "horizon_len", "created_at", "updated_at",
		"short_name", "stock_type", "watchlist_count", "total_count",
	}).AddRow(
		5, "etf-validation-key", "510500", "2.5", "mtf-0.5", `{"score":1}`,
		"mtf-lite", `{}`, "", `{}`,
		1, now, now, now, now,
		now, now, 2048, 7, now, now,
		"中证500ETF", 2, 3, 1,
	)
	mock.ExpectQuery(regexp.QuoteMeta("COALESCE(vst.stock_type, 1)::int AS stock_type")).
		WithArgs(2).
		WillReturnRows(rows)

	service := NewWatchlistService(&database.DB{Conn: db}, &config.Config{})
	page, err := service.ListPublicMTFBestPageByStockType(0, "", false, 0, 0, 2)
	if err != nil {
		t.Fatalf("ListPublicMTFBestPageByStockType error = %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Symbol != "510500" || page.Items[0].StockType != 2 {
		t.Fatalf("Items = %#v, want validation chunk ETF item", page.Items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestNormalizeMTFBestTrainRequestLevel0RejectsCov(t *testing.T) {
	req := &models.MTFBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "cov",
		HorizonLen:     7,
		ContextLen:     512,
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
		ContextLen:     512,
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
	if received["best_max_age_days"] != float64(180) {
		t.Fatalf("best_max_age_days = %#v, want 180", received["best_max_age_days"])
	}
	if received["predict_from_best_val_end"] != true {
		t.Fatalf("predict_from_best_val_end = %#v, want true", received["predict_from_best_val_end"])
	}
	if received["chunk_until_latest"] != true {
		t.Fatalf("chunk_until_latest = %#v, want true", received["chunk_until_latest"])
	}
}

func TestTriggerMTFPredictOnceAllowsBestContinuationOverrides(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	bestMaxAgeDays := 90
	predictFromBestEnd := false
	chunkUntilLatest := false
	req := &models.MTFPredictRequest{
		StockCode:          "600246",
		BestMaxAgeDays:     &bestMaxAgeDays,
		PredictFromBestEnd: &predictFromBestEnd,
		ChunkUntilLatest:   &chunkUntilLatest,
	}

	status, _, err := service.TriggerMTFPredictOnce(req)
	if err != nil {
		t.Fatalf("expected predict once proxy to pass, got error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if received["best_max_age_days"] != float64(90) {
		t.Fatalf("best_max_age_days = %#v, want 90", received["best_max_age_days"])
	}
	if received["predict_from_best_val_end"] != false {
		t.Fatalf("predict_from_best_val_end = %#v, want false", received["predict_from_best_val_end"])
	}
	if received["chunk_until_latest"] != false {
		t.Fatalf("chunk_until_latest = %#v, want false", received["chunk_until_latest"])
	}
}

func TestTriggerMTFPredictOncePassesContinuationDates(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	startDate := "2026-04-30"
	endDate := "2026-06-01"
	req := &models.MTFPredictRequest{
		StockCode: "600246",
		StartDate: &startDate,
		EndDate:   &endDate,
	}

	status, _, err := service.TriggerMTFPredictOnce(req)
	if err != nil {
		t.Fatalf("expected predict once proxy to pass, got error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if received["start_date"] != startDate {
		t.Fatalf("start_date = %#v, want %s", received["start_date"], startDate)
	}
	if received["end_date"] != endDate {
		t.Fatalf("end_date = %#v, want %s", received["end_date"], endDate)
	}
}

func TestGetMTFPredictOnceCachedQueriesPostgresHandler(t *testing.T) {
	futureDate := time.Now().UTC().Format("2006-01-02")
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
		if query.Has("covariate_signature") {
			t.Fatalf("query must not include covariate_signature: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"message": "Success",
			"data": {
				"stock_code": "510050",
				"stock_type": 2,
				"prediction_type": "mtf-pro",
				"mtf_version": "2.5",
				"context_len": 2048,
				"horizon_len": 7,
				"latest_data_date": "` + futureDate + `",
				"latest_close": 1.11,
				"change_base_value": 1.11,
				"change_base_date": "2026-06-05",
				"future_dates": ["` + futureDate + `"],
				"best_prediction_item": "mtf-0.5",
				"best_prediction_values": [1.23],
				"adjust_raw_best_prediction_values": [1.11],
				"adjust_raw_latest_close": 1.01,
				"predictions": {"mtf-0.5": [1.23]},
				"predicted_change_percent": {"mtf-0.5": [10.8108]},
				"cache_hit": true,
				"covariate_analysis": {"debug": true},
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
	if data["best_prediction_item"] != "mtf-0.5" {
		t.Fatalf("best_prediction_item = %#v, want mtf-0.5", data["best_prediction_item"])
	}
	if rawValues, ok := data["adjust_raw_best_prediction_values"].([]interface{}); !ok || len(rawValues) != 1 || rawValues[0] != float64(1.11) {
		t.Fatalf("adjust_raw_best_prediction_values = %#v, want [1.11]", data["adjust_raw_best_prediction_values"])
	}
	if data["adjust_raw_latest_close"] != float64(1.01) {
		t.Fatalf("adjust_raw_latest_close = %#v, want 1.01", data["adjust_raw_latest_close"])
	}
	if data["change_base_value"] == nil || data["change_base_date"] == nil || data["predicted_change_percent"] == nil {
		t.Fatalf("expected change fields in cached response: %#v", data)
	}
	if _, exists := data["cache_hit"]; exists {
		t.Fatalf("cache_hit must be omitted from public response: %#v", data["cache_hit"])
	}
	if _, exists := data["predictions"]; exists {
		t.Fatalf("predictions must be omitted from cached response")
	}
	if _, exists := data["covariate_analysis"]; exists {
		t.Fatalf("covariate_analysis must be omitted from cached response")
	}
	if _, exists := data["mtf_version"]; exists {
		t.Fatalf("mtf_version must be omitted from cached response")
	}
	if _, exists := data["history_rows"]; exists {
		t.Fatalf("history_rows must be omitted from cached response")
	}
	if _, exists := data["covariate_signature"]; exists {
		t.Fatalf("covariate_signature must be omitted from cached response")
	}
}

func TestGetMTFJobStatusReturnsSlimResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/job-test" {
			t.Fatalf("path = %s, want /jobs/job-test", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"backend":"xpu",
			"covariate_signature":"sig123",
			"created_at":"2026-06-05T10:27:27Z",
			"current_stage":"",
			"error":"",
			"finished_at":"2026-06-05T10:27:28Z",
			"force_enqueue":true,
			"job_id":"job-test",
			"job_kind":"inference",
			"prediction_type":"mtf-pro",
			"request_key":"/internal/predict_once_sync:{}",
			"result":{
				"data":{
					"stock_code":"300442",
					"stock_type":1,
					"prediction_type":"mtf-pro",
					"mtf_version":"2.5",
					"context_len":2048,
					"horizon_len":7,
					"latest_data_date":"2026-06-05",
					"latest_close":78.17,
					"future_dates":["2026-06-05"],
					"best_prediction_item":"mtf-0.6",
					"best_prediction_values":[81.45],
					"cache_hit":true,
					"covariate_analysis":{"debug":true},
					"covariate_config":{"enabled":true},
					"covariate_signature":"sig123",
					"history_rows":2676,
					"predictions":{"mtf-0.6":[81.45]},
					"timesfm_version":"2.5",
					"unique_key":"internal-key"
				},
				"gpu_id":"0",
				"message":"单次预测完成",
				"stock_code":"300442",
				"success":true
			},
			"started_at":"2026-06-05T10:27:27Z",
			"status":"succeeded",
			"stock_code":"300442",
			"target_path":"/internal/predict_once_sync",
			"ticker":"300442",
			"upstream_status":200
		}`))
	}))
	defer server.Close()

	service := NewWatchlistService(nil, &config.Config{
		InferenceGateway: config.InferenceGatewayConfig{
			BaseURL: server.URL,
			Timeout: 2,
		},
	})

	status, body, err := service.GetMTFJobStatus("job-test")
	if err != nil {
		t.Fatalf("GetMTFJobStatus returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if _, exists := body["request_key"]; exists {
		t.Fatalf("request_key must be omitted from job response")
	}
	if _, exists := body["target_path"]; exists {
		t.Fatalf("target_path must be omitted from job response")
	}
	if _, exists := body["backend"]; exists {
		t.Fatalf("backend must be omitted from job response")
	}
	result, ok := body["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("result = %#v, want object", body["result"])
	}
	data, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("result.data = %#v, want object", result["data"])
	}
	if data["best_prediction_item"] != "mtf-0.6" {
		t.Fatalf("best_prediction_item = %#v, want mtf-0.6", data["best_prediction_item"])
	}
	for _, key := range []string{
		"cache_hit",
		"covariate_analysis",
		"covariate_config",
		"covariate_signature",
		"history_rows",
		"mtf_version",
		"predictions",
		"timesfm_version",
		"unique_key",
	} {
		if _, exists := data[key]; exists {
			t.Fatalf("%s must be omitted from job result data", key)
		}
	}
}

func TestDirectPredictionCacheFreshness(t *testing.T) {
	staleData := map[string]interface{}{
		"future_dates": []interface{}{"2026-06-01", "2026-06-02", "2026-06-03"},
	}
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	if isDirectPredictionCacheFresh(staleData, now) {
		t.Fatal("cache ending on 2026-06-03 must be stale on 2026-06-05")
	}

	freshData := map[string]interface{}{
		"future_dates": []interface{}{"2026-06-05", "2026-06-08"},
	}
	if !isDirectPredictionCacheFresh(freshData, now) {
		t.Fatal("cache covering 2026-06-05 must be fresh")
	}
}
