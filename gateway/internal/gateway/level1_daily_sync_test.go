package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLevel1TriggerDailyPostsDateConcurrentAndToken(t *testing.T) {
	var payload struct {
		Date       string `json:"date"`
		Concurrent int    `json:"concurrent"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/daily" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Token"); got != "token" {
			t.Fatalf("expected X-Token header, got %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		_ = json.NewEncoder(w).Encode(level1TriggerResponse{
			Date:     payload.Date,
			Mode:     "daily",
			ExitCode: 0,
		})
	}))
	defer server.Close()

	syncer := NewLevel1DailySyncer(server.URL, "", "token", time.UTC, 22, 0, 37)
	response, err := syncer.triggerDaily(context.Background(), "20260602")
	if err != nil {
		t.Fatalf("triggerDaily() error: %v", err)
	}
	if payload.Date != "20260602" || payload.Concurrent != 37 {
		t.Fatalf("unexpected trigger payload: %#v", payload)
	}
	if response.Date != "20260602" || response.Mode != "daily" || response.ExitCode != 0 {
		t.Fatalf("unexpected trigger response: %#v", response)
	}
}

func TestLevel1RunOnceSkipsNonTradingDay(t *testing.T) {
	triggerCalls := 0
	triggerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		triggerCalls++
		t.Fatalf("trigger server should not be called on non-trading day")
	}))
	defer triggerServer.Close()

	historyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/trading-day" {
			t.Fatalf("unexpected history path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Token"); got != "token" {
			t.Fatalf("expected X-Token header, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(tradingDayResponse{
			Code:         http.StatusOK,
			Date:         r.URL.Query().Get("date"),
			IsTradingDay: false,
		})
	}))
	defer historyServer.Close()

	syncer := NewLevel1DailySyncer(triggerServer.URL, historyServer.URL, "token", time.UTC, 22, 0, 50)
	if err := syncer.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if triggerCalls != 0 {
		t.Fatalf("expected no trigger calls, got %d", triggerCalls)
	}
}

func TestLevel1RunOnceUsesSeparateHistoryAndTriggerTokens(t *testing.T) {
	triggerCalls := 0
	triggerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		triggerCalls++
		if got := r.Header.Get("X-Token"); got != "level1-token" {
			t.Fatalf("expected trigger X-Token header, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(level1TriggerResponse{
			Date:     "20260602",
			Mode:     "daily",
			ExitCode: 0,
		})
	}))
	defer triggerServer.Close()

	historyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/trading-day" {
			t.Fatalf("unexpected history path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Token"); got != "history-token" {
			t.Fatalf("expected history X-Token header, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(tradingDayResponse{
			Code:         http.StatusOK,
			Date:         r.URL.Query().Get("date"),
			IsTradingDay: true,
		})
	}))
	defer historyServer.Close()

	syncer := NewLevel1DailySyncerWithTokens(triggerServer.URL, historyServer.URL, "level1-token", "history-token", time.UTC, 22, 0, 50)
	if err := syncer.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if triggerCalls != 1 {
		t.Fatalf("expected one trigger call, got %d", triggerCalls)
	}
}
