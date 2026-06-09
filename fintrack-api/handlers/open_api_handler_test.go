package handlers

import (
	"context"
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
	mock.ExpectQuery("SELECT k.id, k.owner_user_id, k.scopes, k.status, k.expires_at").
		WithArgs(services.HashOpenAPIKey(apiKey)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "scopes", "status", "expires_at",
			"user_id", "email", "username", "is_premium", "is_admin", "membership_level", "membership_expires_at", "created_at", "updated_at",
		}).AddRow(3, 7, "{mtf:predict}", "active", nil, 7, "alice@example.com", "alice", false, false, 3, nil, now, now))
	mock.ExpectExec("UPDATE open_api_keys SET last_used_at").
		WithArgs(3).
		WillReturnResult(sqlmock.NewResult(0, 1))

	gatewayCalled := false
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	futureDate := time.Now().UTC().Format("2006-01-02")
	postgres := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/save-predictions/mtf-direct/by-request" {
			t.Fatalf("postgres path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("stock_type") != "2" {
			t.Fatalf("stock_type = %q, want 2", r.URL.Query().Get("stock_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code":0,
			"message":"ok",
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
				"adjust_raw_best_prediction_values":[1.11],
				"adjust_raw_latest_close":1.01,
				"predictions":{"mtf-0.5":[1.23]},
				"cache_hit":true
			}
		}`))
	}))
	defer postgres.Close()

	cfg := &config.Config{
		InferenceGateway: config.InferenceGatewayConfig{BaseURL: gateway.URL, Timeout: 1},
		PostgresHandler: config.PostgresHandlerConfig{
			BaseURL: postgres.URL,
			Timeout: 1,
		},
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
	if gatewayCalled {
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

var _ = pq.Array
