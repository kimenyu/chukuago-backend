package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	otpMaxAttempts = 3
	otpKeyPrefix   = "otp:"
	otpAttemptsKey = "otp_attempts:"
)

// SMSProvider abstracts the third-party SMS gateway.

type SMSProvider interface {
	SendSMS(ctx context.Context, phone, message string) error
}

// OTPService manages generation, storage, and verification of phone OTPs.
type OTPService struct {
	redis  *redis.Client
	sms    SMSProvider
	expiry time.Duration
}

// NewOTPService creates an OTPService backed by Redis and an SMS provider.
func NewOTPService(redis *redis.Client, sms SMSProvider, expiry time.Duration) *OTPService {
	return &OTPService{redis: redis, sms: sms, expiry: expiry}
}

// Send generates a 6-digit OTP, stores it in Redis, and dispatches an SMS.
func (s *OTPService) Send(ctx context.Context, phone string) error {
	otp, err := generateOTP()
	if err != nil {
		return fmt.Errorf("generate otp: %w", err)
	}

	key := otpKeyPrefix + phone
	attemptsKey := otpAttemptsKey + phone

	pipe := s.redis.Pipeline()
	pipe.Set(ctx, key, otp, s.expiry)
	pipe.Del(ctx, attemptsKey) // reset attempt counter on resend
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("store otp in redis: %w", err)
	}

	msg := fmt.Sprintf("Your ChukuaGo verification code is: %s. Valid for %d minutes. Do not share this code.", otp, int(s.expiry.Minutes()))
	if err := s.sms.SendSMS(ctx, phone, msg); err != nil {
		return fmt.Errorf("send sms: %w", err)
	}

	return nil
}

// Verify checks the supplied OTP against the stored value.
// Returns ErrOTPInvalid, ErrOTPExpired, or ErrOTPLocked on failure.
func (s *OTPService) Verify(ctx context.Context, phone, supplied string) error {
	attemptsKey := otpAttemptsKey + phone

	attempts, err := s.redis.Incr(ctx, attemptsKey).Result()
	if err != nil {
		return fmt.Errorf("increment attempts: %w", err)
	}
	// Set TTL on first increment so the lock expires naturally.
	if attempts == 1 {
		s.redis.Expire(ctx, attemptsKey, s.expiry) //nolint:errcheck
	}
	if attempts > otpMaxAttempts {
		return ErrOTPLocked
	}

	key := otpKeyPrefix + phone
	stored, err := s.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return ErrOTPExpired
	}
	if err != nil {
		return fmt.Errorf("fetch otp from redis: %w", err)
	}

	if stored != supplied {
		return ErrOTPInvalid
	}

	// Consume the OTP so it cannot be reused.
	s.redis.Del(ctx, key, attemptsKey) //nolint:errcheck

	return nil
}

// generateOTP returns a cryptographically random 6-digit string.
func generateOTP() (string, error) {
	max := big.NewInt(1_000_000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
