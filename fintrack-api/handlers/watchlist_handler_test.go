package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
