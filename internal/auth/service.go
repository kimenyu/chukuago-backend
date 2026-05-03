package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service orchestrates authentication: OTP flow, token issuance, and session lifecycle.
type Service struct {
	store    *Store
	otpSvc   *OTPService
	tokenSvc *TokenService
	log      *zap.Logger
}

func NewService(store *Store, otpSvc *OTPService, tokenSvc *TokenService, log *zap.Logger) *Service {
	return &Service{store: store, otpSvc: otpSvc, tokenSvc: tokenSvc, log: log}
}

func (s *Service) SendOTP(ctx context.Context, req SendOTPRequest) error {
	if err := s.otpSvc.Send(ctx, req.Phone); err != nil {
		return fmt.Errorf("send otp: %w", err)
	}
	s.log.Info("OTP dispatched", zap.String("phone", req.Phone))
	return nil
}

func (s *Service) VerifyOTP(ctx context.Context, req VerifyOTPRequest, role string) (*TokenPair, error) {
	if err := s.otpSvc.Verify(ctx, req.Phone, req.OTP); err != nil {
		switch {
		case errors.Is(err, ErrOTPLocked):
			return nil, ErrOTPLocked
		case errors.Is(err, ErrOTPExpired):
			return nil, ErrOTPExpired
		default:
			return nil, ErrOTPInvalid
		}
	}

	user, err := s.store.GetOrCreateUserByPhone(ctx, req.Phone, role)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	
	if string(user.Role) != role {
        return nil, ErrRoleMismatch
    }

	return s.issuePair(ctx, user.ID, string(user.Role))
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := s.tokenSvc.Validate(refreshToken)
	if err != nil {
		return nil, ErrTokenInvalid
	}

	sess, err := s.store.GetSessionByToken(ctx, refreshToken)
	if err != nil {
		return nil, ErrSessionGone
	}

	if err := s.store.RevokeSession(ctx, sess.ID); err != nil {
		return nil, fmt.Errorf("revoke old session: %w", err)
	}

	return s.issuePair(ctx, claims.UserID, claims.Role)
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	sess, err := s.store.GetSessionByToken(ctx, refreshToken)
	if err != nil {
		return nil // already gone — idempotent
	}
	return s.store.RevokeSession(ctx, sess.ID)
}

func (s *Service) issuePair(ctx context.Context, userID uuid.UUID, role string) (*TokenPair, error) {
	access, err := s.tokenSvc.NewAccessToken(userID, role)
	if err != nil {
		return nil, fmt.Errorf("mint access token: %w", err)
	}

	refresh, err := s.tokenSvc.NewRefreshToken(userID, role)
	if err != nil {
		return nil, fmt.Errorf("mint refresh token: %w", err)
	}

	claims, err := s.tokenSvc.Validate(refresh)
	if err != nil {
		return nil, fmt.Errorf("validate refresh token: %w", err)
	}

	if err := s.store.CreateSession(ctx, userID, refresh, claims.ExpiresAt.Time); err != nil {
		return nil, fmt.Errorf("persist session: %w", err)
	}

	return &TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}
