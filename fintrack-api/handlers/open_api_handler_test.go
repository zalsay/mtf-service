package handlers

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fintrack-api/config"
	"fintrack-api/database"
	"fintrack-api/services"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

type memoryAPIKeyTempTokenStore struct {
	userID int
	token  string
	ttl    time.Duration
}

func (s *memoryAPIKeyTempTokenStore) Save(ctx context.Context, token string, userID int, ttl time.Duration) error {
	s.token = token
	s.userID = userID
	s.ttl = ttl
	return nil
}

func (s *memoryAPIKeyTempTokenStore) Consume(ctx context.Context, token string) (int, bool, error) {
	if token == "" || token != s.token {
		return 0, false, nil
	}
	userID := s.userID
	s.token = ""
	return userID, true, nil
}

func TestOpenAPICreateAPIKeyReturnsRawKeyOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	mock.ExpectQuery("SELECT id, password_hash").
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows([]string{"id", "password_hash"}).AddRow(7, string(passwordHash)))
	mock.ExpectExec("INSERT INTO open_api_keys").
		WithArgs(sqlmock.AnyArg(), "agent-key", 7, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	handler := NewOpenAPIHandler(services.NewOpenAPIService(&database.DB{Conn: db}), nil, nil, nil, nil)
	router := gin.New()
	router.POST("/api/open/v1/auth/api-key", handler.CreateAPIKey)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/open/v1/auth/api-key", strings.NewReader(`{"username":"alice","password":"secret123","name":"agent-key"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v", body["data"])
	}
	apiKey, _ := data["api_key"].(string)
	if !strings.HasPrefix(apiKey, "ftk_") {
		t.Fatalf("api_key = %q, want ftk_ prefix", apiKey)
	}
	if strings.Contains(rec.Body.String(), services.HashOpenAPIKey(apiKey)) {
		t.Fatal("response must not expose stored key hash")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOpenAPICreateTempTokenStoresOneTimeToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memoryAPIKeyTempTokenStore{}
	handler := NewOpenAPIHandler(services.NewOpenAPIService(nil), nil, nil, nil, nil)
	handler.SetAPIKeyTempTokenStore(store)
	router := gin.New()
	router.POST("/api/v1/auth/api-key-temp-token", func(c *gin.Context) {
		c.Set("user_id", 7)
		handler.CreateAPIKeyTempToken(c)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/api-key-temp-token", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Token) != 32 {
		t.Fatalf("token length = %d, want 32", len(body.Token))
	}
	if body.ExpiresIn != 300 {
		t.Fatalf("expires_in = %d, want 300", body.ExpiresIn)
	}
	if store.token != body.Token || store.userID != 7 {
		t.Fatalf("stored token/user = %q/%d, want %q/7", store.token, store.userID, body.Token)
	}
	if store.ttl != 5*time.Minute {
		t.Fatalf("ttl = %s, want 5m", store.ttl)
	}
}

func TestOpenAPIRedeemTempTokenCreatesAndReturnsAPIKeyWhenActiveKeyExists(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := &memoryAPIKeyTempTokenStore{token: "12345678901234567890123456789012", userID: 7}
	mock.ExpectExec("INSERT INTO open_api_keys").
		WithArgs(sqlmock.AnyArg(), "codex-smoke-test", 7, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	handler := NewOpenAPIHandler(services.NewOpenAPIService(&database.DB{Conn: db}), nil, nil, nil, nil)
	handler.SetAPIKeyTempTokenStore(store)
	router := gin.New()
	router.POST("/api/open/v1/auth/api-key/from-token", handler.CreateAPIKeyFromTempToken)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/open/v1/auth/api-key/from-token", strings.NewReader(`{"token":"12345678901234567890123456789012","name":"codex-smoke-test"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v", body["data"])
	}
	apiKey, _ := data["api_key"].(string)
	if !strings.HasPrefix(apiKey, "ftk_") {
		t.Fatalf("api_key = %q, want ftk_ prefix", apiKey)
	}
	if strings.Contains(rec.Body.String(), services.HashOpenAPIKey(apiKey)) {
		t.Fatal("response must not expose stored key hash")
	}
	if data["has_existing_key"] != false {
		t.Fatalf("has_existing_key = %#v, want false", data["has_existing_key"])
	}
	if data["name"] != "codex-smoke-test" {
		t.Fatalf("name = %#v, want codex-smoke-test", data["name"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOpenAPICreateAPIKeyRejectsInvalidPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	mock.ExpectQuery("SELECT id, password_hash").
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows([]string{"id", "password_hash"}).AddRow(7, string(passwordHash)))

	handler := NewOpenAPIHandler(services.NewOpenAPIService(&database.DB{Conn: db}), nil, nil, nil, nil)
	router := gin.New()
	router.POST("/api/open/v1/auth/api-key", handler.CreateAPIKey)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/open/v1/auth/api-key", strings.NewReader(`{"username":"alice","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status string `json:"status"`
		Error  struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "error" || body.Error.Code != "invalid_credentials" {
		t.Fatalf("body = %#v", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOpenAPIAuthMiddlewareDeniesMissingScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	apiKey := "ftk_test_key"
	now := time.Now()
	mock.ExpectQuery("SELECT k.id, k.owner_user_id, k.scopes, k.status, k.expires_at").
		WithArgs(services.HashOpenAPIKey(apiKey)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "scopes", "status", "expires_at",
			"user_id", "email", "username", "is_premium", "is_admin", "membership_level", "membership_expires_at", "created_at", "updated_at",
		}).AddRow(3, 7, "{etf:read}", "active", nil, 7, "alice@example.com", "alice", false, false, 1, nil, now, now))
	mock.ExpectExec("UPDATE open_api_keys SET last_used_at").
		WithArgs(3).
		WillReturnResult(sqlmock.NewResult(0, 1))

	handler := NewOpenAPIHandler(services.NewOpenAPIService(&database.DB{Conn: db}), nil, nil, nil, nil)
	router := gin.New()
	router.GET("/protected", handler.AuthMiddleware("mtf:predict"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "scope_denied" {
		t.Fatalf("error code = %q, want scope_denied", body.Error.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOpenAPIPredictOncePrefersCachedPrediction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	apiKey := "ftk_predict_key"
	now := time.Now()
	futureDate := now.UTC().Format("2006-01-02")
	mock.ExpectQuery("SELECT k.id, k.owner_user_id, k.scopes, k.status, k.expires_at").
		WithArgs(services.HashOpenAPIKey(apiKey)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "scopes", "status", "expires_at",
			"user_id", "email", "username", "is_premium", "is_admin", "membership_level", "membership_expires_at", "created_at", "updated_at",
		}).AddRow(3, 7, "{mtf:predict}", "active", nil, 7, "alice@example.com", "alice", false, false, 3, nil, now, now))
	mock.ExpectExec("UPDATE open_api_keys SET last_used_at").
		WithArgs(3).
		WillReturnResult(sqlmock.NewResult(0, 1))

	predictOnceCalled := false
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict_once_cached" {
			if r.URL.Path == "/predict_once" {
				predictOnceCalled = true
			}
			t.Fatalf("gateway path = %s, want /predict_once_cached", r.URL.Path)
		}
		var received map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		if received["stock_type"] != float64(2) {
			t.Fatalf("stock_type = %#v, want 2", received["stock_type"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success":true,
			"message":"单次预测缓存命中",
			"data":{
				"stock_code":"510300",
				"stock_type":2,
				"prediction_type":"mtf-lite",
				"context_len":512,
				"horizon_len":7,
				"latest_data_date":"` + futureDate + `",
				"future_dates":["` + futureDate + `"],
				"best_prediction_item":"mtf-0.5",
				"best_prediction_values":[1.23],
				"change_base_value":1.11,
				"change_base_date":"2026-06-05",
				"prediction_change_base":{"value":1.11,"date":"2026-06-05","source":"latest_best_validation_chunk"},
				"predicted_change_percent":{"mtf-0.5":[10.8108]},
				"adjust_raw_best_prediction_values":[1.11],
				"adjust_raw_latest_close":1.01,
				"predictions":{"mtf-0.5":[1.23]},
				"cache_hit":true
			}
		}`))
	}))
	defer gateway.Close()

	cfg := &config.Config{
		InferenceGateway: config.InferenceGatewayConfig{BaseURL: gateway.URL, Timeout: 1},
	}
	handler := NewOpenAPIHandler(
		services.NewOpenAPIService(&database.DB{Conn: db}),
		services.NewWatchlistService(&database.DB{Conn: db}, cfg),
		nil,
		nil,
		nil,
	)
	router := gin.New()
	router.POST("/api/open/v1/mtf/predict-once", handler.AuthMiddleware("mtf:predict"), handler.MTFPredictOnce)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/open/v1/mtf/predict-once", strings.NewReader(`{"stock_code":"510300","stock_type":2,"horizon_len":7,"context_len":512,"prediction_type":"mtf-lite","prefer_cache":true}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if predictOnceCalled {
		t.Fatal("expected cached response without calling gateway predict_once")
	}
	if !strings.Contains(rec.Body.String(), `"best_prediction_item":"mtf-0.5"`) {
		t.Fatalf("expected slim cached response, body=%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"cache_hit"`) {
		t.Fatalf("cache_hit must be omitted, body=%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"predictions"`) {
		t.Fatalf("predictions must be omitted, body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"adjust_raw_best_prediction_values":[1.11]`) {
		t.Fatalf("expected adjust_raw_best_prediction_values passthrough, body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"predicted_change_percent"`) {
		t.Fatalf("expected predicted_change_percent passthrough, body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"predicted_change_percent":[10.8108]`) {
		t.Fatalf("expected slim predicted_change_percent array, body=%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"mtf-0.5":[10.8108]`) {
		t.Fatalf("predicted_change_percent must not be keyed by best item, body=%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"prediction_change_base"`) {
		t.Fatalf("prediction_change_base must be omitted, body=%s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOpenAPIMTFBestFiltersByStockType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	apiKey := "ftk_mtf_best_key"
	now := time.Now()
	mock.ExpectQuery("SELECT k.id, k.owner_user_id, k.scopes, k.status, k.expires_at").
		WithArgs(services.HashOpenAPIKey(apiKey)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "scopes", "status", "expires_at",
			"user_id", "email", "username", "is_premium", "is_admin", "membership_level", "membership_expires_at", "created_at", "updated_at",
		}).AddRow(3, 7, "{mtf:read}", "active", nil, 7, "alice@example.com", "alice", false, false, 3, nil, now, now))
	mock.ExpectExec("UPDATE open_api_keys SET last_used_at").
		WithArgs(3).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("WITH scoped_best").
		WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "unique_key", "symbol", "mtf_version", "best_prediction_item", "best_metrics",
			"prediction_type", "covariate_config", "covariate_signature", "covariate_analysis",
			"is_public", "train_start_date", "train_end_date", "test_start_date", "test_end_date",
			"val_start_date", "val_end_date", "context_len", "horizon_len",
			"created_at", "updated_at", "short_name", "stock_type", "watchlist_count", "total_count",
		}).
			AddRow(1, "stock-key", "000001", "timesfm", "mtf-0.5", `{"mae":1}`,
				"mtf-lite", `{}`, "", `{}`, 1,
				now, now, now, now, now, now, 512, 7,
				now, now, "平安银行", 1, 3, 2).
			AddRow(2, "etf-key", "510300", "timesfm", "mtf-0.5", `{"mae":1}`,
				"mtf-lite", `{}`, "", `{}`, 1,
				now, now, now, now, now, now, 512, 7,
				now, now, "沪深300ETF", 2, 5, 2))

	handler := NewOpenAPIHandler(
		services.NewOpenAPIService(&database.DB{Conn: db}),
		services.NewWatchlistService(&database.DB{Conn: db}, &config.Config{}),
		nil,
		nil,
		nil,
	)
	router := gin.New()
	router.GET("/api/open/v1/mtf/best", handler.AuthMiddleware("mtf:read"), handler.MTFBest)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/open/v1/mtf/best?stock_type=2&include_validation=false", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Count int `json:"count"`
			Items []struct {
				Symbol    string `json:"symbol"`
				StockType int    `json:"stock_type"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Data.Count != 1 || len(body.Data.Items) != 1 {
		t.Fatalf("items/count = %d/%d, want 1/1; body=%s", len(body.Data.Items), body.Data.Count, rec.Body.String())
	}
	if body.Data.Items[0].Symbol != "510300" || body.Data.Items[0].StockType != 2 {
		t.Fatalf("item = %#v, want ETF stock_type=2", body.Data.Items[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOpenAPIMTFBestRejectsSymbolOutsideWatchlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	apiKey := "ftk_mtf_best_watchlist_key"
	now := time.Now()
	expectOpenAPIAuth(mock, apiKey, "{mtf:read}", 7, 3, now)
	expectWatchlistSymbolCheck(mock, 7, "510300", false)

	handler := NewOpenAPIHandler(
		services.NewOpenAPIService(&database.DB{Conn: db}),
		services.NewWatchlistService(&database.DB{Conn: db}, &config.Config{}),
		nil,
		nil,
		nil,
	)
	router := gin.New()
	router.GET("/api/open/v1/mtf/best", handler.AuthMiddleware("mtf:read"), handler.MTFBest)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/open/v1/mtf/best?symbol=510300&include_validation=false", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	router.ServeHTTP(rec, req)

	assertOpenAPIErrorCode(t, rec, http.StatusForbidden, "watchlist_required")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOpenAPIMTFBestByConfigRejectsSymbolOutsideWatchlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	apiKey := "ftk_mtf_config_watchlist_key"
	now := time.Now()
	expectOpenAPIAuth(mock, apiKey, "{mtf:read}", 7, 3, now)
	expectWatchlistSymbolCheck(mock, 7, "510300", false)

	handler := NewOpenAPIHandler(
		services.NewOpenAPIService(&database.DB{Conn: db}),
		services.NewWatchlistService(&database.DB{Conn: db}, &config.Config{}),
		nil,
		nil,
		nil,
	)
	router := gin.New()
	router.GET("/api/open/v1/mtf/best/by-config", handler.AuthMiddleware("mtf:read"), handler.MTFBestByConfig)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/open/v1/mtf/best/by-config?symbol=510300&horizon_len=7&context_len=2048", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	router.ServeHTTP(rec, req)

	assertOpenAPIErrorCode(t, rec, http.StatusForbidden, "watchlist_required")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOpenAPIMTFBestByConfigAggregatesWhenHorizonAndContextOmitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	apiKey := "ftk_mtf_config_all_key"
	now := time.Now()
	expectOpenAPIAuth(mock, apiKey, "{mtf:read}", 7, 3, now)
	expectWatchlistSymbolCheck(mock, 7, "510050", true)
	mock.ExpectQuery("SELECT horizon_len, context_len, mtf_version, prediction_type, unique_key").
		WithArgs("510050", "", 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"horizon_len", "context_len", "mtf_version", "prediction_type", "unique_key",
		}).
			AddRow(7, 2048, "v_2.5", "mtf-lite", "510050_best_hlen_7_clen_2048_v_2.5").
			AddRow(7, 2048, "v_2.5", "mtf-pro", "510050_best_hlen_7_clen_2048_v_2.5_mtf-pro").
			AddRow(14, 2048, "v_2.5", "mtf-lite", "510050_best_hlen_14_clen_2048_v_2.5"))

	handler := NewOpenAPIHandler(
		services.NewOpenAPIService(&database.DB{Conn: db}),
		services.NewWatchlistService(&database.DB{Conn: db}, &config.Config{}),
		nil,
		nil,
		nil,
	)
	router := gin.New()
	router.GET("/api/open/v1/mtf/best/by-config", handler.AuthMiddleware("mtf:read"), handler.MTFBestByConfig)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/open/v1/mtf/best/by-config?symbol=510050&stock_type=2", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Symbol    string `json:"symbol"`
			StockType int    `json:"stock_type"`
			Count     int    `json:"count"`
			Items     []struct {
				HorizonLen       int    `json:"horizon_len"`
				ContextLen       int    `json:"context_len"`
				MTFVersion       string `json:"mtf_version"`
				MTFLiteUniqueKey string `json:"mtf_lite_unique_key"`
				MTFProUniqueKey  string `json:"mtf_pro_unique_key"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Data.Symbol != "510050" || body.Data.StockType != 2 || body.Data.Count != 2 {
		t.Fatalf("data summary = %#v, body=%s", body.Data, rec.Body.String())
	}
	if len(body.Data.Items) != 2 {
		t.Fatalf("items length = %d, want 2; body=%s", len(body.Data.Items), rec.Body.String())
	}
	first := body.Data.Items[0]
	if first.HorizonLen != 7 || first.ContextLen != 2048 || first.MTFLiteUniqueKey == "" || first.MTFProUniqueKey == "" {
		t.Fatalf("first item = %#v, want aggregated lite/pro 7/2048 keys", first)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOpenAPIMTFBestByConfigAggregatesWithPartialConfigFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name        string
		url         string
		args        []driver.Value
		wantHorizon int
		wantContext int
	}{
		{
			name:        "horizon only",
			url:         "/api/open/v1/mtf/best/by-config?symbol=510050&stock_type=2&horizon_len=7",
			args:        []driver.Value{"510050", "", 2, 7},
			wantHorizon: 7,
			wantContext: 2048,
		},
		{
			name:        "context only",
			url:         "/api/open/v1/mtf/best/by-config?symbol=510050&stock_type=2&context_len=2048",
			args:        []driver.Value{"510050", "", 2, 2048},
			wantHorizon: 7,
			wantContext: 2048,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock: %v", err)
			}
			defer db.Close()

			apiKey := "ftk_mtf_config_partial_key"
			now := time.Now()
			expectOpenAPIAuth(mock, apiKey, "{mtf:read}", 7, 3, now)
			expectWatchlistSymbolCheck(mock, 7, "510050", true)
			mock.ExpectQuery("SELECT horizon_len, context_len, mtf_version, prediction_type, unique_key").
				WithArgs(tc.args...).
				WillReturnRows(sqlmock.NewRows([]string{
					"horizon_len", "context_len", "mtf_version", "prediction_type", "unique_key",
				}).
					AddRow(tc.wantHorizon, tc.wantContext, "v_2.5", "mtf-lite", "510050_best_hlen_7_clen_2048_v_2.5").
					AddRow(tc.wantHorizon, tc.wantContext, "v_2.5", "mtf-pro", "510050_best_hlen_7_clen_2048_v_2.5_mtf-pro"))

			handler := NewOpenAPIHandler(
				services.NewOpenAPIService(&database.DB{Conn: db}),
				services.NewWatchlistService(&database.DB{Conn: db}, &config.Config{}),
				nil,
				nil,
				nil,
			)
			router := gin.New()
			router.GET("/api/open/v1/mtf/best/by-config", handler.AuthMiddleware("mtf:read"), handler.MTFBestByConfig)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			req.Header.Set("Authorization", "Bearer "+apiKey)
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var body struct {
				Data struct {
					Count int `json:"count"`
					Items []struct {
						HorizonLen       int    `json:"horizon_len"`
						ContextLen       int    `json:"context_len"`
						MTFLiteUniqueKey string `json:"mtf_lite_unique_key"`
						MTFProUniqueKey  string `json:"mtf_pro_unique_key"`
					} `json:"items"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Data.Count != 1 || len(body.Data.Items) != 1 {
				t.Fatalf("items/count = %d/%d, want 1/1; body=%s", len(body.Data.Items), body.Data.Count, rec.Body.String())
			}
			item := body.Data.Items[0]
			if item.HorizonLen != tc.wantHorizon || item.ContextLen != tc.wantContext || item.MTFLiteUniqueKey == "" || item.MTFProUniqueKey == "" {
				t.Fatalf("item = %#v, want filtered aggregated keys", item)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("sql expectations: %v", err)
			}
		})
	}
}

func TestOpenAPIMTFFutureRejectsUniqueKeyOutsideWatchlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	apiKey := "ftk_mtf_future_watchlist_key"
	now := time.Now()
	expectOpenAPIAuth(mock, apiKey, "{mtf:read}", 7, 3, now)
	mock.ExpectQuery("SELECT symbol FROM mtf_best_predictions").
		WithArgs("510300-h7-c2048").
		WillReturnRows(sqlmock.NewRows([]string{"symbol"}).AddRow("510300"))
	expectWatchlistSymbolCheck(mock, 7, "510300", false)

	handler := NewOpenAPIHandler(
		services.NewOpenAPIService(&database.DB{Conn: db}),
		services.NewWatchlistService(&database.DB{Conn: db}, &config.Config{}),
		nil,
		nil,
		nil,
	)
	router := gin.New()
	router.GET("/api/open/v1/mtf/future", handler.AuthMiddleware("mtf:read"), handler.MTFFuture)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/open/v1/mtf/future?unique_key=510300-h7-c2048", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	router.ServeHTTP(rec, req)

	assertOpenAPIErrorCode(t, rec, http.StatusForbidden, "watchlist_required")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOpenAPIMTFFutureUsesPredictOnceCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	apiKey := "ftk_mtf_future_once_key"
	now := time.Now()
	expectOpenAPIAuth(mock, apiKey, "{mtf:read}", 7, 3, now)
	mock.ExpectQuery("SELECT symbol FROM mtf_best_predictions").
		WithArgs("510050_best_hlen_7_clen_2048_v_2.5").
		WillReturnRows(sqlmock.NewRows([]string{"symbol"}).AddRow("510050"))
	expectWatchlistSymbolCheck(mock, 7, "510050", true)
	mock.ExpectQuery("FROM mtf_best_predictions p").
		WithArgs("510050_best_hlen_7_clen_2048_v_2.5").
		WillReturnRows(sqlmock.NewRows([]string{
			"symbol", "prediction_type", "horizon_len", "context_len", "stock_type",
			"covariate_config", "covariate_signature",
		}).AddRow("510050", "mtf-lite", 7, 2048, 2, []byte(`{}`), ""))

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict_once_cached" {
			t.Fatalf("gateway path = %s, want /predict_once_cached", r.URL.Path)
		}
		var received map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		if received["stock_code"] != "510050" {
			t.Fatalf("stock_code = %#v, want 510050", received["stock_code"])
		}
		if received["stock_type"] != float64(2) {
			t.Fatalf("stock_type = %#v, want 2", received["stock_type"])
		}
		if received["prediction_type"] != "mtf-lite" {
			t.Fatalf("prediction_type = %#v, want mtf-lite", received["prediction_type"])
		}
		if received["horizon_len"] != float64(7) || received["context_len"] != float64(2048) {
			t.Fatalf("horizon/context = %#v/%#v, want 7/2048", received["horizon_len"], received["context_len"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success":true,
			"message":"单次预测缓存命中",
			"data":{
				"stock_code":"510050",
				"stock_type":2,
				"prediction_type":"mtf-lite",
				"context_len":2048,
				"horizon_len":7,
				"latest_close":3.026,
				"future_dates":["2026-06-15","2026-06-16"],
				"best_prediction_item":"mtf-0.6",
				"best_prediction_values":[2.9595,2.9631],
				"predicted_change_percent":[-2.2,-2.08]
			}
		}`))
	}))
	defer gateway.Close()

	handler := NewOpenAPIHandler(
		services.NewOpenAPIService(&database.DB{Conn: db}),
		services.NewWatchlistService(&database.DB{Conn: db}, &config.Config{
			InferenceGateway: config.InferenceGatewayConfig{BaseURL: gateway.URL, Timeout: 1},
		}),
		nil,
		nil,
		nil,
	)
	router := gin.New()
	router.GET("/api/open/v1/mtf/future", handler.AuthMiddleware("mtf:read"), handler.MTFFuture)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/open/v1/mtf/future?unique_key=510050_best_hlen_7_clen_2048_v_2.5", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"unique_key":"510050_best_hlen_7_clen_2048_v_2.5"`) {
		t.Fatalf("expected unique_key passthrough, body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"future_dates":["2026-06-15","2026-06-16"]`) {
		t.Fatalf("expected future_dates from predict once, body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"best_prediction_values":[2.9595,2.9631]`) {
		t.Fatalf("expected predict once values, body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"predicted_change_percent":[-2.2,-2.08]`) {
		t.Fatalf("expected predicted_change_percent array from predict once, body=%s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func expectOpenAPIAuth(mock sqlmock.Sqlmock, apiKey string, scopes string, userID int, keyID int, now time.Time) {
	mock.ExpectQuery("SELECT k.id, k.owner_user_id, k.scopes, k.status, k.expires_at").
		WithArgs(services.HashOpenAPIKey(apiKey)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "scopes", "status", "expires_at",
			"user_id", "email", "username", "is_premium", "is_admin", "membership_level", "membership_expires_at", "created_at", "updated_at",
		}).AddRow(keyID, userID, scopes, "active", nil, userID, "alice@example.com", "alice", false, false, 3, nil, now, now))
	mock.ExpectExec("UPDATE open_api_keys SET last_used_at").
		WithArgs(keyID).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectWatchlistSymbolCheck(mock sqlmock.Sqlmock, userID int, symbol string, exists bool) {
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(userID, symbol).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(exists))
}

func assertOpenAPIErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	var body struct {
		Status string `json:"status"`
		Error  struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "error" || body.Error.Code != wantCode {
		t.Fatalf("body = %#v, want error code %s", body, wantCode)
	}
}

var _ = pq.Array
