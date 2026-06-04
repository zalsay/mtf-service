package payment

import (
	"testing"
	"time"
)

func TestLocalVerifierAcceptsSignedCredential(t *testing.T) {
	verifier := NewLocalVerifier("test-secret", time.Hour)
	credential, err := verifier.IssueDevCredential("order-1", "resource.demo", time.Now())
	if err != nil {
		t.Fatalf("IssueDevCredential error = %v", err)
	}

	result, err := verifier.Verify(t.Context(), credential, VerifyRequest{
		ResourceID: "resource.demo",
		OrderID:    "order-1",
	})
	if err != nil {
		t.Fatalf("Verify error = %v", err)
	}
	if !result.Valid {
		t.Fatalf("Valid = false, want true: %#v", result)
	}
}

func TestLocalVerifierRejectsWrongResource(t *testing.T) {
	verifier := NewLocalVerifier("test-secret", time.Hour)
	credential, err := verifier.IssueDevCredential("order-1", "resource.demo", time.Now())
	if err != nil {
		t.Fatalf("IssueDevCredential error = %v", err)
	}

	result, err := verifier.Verify(t.Context(), credential, VerifyRequest{
		ResourceID: "resource.other",
		OrderID:    "order-1",
	})
	if err != nil {
		t.Fatalf("Verify error = %v", err)
	}
	if result.Valid {
		t.Fatalf("Valid = true, want false")
	}
}
