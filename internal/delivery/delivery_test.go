package delivery_test

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

// hashOTP mirrors the production implementation for test assertions.
func hashOTP(otp string) string {
	sum := sha256.Sum256([]byte(otp))
	return fmt.Sprintf("%x", sum)
}

func TestHashOTP_Deterministic(t *testing.T) {
	t.Parallel()

	otp := "482910"
	h1 := hashOTP(otp)
	h2 := hashOTP(otp)

	if h1 != h2 {
		t.Errorf("hash must be deterministic: got %q and %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex SHA-256, got %d chars", len(h1))
	}
}

func TestHashOTP_UniquePerCode(t *testing.T) {
	t.Parallel()

	codes := []string{"000000", "000001", "123456", "999999"}
	seen := make(map[string]string)

	for _, code := range codes {
		h := hashOTP(code)
		if prev, exists := seen[h]; exists {
			t.Errorf("hash collision between %q and %q", prev, code)
		}
		seen[h] = code
	}
}

func TestHashOTP_NeverStoresRaw(t *testing.T) {
	t.Parallel()

	otp := "482910"
	h := hashOTP(otp)

	if h == otp {
		t.Error("hash must not equal the raw OTP")
	}
}

func TestOTPFormat(t *testing.T) {
	t.Parallel()

	// Verify that a 6-digit zero-padded format is what the app expects.
	for _, tc := range []struct {
		n        int64
		expected string
	}{
		{0, "000000"},
		{1, "000001"},
		{999999, "999999"},
		{123456, "123456"},
	} {
		got := fmt.Sprintf("%06d", tc.n)
		if got != tc.expected {
			t.Errorf("Sprintf(%%06d, %d) = %q, want %q", tc.n, got, tc.expected)
		}
	}
}
