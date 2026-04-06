package auth

import "errors"

var (
	ErrOTPInvalid   = errors.New("invalid OTP")
	ErrOTPExpired   = errors.New("OTP has expired")
	ErrOTPLocked    = errors.New("OTP is locked after too many attempts")
	ErrUserNotFound = errors.New("user not found")
	ErrTokenInvalid = errors.New("token is invalid or expired")
	ErrSessionGone  = errors.New("session has been revoked or expired")
)
