package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fintrack-api/config"
	"fintrack-api/database"
	"fintrack-api/models"
	"fintrack-api/services"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func TestAssignStrategyParamsUserIDFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("user_id", 42)

	req := models.SaveStrategyParamsRequest{UniqueKey: "tpl_test"}
	assignStrategyParamsUserID(c, &req)

	if req.UserID == nil || *req.UserID != 42 {
		t.Fatalf("UserID = %#v, want 42", req.UserID)
	}
}

func TestAssignStrategyParamsUserIDKeepsRequestValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("user_id", 42)

	userID := 7
	req := models.SaveStrategyParamsRequest{UniqueKey: "tpl_test", UserID: &userID}
	assignStrategyParamsUserID(c, &req)

	if req.UserID == nil || *req.UserID != 7 {
		t.Fatalf("UserID = %#v, want existing value 7", req.UserID)
	}
}

func TestGetMTFBestValueByUniqueKeyHandlerReturnsValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
			"best-key", "mtf-0.5", 0.5, []byte(`[1.23]`), []byte(`["2026-03-02"]`), []byte(`[1.11]`),
		))

	handler := NewWatchlistHandler(services.NewWatchlistService(&database.DB{Conn: db}, &config.Config{}))
	router := gin.New()
	router.GET("/api/v1/save-predictions/mtf-best/value", handler.GetMTFBestValueByUniqueKey)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/save-predictions/mtf-best/value?unique_key=best-key", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Value models.MTFBestValue `json:"value"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Value.UniqueKey != "best-key" || body.Value.Source != "saved" {
		t.Fatalf("value = %#v, want saved best-key", body.Value)
	}
	if body.Value.BestPredictionQuantile == nil || *body.Value.BestPredictionQuantile != 0.5 {
		t.Fatalf("best_prediction_quantile = %#v, want 0.5", body.Value.BestPredictionQuantile)
	}
	if len(body.Value.BestPredictionValues) != 1 || body.Value.BestPredictionValues[0] != 1.23 {
		t.Fatalf("best_prediction_values = %#v, want [1.23]", body.Value.BestPredictionValues)
	}
	if !strings.Contains(rec.Body.String(), `"adjust_raw_best_prediction_values":[1.11]`) {
		t.Fatalf("expected adjust raw values in response, body=%s", rec.Body.String())
	}
	if body.Value.FutureDates[0] != "2026-03-02" {
		t.Fatalf("future_dates = %#v, want 2026-03-02", body.Value.FutureDates)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGetMTFBestValueByUniqueKeyHandlerRequiresUniqueKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewWatchlistHandler(services.NewWatchlistService(nil, &config.Config{}))
	router := gin.New()
	router.GET("/api/v1/save-predictions/mtf-best/value", handler.GetMTFBestValueByUniqueKey)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/save-predictions/mtf-best/value", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unique_key is required") {
		t.Fatalf("body = %s, want unique_key error", rec.Body.String())
	}
}

func TestListPublicMTFBestWithValidationGroupsVariantsBySymbol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	now := time.Now()
	bestRows := sqlmock.NewRows([]string{
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
	mock.ExpectQuery("FROM ranked").
		WithArgs(2, 1).
		WillReturnRows(bestRows)

	chunkRows := sqlmock.NewRows([]string{
		"unique_key", "chunk_index", "start_date", "end_date", "symbol",
		"predictions", "actual_values", "predicted_change_percent", "actual_change_percent",
		"change_base_value", "change_base_date", "dates", "prediction_type",
		"covariate_config", "covariate_signature", "covariate_analysis",
		"stock_type", "adjust_raw_chunks",
	}).AddRow(
		"510050-h7-c2048", 0, "2026-01-01", "2026-01-07", "510050",
		[]byte(`{"mtf-0.5":[2.1,2.2]}`), []byte(`[2.0,2.1]`), []byte(`{"mtf-0.5":[1,2]}`), []byte(`[0,1]`),
		2.0, "2025-12-31", []byte(`["2026-01-01","2026-01-02"]`), "mtf-pro",
		[]byte(`{}`), "", []byte(`{}`), 2, []byte(`null`),
	).AddRow(
		"510050-h14-c2048", 0, "2026-01-01", "2026-01-14", "510050",
		[]byte(`{"mtf-0.5":[2.1,2.3]}`), []byte(`[2.0,2.1]`), []byte(`{"mtf-0.5":[1,3]}`), []byte(`[0,1]`),
		2.0, "2025-12-31", []byte(`["2026-01-01","2026-01-02"]`), "mtf-pro",
		[]byte(`{}`), "", []byte(`{}`), 2, []byte(`null`),
	)
	mock.ExpectQuery("FROM mtf_best_validation_chunks").
		WillReturnRows(chunkRows)

	handler := NewWatchlistHandler(services.NewWatchlistService(&database.DB{Conn: db}, &config.Config{}))
	router := gin.New()
	router.GET("/api/v1/get-predictions/mtf-best/public", handler.ListPublicMTFBestWithValidation)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/get-predictions/mtf-best/public?stock_type=2&limit=1", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Count int `json:"count"`
		Total int `json:"total"`
		Items []struct {
			Symbol   string        `json:"symbol"`
			Variants []interface{} `json:"variants"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Count != 1 || body.Total != 1 || len(body.Items) != 1 {
		t.Fatalf("body = %#v, want one grouped item", body)
	}
	if body.Items[0].Symbol != "510050" || len(body.Items[0].Variants) != 2 {
		t.Fatalf("grouped item = %#v, want 510050 with 2 variants", body.Items[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSlimAccessibleMTFBestItemsKeepsOnlyFrontendFields(t *testing.T) {
	items := []gin.H{
		{
			"best": gin.H{
				"unique_key":           "best-key",
				"symbol":               "sh562950",
				"mtf_version":          "2.5",
				"best_prediction_item": "mtf-0.5",
				"best_metrics":         `{"composite_score":12.3,"unused":99}`,
				"prediction_type":      "mtf-lite",
				"short_name":           "",
				"watchlist_count":      7,
				"context_len":          2048,
				"horizon_len":          7,
				"stock_type":           2,
				"created_at":           "2026-01-01T00:00:00Z",
				"updated_at":           "2026-01-02T00:00:00Z",
				"covariate_analysis":   gin.H{"unused": true},
			},
			"chunks": []gin.H{
				{
					"chunk_index":              3,
					"start_date":               "2026-01-01",
					"end_date":                 "2026-01-07",
					"symbol":                   "sh562950",
					"stock_type":               2,
					"predictions":              gin.H{"mtf-0.5": []float64{1.1, 1.2}, "unused": []float64{9}},
					"actual_values":            []float64{1.0, 1.1},
					"predicted_change_percent": gin.H{"mtf-0.5": []float64{1, 2}, "unused": []float64{9}},
					"actual_change_percent":    []float64{0, 1},
					"change_base_value":        1.0,
					"change_base_date":         "2025-12-31",
					"dates":                    []string{"2026-01-01", "2026-01-02"},
					"prediction_type":          "mtf-lite",
					"adjust_raw_chunks":        gin.H{"predictions": gin.H{"unused": []float64{9}}},
				},
				{
					"chunk_index":              4,
					"start_date":               "2026-06-04",
					"end_date":                 "2026-06-12",
					"symbol":                   "sh562950",
					"stock_type":               2,
					"predictions":              gin.H{"mtf-0.5": []float64{2.1, 2.2}},
					"actual_values":            []float64{2.0, 2.1},
					"predicted_change_percent": gin.H{"mtf-0.5": []float64{1, 2}},
					"actual_change_percent":    []float64{0, 1},
					"dates":                    []string{"2026-06-04", "2026-06-05"},
				},
			},
			"max_deviation_percent": 5.5,
		},
	}

	result := slimAccessibleMTFBestItems(items, nil)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}

	best := result[0]["best"].(gin.H)
	for _, key := range []string{"stock_type", "created_at", "covariate_analysis", "short_name"} {
		if _, ok := best[key]; ok {
			t.Fatalf("best contains %q: %#v", key, best)
		}
	}
	metrics := best["best_metrics"].(gin.H)
	if metrics["composite_score"] != float64(12.3) || len(metrics) != 1 {
		t.Fatalf("best_metrics = %#v, want only composite_score", metrics)
	}

	chunks := result[0]["chunks"].([]gin.H)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want latest drawable chunk only", len(chunks))
	}
	chunk := chunks[0]
	if chunk["start_date"] != "2026-06-04" {
		t.Fatalf("chunk start_date = %#v, want latest chunk 2026-06-04", chunk["start_date"])
	}
	for _, key := range []string{"chunk_index", "end_date", "symbol", "prediction_type", "change_base_value", "change_base_date", "stock_type"} {
		if _, ok := chunk[key]; ok {
			t.Fatalf("chunk contains %q: %#v", key, chunk)
		}
	}
	if _, ok := chunk["predictions"].(gin.H)["mtf-0.5"]; !ok {
		t.Fatalf("chunk predictions = %#v, want best prediction series", chunk["predictions"])
	}
	if _, ok := chunk["adjust_raw_chunks"]; ok {
		t.Fatalf("chunk contains empty adjust_raw_chunks: %#v", chunk)
	}
}
