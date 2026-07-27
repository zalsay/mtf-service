package main

import (
	"context"
	"path/filepath"
	"testing"

	"ai-functions/internal/store"
)

func TestDailySyncSchedulesIncludesPrimaryAndExtraTimes(t *testing.T) {
	schedules := dailySyncSchedules(22, 0, "01:00,22:00,bad,09:30")

	if got := formatDailySyncSchedules(schedules); got != "22:00,01:00,09:30" {
		t.Fatalf("unexpected schedules: %s", got)
	}
}

func TestParseDailySyncTimeRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{"", "1", "24:00", "01:60", "aa:00"} {
		if schedule, ok := parseDailySyncTime(raw); ok {
			t.Fatalf("expected %q to be rejected, got %#v", raw, schedule)
		}
	}
}

func TestLevel1DailyTokenDefaultsToPostgresHandlerToken(t *testing.T) {
	t.Setenv("A_STOCK_DAILY_TOKEN", "")
	t.Setenv("LEVEL1_DAILY_TOKEN", "")
	t.Setenv("DAILY_STOCK_SYNC_TOKEN", "")

	if got := level1DailyToken("postgres-token"); got != "postgres-token" {
		t.Fatalf("level1DailyToken() = %q, want postgres-token", got)
	}
}

func TestLevel1DailyTokenCanBeOverridden(t *testing.T) {
	t.Setenv("A_STOCK_DAILY_TOKEN", "level1-token")
	t.Setenv("LEVEL1_DAILY_TOKEN", "")
	t.Setenv("DAILY_STOCK_SYNC_TOKEN", "")

	if got := level1DailyToken("postgres-token"); got != "level1-token" {
		t.Fatalf("level1DailyToken() = %q, want level1-token", got)
	}
}

func TestNewGatewayStoreSupportsMemoryMode(t *testing.T) {
	t.Setenv("GATEWAY_STORE", "memory")

	jobStore, mode, err := newGatewayStore()
	if err != nil {
		t.Fatalf("newGatewayStore() error = %v", err)
	}
	defer jobStore.Close()
	if mode != "memory" {
		t.Fatalf("store mode = %q, want memory", mode)
	}
	if err := jobStore.Ping(context.Background()); err != nil {
		t.Fatalf("memory store Ping() error = %v", err)
	}
}

func TestNewGatewayStoreRejectsUnknownMode(t *testing.T) {
	t.Setenv("GATEWAY_STORE", "unknown")

	jobStore, mode, err := newGatewayStore()
	if err == nil || jobStore != nil {
		t.Fatalf("newGatewayStore() = store=%#v mode=%q err=%v, want configuration error", jobStore, mode, err)
	}
}

func TestNewGatewayStoreUsesHybridSQLiteMode(t *testing.T) {
	t.Setenv("GATEWAY_STORE", "sqlite")
	t.Setenv("GATEWAY_SQLITE_PATH", filepath.Join(t.TempDir(), "gateway.db"))

	jobStore, mode, err := newGatewayStore()
	if err != nil {
		t.Fatalf("newGatewayStore() error = %v", err)
	}
	defer jobStore.Close()
	if mode != "sqlite" {
		t.Fatalf("store mode = %q, want sqlite", mode)
	}
	if _, ok := jobStore.(*store.HybridStore); !ok {
		t.Fatalf("store type = %T, want *store.HybridStore", jobStore)
	}
}
