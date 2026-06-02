package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"fintrack-api/config"

	"github.com/gin-gonic/gin"
)

func TestSetupRouterDoesNotPanicWithUZIReportRoutes(t *testing.T) {
	cfg := &config.Config{}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SetupRouter should not panic when registering UZI report routes: %v", r)
		}
	}()

	router := SetupRouter(cfg, nil)
	if router == nil {
		t.Fatal("SetupRouter returned nil router")
	}
}

func TestSetupRouterRegistersMTFAgentRoutes(t *testing.T) {
	cfg := &config.Config{}
	router := SetupRouter(cfg, nil)

	wantRoutes := map[string]string{
		"GET /api/v1/mtf-agent/session":               "",
		"GET /api/v1/mtf-agent/messages":              "",
		"POST /api/v1/mtf-agent/messages":             "",
		"POST /api/v1/mtf-agent/messages/stream":      "",
		"POST /api/v1/mtf-agent/messages/jobs":        "",
		"GET /api/v1/mtf-agent/messages/jobs/:jobID":  "",
		"POST /api/v1/mtf-agent/reset":                "",
		"GET /api/v1/mtf-agent/memory":                "",
		"DELETE /api/v1/mtf-agent/memory":             "",
		"GET /api/v1/mtf-agent/skills/history-trends": "",
		"GET /api/v1/mtf-agent/skills/uzi-reports":    "",
	}

	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := wantRoutes[key]; ok {
			wantRoutes[key] = route.Handler
		}
	}

	for route, handler := range wantRoutes {
		if handler == "" {
			t.Fatalf("missing MTF Agent route %s", route)
		}
	}
}

func TestSetupRouterRegistersFinanceNewsRoute(t *testing.T) {
	cfg := &config.Config{}
	router := SetupRouter(cfg, nil)

	for _, route := range router.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/finance-news" {
			return
		}
	}
	t.Fatal("missing finance news route GET /api/v1/finance-news")
}

func TestSetupRouterRegistersAdminGatewayQueueRoute(t *testing.T) {
	cfg := &config.Config{}
	router := SetupRouter(cfg, nil)

	for _, route := range router.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/admin/gateway-queue" {
			return
		}
	}
	t.Fatal("missing admin gateway queue route GET /api/v1/admin/gateway-queue")
}

func TestGzipResponseMiddlewareSkipsMTFAgentStreamRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(GzipResponseMiddleware())
	router.POST("/api/v1/mtf-agent/messages/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.String(http.StatusOK, "event:done\ndata:{}\n\n")
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/mtf-agent/messages/stream",
		nil,
	)
	request.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if encoding := recorder.Header().Get("Content-Encoding"); encoding != "" {
		t.Fatalf("SSE response must not be gzip encoded, got %q", encoding)
	}
}
