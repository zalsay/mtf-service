package services

import (
	"context"
	"testing"
	"time"

	"fintrack-api/config"
)

func TestNewAPIKeyTempTokenStoreUsesMemoryWhenRedisDisabled(t *testing.T) {
	store := NewAPIKeyTempTokenStore(config.RedisConfig{Enabled: false})

	if _, ok := store.(*MemoryAPIKeyTempTokenStore); !ok {
		t.Fatalf("store type = %T, want *MemoryAPIKeyTempTokenStore", store)
	}
}

func TestNewAPIKeyTempTokenStoreUsesRedisWhenEnabled(t *testing.T) {
	store := NewAPIKeyTempTokenStore(config.RedisConfig{Enabled: true})

	if _, ok := store.(*RedisAPIKeyTempTokenStore); !ok {
		t.Fatalf("store type = %T, want *RedisAPIKeyTempTokenStore", store)
	}
}

func TestMemoryAPIKeyTempTokenStoreConsumesOnce(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryAPIKeyTempTokenStore()

	if err := store.Save(ctx, "token-1", 7, time.Minute); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	userID, ok, err := store.Consume(ctx, "token-1")
	if err != nil {
		t.Fatalf("Consume first error = %v", err)
	}
	if !ok || userID != 7 {
		t.Fatalf("Consume first = (%d, %v), want (7, true)", userID, ok)
	}

	userID, ok, err = store.Consume(ctx, "token-1")
	if err != nil {
		t.Fatalf("Consume second error = %v", err)
	}
	if ok || userID != 0 {
		t.Fatalf("Consume second = (%d, %v), want (0, false)", userID, ok)
	}
}

func TestMemoryAPIKeyTempTokenStoreRejectsExpiredToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryAPIKeyTempTokenStore()

	if err := store.Save(ctx, "token-1", 7, time.Nanosecond); err != nil {
		t.Fatalf("Save error = %v", err)
	}
	time.Sleep(time.Millisecond)

	userID, ok, err := store.Consume(ctx, "token-1")
	if err != nil {
		t.Fatalf("Consume error = %v", err)
	}
	if ok || userID != 0 {
		t.Fatalf("Consume expired = (%d, %v), want (0, false)", userID, ok)
	}
}
