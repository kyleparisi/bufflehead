package aws

import (
	"errors"
	"strings"
	"testing"
)

// realWorldSSOError is the exact error surfaced to the UI when an SSO login
// expires (from a build rds auth token failure).
const realWorldSSOError = "connection health check failed: build rds auth token: failed to refresh cached credentials, refresh cached SSO token failed, unable to refresh SSO token, operation error SSO OIDC: CreateToken, https response error StatusCode: 400, RequestID: 05eb936b-1c85-44a5-89c3-7c292d6080af, InvalidGrantException:"

func TestIsAuthErrorString(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{"real-world expired SSO", realWorldSSOError, true},
		{"invalid grant exception", "operation error SSO OIDC: CreateToken, InvalidGrantException:", true},
		{"refresh cached sso token failed", "refresh cached SSO token failed", true},
		{"unable to refresh sso token", "unable to refresh SSO token", true},
		{"failed to refresh cached credentials", "failed to refresh cached credentials", true},
		{"expired sso", "the SSO session has expired sso", true},
		{"expired token", "ExpiredToken: the security token included in the request is expired", true},
		{"bare sso token phrase", "could not load sso token", true},
		{"case insensitive", "INVALIDGRANTEXCEPTION", true},

		{"empty", "", false},
		{"unrelated postgres error", "pq: relation \"foo\" does not exist", false},
		{"connection refused", "dial tcp 127.0.0.1:5432: connect: connection refused", false},
		{"timeout", "connection timed out after 30s — tunnel may not be forwarding data", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAuthErrorString(tt.msg); got != tt.want {
				t.Errorf("IsAuthErrorString(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestFormatConnError_ExpiredLogin(t *testing.T) {
	msg, isAuth := FormatConnError(errors.New(realWorldSSOError))

	if !isAuth {
		t.Fatalf("expected isAuth=true for expired SSO error")
	}
	// The friendly message must not leak the raw AWS SDK noise.
	if strings.Contains(msg, "InvalidGrantException") || strings.Contains(msg, "SSO OIDC") {
		t.Errorf("friendly message leaked raw error text: %q", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "expired") {
		t.Errorf("friendly message should explain the login expired: %q", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "log in again") {
		t.Errorf("friendly message should guide the user to log in again: %q", msg)
	}
}

func TestFormatConnError_NonAuth(t *testing.T) {
	raw := "pq: relation \"foo\" does not exist"
	msg, isAuth := FormatConnError(errors.New(raw))

	if isAuth {
		t.Errorf("expected isAuth=false for non-auth error")
	}
	if msg != raw {
		t.Errorf("non-auth error should pass through unchanged: got %q, want %q", msg, raw)
	}
}

func TestFormatConnError_Nil(t *testing.T) {
	msg, isAuth := FormatConnError(nil)
	if isAuth {
		t.Errorf("expected isAuth=false for nil error")
	}
	if msg != "" {
		t.Errorf("expected empty message for nil error, got %q", msg)
	}
}
