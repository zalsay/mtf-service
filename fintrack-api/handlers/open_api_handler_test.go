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
	"fintrack-api/services"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

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

	pythonCalled := false
	python := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pythonCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer python.Close()

	postgres := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/save-predictions/mtf-direct/by-request" {
			t.Fatalf("postgres path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("stock_type") != "2" {
			t.Fatalf("stock_type = %q, want 2", r.URL.Query().Get("stock_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"prediction_id":"cached-1"}}`))
	}))
	defer postgres.Close()

	cfg := &config.Config{
		PythonService: config.PythonServiceConfig{BaseURL: python.URL, Timeout: 1},
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
	req := httptest.NewRequest(http.MethodPost, "/api/open/v1/mtf/predict-once", strings.NewReader(`{"stock_code":"510300","stock_type":2,"horizon_len":7,"context_len":256,"prediction_type":"mtf-lite","prefer_cache":true}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if pythonCalled {
		t.Fatal("expected cached response without calling python predict_once")
	}
	if !strings.Contains(rec.Body.String(), `"cache_hit":true`) {
		t.Fatalf("expected cache_hit response, body=%s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

var _ = pq.Array
