package handlers

import (
	"errors"
	"testing"
)

func TestIsRegisterInviteError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "missing invite", err: errors.New("invite code not found"), want: true},
		{name: "disabled invite", err: errors.New("invite code is disabled"), want: true},
		{name: "empty invite", err: errors.New("invite code is required"), want: true},
		{name: "usage limit reached", err: errors.New("invite code usage limit reached"), want: true},
		{name: "database error", err: errors.New("failed to create user"), want: false},
		{name: "nil error", err: nil, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRegisterInviteError(tc.err); got != tc.want {
				t.Fatalf("isRegisterInviteError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
