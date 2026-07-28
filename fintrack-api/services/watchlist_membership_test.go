package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
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

func TestSaveStrategyParamsPersistsPersonalStrategyAsPrivate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	userID := 1
	name := "3.5+-1"
	mock.ExpectExec("INSERT INTO mtf_strategy_params").
		WithArgs(
			"tpl_personal", userID, &name,
			3.5, -1.0, 100000.0,
			true, 1.0, 0.0,
			0.2, 0.05,
			0.001, 0.2, 0.5,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewWatchlistService(&database.DB{Conn: db}, &config.Config{})
	err = service.SaveStrategyParams(&models.SaveStrategyParamsRequest{
		UniqueKey:              "tpl_personal",
		UserID:                 &userID,
		Name:                   &name,
		BuyThresholdPct:        3.5,
		SellThresholdPct:       -1.0,
		InitialCash:            100000.0,
		EnableRebalance:        true,
		MaxPositionPct:         1.0,
		MinPositionPct:         0.0,
		SlopePositionPerPct:    0.2,
		RebalanceTolerancePct:  0.05,
		TradeFeeRate:           0.001,
		TakeProfitThresholdPct: 0.2,
		TakeProfitSellFrac:     0.5,
	})
	if err != nil {
		t.Fatalf("SaveStrategyParams error = %v", err)
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
	original := chinaNowFunc
	chinaNowFunc = func() time.Time {
		return time.Date(2026, 6, 11, 10, 0, 0, 0, chinaLocation())
	}
	defer func() {
		chinaNowFunc = original
	}()

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
			[]byte(`["2026-06-15","2026-06-16"]`),
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

func TestListFuturePredictionsByUniqueKeyKeepsFutureDatesInsideStartedChunk(t *testing.T) {
	original := chinaNowFunc
	chinaNowFunc = func() time.Time {
		return time.Date(2026, 6, 11, 10, 0, 0, 0, chinaLocation())
	}
	defer func() {
		chinaNowFunc = original
	}()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT best_prediction_item FROM mtf_best_predictions").
		WithArgs("best-key").
		WillReturnRows(sqlmock.NewRows([]string{"best_prediction_item"}).AddRow("mtf-0.6"))
	mock.ExpectQuery("SELECT dates, predictions").
		WithArgs("best-key").
		WillReturnRows(sqlmock.NewRows([]string{"dates", "predictions"}).AddRow(
			[]byte(`["2026-06-08","2026-06-12","2026-06-15","2026-06-16"]`),
			[]byte(`{"mtf-0.6":[2.9524,2.9583,2.9595,2.9631]}`),
		))
	mock.ExpectQuery("SELECT actual_values").
		WithArgs("best-key").
		WillReturnRows(sqlmock.NewRows([]string{"actual_values"}).AddRow([]byte(`[3.026]`)))

	service := NewWatchlistService(&database.DB{Conn: db}, &config.Config{})
	dates, preds, predictedLatest, actualLatest, changePct, err := service.ListFuturePredictionsByUniqueKey("best-key")
	if err != nil {
		t.Fatalf("ListFuturePredictionsByUniqueKey error = %v", err)
	}
	if len(dates) != 3 || dates[0] != "2026-06-12" || dates[1] != "2026-06-15" || dates[2] != "2026-06-16" {
		t.Fatalf("dates = %#v, want only future dates", dates)
	}
	if len(preds) != 3 || preds[0] != 2.9583 || preds[1] != 2.9595 || preds[2] != 2.9631 {
		t.Fatalf("preds = %#v, want future prediction values", preds)
	}
	if predictedLatest != 2.9631 || actualLatest != 3.026 {
		t.Fatalf("latest values = %v/%v, want 2.9631/3.026", predictedLatest, actualLatest)
	}
	if changePct >= 0 {
		t.Fatalf("changePct = %v, want negative change", changePct)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestNormalizeMTFBestTrainRequestLevel0AllowsOnlyFixedNonCov(t *testing.T) {
	original := chinaNowFunc
	chinaNowFunc = func() time.Time {
		return time.Date(2026, 6, 11, 10, 0, 0, 0, chinaLocation())
	}
	defer func() {
		chinaNowFunc = original
	}()

	req := &models.MTFBestTrainRequest{
		StockCode:      "000001",
		StockType:      1,
		PredictionType: "non_cov",
		HorizonLen:     7,
		ContextLen:     1024,
	}

	normalized, err := NormalizeMTFBestTrainRequest(req, 0, 12, false)
	if err != nil {
		t.Fatalf("expected level 0 request to pass, got error: %v", err)
	}
	if normalized.UserID == nil || *normalized.UserID != 12 {
		t.Fatalf("expected user_id to be injected")
	}
	if normalized.EndDate == nil || *normalized.EndDate != "2026-06-10" {
		t.Fatalf("expected end_date to default to yesterday, got %#v", normalized.EndDate)
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
		ContextLen:     2048,
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
	mock.ExpectQuery("paged_symbols").
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
	mock.ExpectQuery(regexp.QuoteMeta("AND stock_type = $1")).
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

func TestListPublicMTFBestPagePaginatesBySymbolAndReturnsAllVariants(t *testing.T) {
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
		1, "510050-h7-c2048", "510050", "2.5", "mtf-0.5", `{"score":1}`,
		"mtf-pro", `{}`, "", `{}`,
		1, now, now, now, now,
		now, now, 2048, 7, now, now,
		"上证50ETF", 2, 5, 1,
	).AddRow(
		2, "510050-h14-c2048", "510050", "2.5", "mtf-0.5", `{"score":1}`,
		"mtf-pro", `{}`, "", `{}`,
		1, now, now, now, now,
		now, now, 2048, 14, now, now,
		"上证50ETF", 2, 5, 1,
	)
	mock.ExpectQuery("paged_symbols").
		WithArgs(2, 1).
		WillReturnRows(rows)

	service := NewWatchlistService(&database.DB{Conn: db}, &config.Config{})
	page, err := service.ListPublicMTFBestPageByStockType(0, "", false, 1, 0, 2)
	if err != nil {
		t.Fatalf("ListPublicMTFBestPageByStockType error = %v", err)
	}
	if page.Total != 1 || page.Limit != 1 || page.Offset != 0 {
		t.Fatalf("page = %#v, want total=1 limit=1 offset=0", page)
	}
	if len(page.Items) != 2 {
		t.Fatalf("len(Items) = %d, want two variants for one symbol", len(page.Items))
	}
	if page.Items[0].Symbol != "510050" || page.Items[1].Symbol != "510050" {
		t.Fatalf("Items = %#v, want same symbol variants", page.Items)
	}
	if page.Items[0].HorizonLen == page.Items[1].HorizonLen {
		t.Fatalf("HorizonLen values = %d/%d, want different variants", page.Items[0].HorizonLen, page.Items[1].HorizonLen)
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
		ContextLen:     4096,
	}

	if _, err := NormalizeMTFBestTrainRequest(req, 1, 45, false); err == nil {
		t.Fatal("expected context length above VIP limit to be rejected")
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
	original := chinaNowFunc
	chinaNowFunc = func() time.Time {
		return time.Date(2026, 6, 11, 10, 0, 0, 0, chinaLocation())
	}
	defer func() {
		chinaNowFunc = original
	}()

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
	if normalized.EndDate == nil || *normalized.EndDate != "2026-06-10" {
		t.Fatalf("expected end_date to default to yesterday, got %#v", normalized.EndDate)
	}
}

func TestNormalizeMTFPredictOnceRequestPreservesExplicitEndDate(t *testing.T) {
	endDate := "2026-06-01"
	req := &models.MTFPredictRequest{
		StockCode: "000001",
		StockType: 1,
		EndDate:   &endDate,
	}

	normalized, err := NormalizeMTFPredictOnceRequest(req, 0, 88, false)
	if err != nil {
		t.Fatalf("expected predict once request to pass, got error: %v", err)
	}
	if normalized.EndDate == nil || *normalized.EndDate != endDate {
		t.Fatalf("expected explicit end_date to be preserved, got %#v", normalized.EndDate)
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

func TestNormalizeMTFPredictOnceRequestV2DefaultsToFixedHorizon(t *testing.T) {
	req := &models.MTFPredictRequest{
		StockCode:      "510300",
		StockType:      2,
		PredictionType: "mtf-pro",
	}

	normalized, err := NormalizeMTFPredictOnceRequestV2(req)
	if err != nil {
		t.Fatalf("expected v2 request to pass, got error: %v", err)
	}
	if normalized.HorizonLen == nil || *normalized.HorizonLen != MTFV2HorizonLen {
		t.Fatalf("horizon_len = %#v, want %d", normalized.HorizonLen, MTFV2HorizonLen)
	}
	if normalized.ContextLen == nil || *normalized.ContextLen != 512 {
		t.Fatalf("context_len = %#v, want default 512", normalized.ContextLen)
	}
}

func TestNormalizeMTFPredictOnceRequestV2DoesNotDefaultEndDateForPredictDate(t *testing.T) {
	predictDate := "2026-07-24"
	req := &models.MTFPredictRequest{
		StockCode:      "510300",
		StockType:      2,
		PredictionType: "mtf-pro",
		PredictDate:    &predictDate,
	}

	normalized, err := NormalizeMTFPredictOnceRequestV2(req)
	if err != nil {
		t.Fatalf("expected v2 request to pass, got error: %v", err)
	}
	if normalized.EndDate != nil && strings.TrimSpace(*normalized.EndDate) != "" {
		t.Fatalf("end_date = %#v, want unset when predict_date is provided", normalized.EndDate)
	}
}

func TestNormalizeMTFPredictOnceRequestV2PreservesExplicitEndDateWithPredictDate(t *testing.T) {
	predictDate := "2026-07-24"
	endDate := "2026-07-23"
	req := &models.MTFPredictRequest{
		StockCode:      "510300",
		StockType:      2,
		PredictionType: "mtf-pro",
		PredictDate:    &predictDate,
		EndDate:        &endDate,
	}

	normalized, err := NormalizeMTFPredictOnceRequestV2(req)
	if err != nil {
		t.Fatalf("expected v2 request to pass, got error: %v", err)
	}
	if normalized.EndDate == nil || *normalized.EndDate != endDate {
		t.Fatalf("end_date = %#v, want explicit end_date %q", normalized.EndDate, endDate)
	}
}

func TestNormalizeMTFPredictBestRequestV2UsesCurrentTrainingContract(t *testing.T) {
	req := &models.MTFBestTrainRequest{
		StockCode:      "510300",
		PredictionType: "mtf-pro",
		HorizonLen:     8,
		ContextLen:     2048,
	}

	normalized, err := NormalizeMTFPredictBestRequestV2(req)
	if err != nil {
		t.Fatalf("expected v2 train request to pass, got error: %v", err)
	}
	if normalized.PredictionType != "mtf-pro" {
		t.Fatalf("prediction_type = %q, want mtf-pro", normalized.PredictionType)
	}
	if normalized.StockType != 2 {
		t.Fatalf("stock_type = %#v, want 2", normalized.StockType)
	}
	if normalized.UserID != nil {
		t.Fatalf("user_id = %#v, want unset", normalized.UserID)
	}
	if normalized.HorizonLen == nil || *normalized.HorizonLen != 8 {
		t.Fatalf("horizon_len = %#v, want 8", normalized.HorizonLen)
	}
	if normalized.ContextLen == nil || *normalized.ContextLen != 2048 {
		t.Fatalf("context_len = %#v, want 2048", normalized.ContextLen)
	}
}

func TestNormalizeMTFPredictBestRequestV2RejectsUnsupportedTrainingDimensions(t *testing.T) {
	req := &models.MTFBestTrainRequest{
		StockCode:      "510300",
		PredictionType: "mtf-pro",
		HorizonLen:     7,
		ContextLen:     2048,
	}

	if _, err := NormalizeMTFPredictBestRequestV2(req); err == nil {
		t.Fatal("expected unsupported v2 training horizon to be rejected")
	}
}

func TestNormalizeMTFPredictOnceRequestV2RejectsUnsupportedHorizon(t *testing.T) {
	horizonLen := 7
	req := &models.MTFPredictRequest{
		StockCode:      "510300",
		StockType:      2,
		PredictionType: "mtf-pro",
		HorizonLen:     &horizonLen,
	}

	if _, err := NormalizeMTFPredictOnceRequestV2(req); err == nil {
		t.Fatal("expected v2 horizon_len=7 to be rejected")
	} else if err.Error() != "v2 only supports horizon_len=8,16,32,64" {
		t.Fatalf("error = %q, want supported horizon validation", err.Error())
	}
}

func TestNormalizeMTFPredictOnceRequestV2AcceptsExtendedHorizon(t *testing.T) {
	horizonLen := 16
	req := &models.MTFPredictRequest{
		StockCode:      "510300",
		StockType:      2,
		PredictionType: "mtf-pro",
		HorizonLen:     &horizonLen,
	}

	normalized, err := NormalizeMTFPredictOnceRequestV2(req)
	if err != nil {
		t.Fatalf("expected horizon_len=16 to pass, got error: %v", err)
	}
	if normalized.HorizonLen == nil || *normalized.HorizonLen != 16 {
		t.Fatalf("horizon_len = %#v, want 16", normalized.HorizonLen)
	}
}

func TestTriggerMTFPredictOnceSendsForceRequeueAlias(t *testing.T) {
	original := chinaNowFunc
	chinaNowFunc = func() time.Time {
		return time.Date(2026, 6, 11, 10, 0, 0, 0, chinaLocation())
	}
	defer func() {
		chinaNowFunc = original
	}()

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
	if received["end_date"] != "2026-06-10" {
		t.Fatalf("end_date = %#v, want 2026-06-10", received["end_date"])
	}
	if _, ok := received["best_max_age_days"]; ok {
		t.Fatalf("best_max_age_days should not be forwarded, got %#v", received["best_max_age_days"])
	}
	if received["predict_from_best_val_end"] != true {
		t.Fatalf("predict_from_best_val_end = %#v, want true", received["predict_from_best_val_end"])
	}
	if received["chunk_until_latest"] != true {
		t.Fatalf("chunk_until_latest = %#v, want true", received["chunk_until_latest"])
	}
}

func TestTriggerMTFPredictSendsDateBounds(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict_for_best" {
			t.Fatalf("expected /predict_for_best path, got %s", r.URL.Path)
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
	startDate := "2011-06-10"
	endDate := "2026-06-10"
	req := &models.MTFPredictRequest{
		StockCode: "000001",
		StartDate: &startDate,
		EndDate:   &endDate,
	}

	status, _, err := service.TriggerMTFPredict(req)
	if err != nil {
		t.Fatalf("expected predict proxy to pass, got error: %v", err)
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
	predictFromBestEnd := false
	chunkUntilLatest := false
	req := &models.MTFPredictRequest{
		StockCode:          "600246",
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
	if _, ok := received["best_max_age_days"]; ok {
		t.Fatalf("best_max_age_days should not be forwarded, got %#v", received["best_max_age_days"])
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

func TestTriggerMTFPredictOncePassesPredictDate(t *testing.T) {
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
	predictDate := "2026-06-08"
	req := &models.MTFPredictRequest{
		StockCode:   "159206",
		PredictDate: &predictDate,
	}

	status, _, err := service.TriggerMTFPredictOnce(req)
	if err != nil {
		t.Fatalf("expected predict once proxy to pass, got error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if received["predict_date"] != predictDate {
		t.Fatalf("predict_date = %#v, want %s", received["predict_date"], predictDate)
	}
	if _, ok := received["end_date"]; ok {
		t.Fatalf("end_date should not be injected by fintrack-api, got %#v", received["end_date"])
	}
}

func TestGetMTFPredictOnceCachedQueriesInferenceGateway(t *testing.T) {
	futureDate := time.Now().UTC().Format("2006-01-02")
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict_once_cached" {
			t.Fatalf("path = %s, want /predict_once_cached", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("X-API-Token"); got != "gateway-token" {
			t.Fatalf("X-API-Token = %q, want gateway-token", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if received["stock_code"] != "sh510050" {
			t.Fatalf("stock_code payload = %#v, want sh510050", received["stock_code"])
		}
		if received["stock_type"] != "etf" {
			t.Fatalf("stock_type payload = %#v, want etf", received["stock_type"])
		}
		if received["horizon_len"] != float64(7) || received["context_len"] != float64(2048) {
			t.Fatalf("unexpected horizon/context payload: %#v", received)
		}
		if received["prediction_type"] != "mtf-pro" {
			t.Fatalf("prediction_type payload = %#v, want mtf-pro", received["prediction_type"])
		}
		if received["covariate_signature"] != "sig123" {
			t.Fatalf("covariate_signature payload = %#v, want sig123", received["covariate_signature"])
		}
		if received["predict_date"] != "2026-06-08" {
			t.Fatalf("predict_date payload = %#v, want 2026-06-08", received["predict_date"])
		}
		if received["predict_from_best_val_end"] != true || received["chunk_until_latest"] != true {
			t.Fatalf("expected best continuation flags in payload: %#v", received)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"stock_code": "sh510050",
			"message": "单次预测缓存命中",
			"gpu_id": "0",
			"data": {
				"stock_code": "sh510050",
				"stock_type": 2,
				"prediction_type": "mtf-pro",
				"mtf_version": "2.5",
				"context_len": 2048,
				"horizon_len": 7,
				"latest_data_date": "` + futureDate + `",
				"latest_close": 1.11,
				"change_base_value": 1.11,
				"change_base_date": "2026-06-05",
				"prediction_change_base": {"value": 1.11, "date": "2026-06-05", "source": "latest_best_validation_chunk"},
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
		InferenceGateway: config.InferenceGatewayConfig{
			BaseURL:  server.URL,
			APIToken: "gateway-token",
			Timeout:  2,
		},
	})

	horizonLen := 7
	contextLen := 2048
	predictDate := "2026-06-08"
	status, body, err := service.GetMTFPredictOnceCached(&models.MTFPredictRequest{
		StockCode:          "sh510050",
		StockType:          "etf",
		PredictionType:     "mtf-pro",
		HorizonLen:         &horizonLen,
		ContextLen:         &contextLen,
		PredictDate:        &predictDate,
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
	predictedChange, ok := data["predicted_change_percent"].([]interface{})
	if !ok || len(predictedChange) != 1 || predictedChange[0] != float64(10.8108) {
		t.Fatalf("predicted_change_percent must be best item values array: %#v", data["predicted_change_percent"])
	}
	if _, exists := data["prediction_change_base"]; exists {
		t.Fatalf("prediction_change_base must be omitted from public response")
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

func TestPredictionCacheFreshness(t *testing.T) {
	staleData := map[string]interface{}{
		"future_dates": []interface{}{"2026-06-01", "2026-06-02", "2026-06-03"},
	}
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	if isPredictionCacheFresh(staleData, now) {
		t.Fatal("cache ending on 2026-06-03 must be stale on 2026-06-05")
	}

	freshData := map[string]interface{}{
		"future_dates": []interface{}{"2026-06-05", "2026-06-08"},
	}
	if !isPredictionCacheFresh(freshData, now) {
		t.Fatal("cache covering 2026-06-05 must be fresh")
	}
}
