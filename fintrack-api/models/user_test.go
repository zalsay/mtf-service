package models

import (
	"encoding/json"
	"testing"
)

func TestRegisterRequestAcceptsOptionalActivationCode(t *testing.T) {
	var req RegisterRequest
	if err := json.Unmarshal([]byte(`{
		"email": "new@example.com",
		"username": "newuser",
		"password": "secret123",
		"activation_code": " vip 123 "
	}`), &req); err != nil {
		t.Fatalf("unmarshal register request: %v", err)
	}

	if req.ActivationCode != " vip 123 " {
		t.Fatalf("ActivationCode = %q, want %q", req.ActivationCode, " vip 123 ")
	}
}
