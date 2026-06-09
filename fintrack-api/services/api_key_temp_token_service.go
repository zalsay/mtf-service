package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"fintrack-api/config"

	"github.com/redis/go-redis/v9"
)

const APIKeyTempTokenTTL = 5 * time.Minute

type APIKeyTempTokenStore interface {
	Save(ctx context.Context, token string, userID int, ttl time.Duration) error
	Consume(ctx context.Context, token string) (int, bool, error)
}

type memoryAPIKeyTempTokenRecord struct {
	UserID    int
	ExpiresAt time.Time
}

type MemoryAPIKeyTempTokenStore struct {
	mu     sync.Mutex
	tokens map[string]memoryAPIKeyTempTokenRecord
}

func NewAPIKeyTempTokenStore(cfg config.RedisConfig) APIKeyTempTokenStore {
	if !cfg.Enabled {
		return NewMemoryAPIKeyTempTokenStore()
	}
	return NewRedisAPIKeyTempTokenStore(cfg)
}

func NewMemoryAPIKeyTempTokenStore() *MemoryAPIKeyTempTokenStore {
	return &MemoryAPIKeyTempTokenStore{tokens: make(map[string]memoryAPIKeyTempTokenRecord)}
}

func GenerateAPIKeyTempToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func (s *MemoryAPIKeyTempTokenStore) Save(ctx context.Context, token string, userID int, ttl time.Duration) error {
	if s == nil {
		return fmt.Errorf("memory token store is not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" || userID <= 0 {
		return fmt.Errorf("invalid token payload")
	}
	if ttl <= 0 {
		return fmt.Errorf("invalid token ttl")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = memoryAPIKeyTempTokenRecord{
		UserID:    userID,
		ExpiresAt: time.Now().Add(ttl),
	}
	return nil
}

func (s *MemoryAPIKeyTempTokenStore) Consume(ctx context.Context, token string) (int, bool, error) {
	if s == nil {
		return 0, false, fmt.Errorf("memory token store is not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.tokens[token]
	if !ok {
		return 0, false, nil
	}
	delete(s.tokens, token)
	if time.Now().After(record.ExpiresAt) {
		return 0, false, nil
	}
	return record.UserID, true, nil
}

type RedisAPIKeyTempTokenStore struct {
	client *redis.Client
}

func NewRedisAPIKeyTempTokenStore(cfg config.RedisConfig) *RedisAPIKeyTempTokenStore {
	return &RedisAPIKeyTempTokenStore{
		client: redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
			Password: cfg.Password,
			DB:       cfg.DB,
		}),
	}
}

func (s *RedisAPIKeyTempTokenStore) Save(ctx context.Context, token string, userID int, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("redis token store is not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" || userID <= 0 {
		return fmt.Errorf("invalid token payload")
	}
	return s.client.Set(ctx, apiKeyTempTokenRedisKey(token), strconv.Itoa(userID), ttl).Err()
}

func (s *RedisAPIKeyTempTokenStore) Consume(ctx context.Context, token string) (int, bool, error) {
	if s == nil || s.client == nil {
		return 0, false, fmt.Errorf("redis token store is not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, false, nil
	}
	value, err := s.client.GetDel(ctx, apiKeyTempTokenRedisKey(token)).Result()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	userID, err := strconv.Atoi(value)
	if err != nil || userID <= 0 {
		return 0, false, fmt.Errorf("invalid stored token user")
	}
	return userID, true, nil
}

func apiKeyTempTokenRedisKey(token string) string {
	return "fintrack:open_api:temp_token:" + strings.TrimSpace(token)
}
