package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/chukuago/api/internal/auth"
	"github.com/google/uuid"
)

//  TokenService tests

func TestTokenService_RoundTrip(t *testing.T) {
	t.Parallel()

	svc := auth.NewTokenService("super-secret-test-key-32-bytes!!", 15*time.Minute, 7*24*time.Hour)
	userID := uuid.New()

	access, err := svc.NewAccessToken(userID, "client")
	if err != nil {
		t.Fatalf("NewAccessToken: %v", err)
	}

	claims, err := svc.Validate(access)
	if err != nil {
		t.Fatalf("Validate access token: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("UserID mismatch: got %s, want %s", claims.UserID, userID)
	}
	if claims.Role != "client" {
		t.Errorf("Role mismatch: got %s, want client", claims.Role)
	}
}

func TestTokenService_RefreshRoundTrip(t *testing.T) {
	t.Parallel()

	svc := auth.NewTokenService("super-secret-test-key-32-bytes!!", 15*time.Minute, 7*24*time.Hour)
	userID := uuid.New()

	refresh, err := svc.NewRefreshToken(userID, "runner")
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}

	claims, err := svc.Validate(refresh)
	if err != nil {
		t.Fatalf("Validate refresh token: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("UserID mismatch: got %s, want %s", claims.UserID, userID)
	}
	if claims.Role != "runner" {
		t.Errorf("Role mismatch: got %s, want runner", claims.Role)
	}
}

func TestTokenService_InvalidToken(t *testing.T) {
	t.Parallel()

	svc := auth.NewTokenService("super-secret-test-key-32-bytes!!", 15*time.Minute, 7*24*time.Hour)

	_, err := svc.Validate("not.a.real.token")
	if err == nil {
		t.Error("expected error validating a garbage token")
	}
}

func TestTokenService_WrongSecret(t *testing.T) {
	t.Parallel()

	signer := auth.NewTokenService("secret-A-32-bytes-padded-here!!!", 15*time.Minute, time.Hour)
	verifier := auth.NewTokenService("secret-B-32-bytes-padded-here!!!", 15*time.Minute, time.Hour)

	token, err := signer.NewAccessToken(uuid.New(), "client")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = verifier.Validate(token)
	if err == nil {
		t.Error("expected error when verifying with wrong secret")
	}
}

func TestTokenService_ExpiredToken(t *testing.T) {
	t.Parallel()

	// Mint a token that expires immediately.
	svc := auth.NewTokenService("super-secret-test-key-32-bytes!!", -1*time.Second, time.Hour)
	token, err := svc.NewAccessToken(uuid.New(), "client")
	if err != nil {
		t.Fatalf("NewAccessToken: %v", err)
	}

	// Wait a moment for the expiry to be in the past.
	time.Sleep(10 * time.Millisecond)

	_, err = svc.Validate(token)
	if err == nil {
		t.Error("expected error validating an expired token")
	}
}

// OTPService tests (with mock SMS provider)

type mockSMS struct {
	calls []struct{ phone, message string }
	err   error
}

func (m *mockSMS) SendSMS(_ context.Context, phone, message string) error {
	m.calls = append(m.calls, struct{ phone, message string }{phone, message})
	return m.err
}
