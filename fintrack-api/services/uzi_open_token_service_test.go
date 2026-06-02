package services

import (
	"testing"
	"time"
)

func TestUZIOpenTokenServiceConsumeOnce(t *testing.T) {
	service := NewUZIOpenTokenService(30 * time.Second)

	token, _, err := service.Create(12, "reports/demo.html")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	userID, relativePath, err := service.Consume(token)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if userID != 12 {
		t.Fatalf("Consume() userID = %d, want 12", userID)
	}
	if relativePath != "reports/demo.html" {
		t.Fatalf("Consume() relativePath = %q, want %q", relativePath, "reports/demo.html")
	}

	if _, _, err := service.Consume(token); err != ErrUZIOpenTokenInvalid {
		t.Fatalf("Consume() second time error = %v, want %v", err, ErrUZIOpenTokenInvalid)
	}
}

func TestUZIOpenTokenServiceExpired(t *testing.T) {
	service := NewUZIOpenTokenService(5 * time.Second)
	now := time.Now()
	service.now = func() time.Time { return now }

	token, _, err := service.Create(9, "reports/expired.html")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	service.now = func() time.Time { return now.Add(10 * time.Second) }

	if _, _, err := service.Consume(token); err != ErrUZIOpenTokenExpired {
		t.Fatalf("Consume() expired error = %v, want %v", err, ErrUZIOpenTokenExpired)
	}
}
