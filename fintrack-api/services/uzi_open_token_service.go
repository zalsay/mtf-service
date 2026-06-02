package services

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var (
	ErrUZIOpenTokenInvalid = errors.New("uzi report open token is invalid")
	ErrUZIOpenTokenExpired = errors.New("uzi report open token is expired")
)

type uziOpenTokenRecord struct {
	UserID       int
	RelativePath string
	ExpiresAt    time.Time
}

type UZIOpenTokenService struct {
	mu     sync.Mutex
	tokens map[string]uziOpenTokenRecord
	now    func() time.Time
	ttl    time.Duration
}

func NewUZIOpenTokenService(ttl time.Duration) *UZIOpenTokenService {
	if ttl <= 0 {
		ttl = 45 * time.Second
	}
	return &UZIOpenTokenService{
		tokens: make(map[string]uziOpenTokenRecord),
		now:    time.Now,
		ttl:    ttl,
	}
}

func (s *UZIOpenTokenService) Create(userID int, relativePath string) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, errors.New("uzi open token service is not initialized")
	}

	token, err := randomURLToken(32)
	if err != nil {
		return "", time.Time{}, err
	}

	expiresAt := s.now().Add(s.ttl)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.deleteExpiredLocked()
	s.tokens[token] = uziOpenTokenRecord{
		UserID:       userID,
		RelativePath: relativePath,
		ExpiresAt:    expiresAt,
	}

	return token, expiresAt, nil
}

func (s *UZIOpenTokenService) Consume(token string) (int, string, error) {
	if s == nil {
		return 0, "", errors.New("uzi open token service is not initialized")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.tokens[token]
	if !ok {
		return 0, "", ErrUZIOpenTokenInvalid
	}
	delete(s.tokens, token)

	if !record.ExpiresAt.After(s.now()) {
		return 0, "", ErrUZIOpenTokenExpired
	}

	return record.UserID, record.RelativePath, nil
}

func (s *UZIOpenTokenService) Resolve(token string) (int, string, error) {
	if s == nil {
		return 0, "", errors.New("uzi open token service is not initialized")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.tokens[token]
	if !ok {
		return 0, "", ErrUZIOpenTokenInvalid
	}
	if !record.ExpiresAt.After(s.now()) {
		delete(s.tokens, token)
		return 0, "", ErrUZIOpenTokenExpired
	}

	return record.UserID, record.RelativePath, nil
}

func (s *UZIOpenTokenService) deleteExpiredLocked() {
	now := s.now()
	for token, record := range s.tokens {
		if !record.ExpiresAt.After(now) {
			delete(s.tokens, token)
		}
	}
}

func randomURLToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
