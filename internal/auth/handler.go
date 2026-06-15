package auth

import (
	"errors"
	"net/http"

	"github.com/chukuago/api/pkg/response"
	"github.com/chukuago/api/pkg/validator"
	"go.uber.org/zap"
)

// Handler exposes auth endpoints over HTTP.
type Handler struct {
	svc *Service
	log *zap.Logger
}

func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// SendOTP godoc
// POST /api/v1/auth/send-otp
func (h *Handler) SendOTP(w http.ResponseWriter, r *http.Request) {
	var req SendOTPRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	if err := h.svc.SendOTP(r.Context(), req); err != nil {
		h.log.Error("SendOTP failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "OTP sent successfully"})
}

// VerifyOTP godoc
// POST /api/v1/auth/verify-otp
func (h *Handler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req VerifyOTPRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	// Role is provided at registration (send-otp step).
	type verifyBody struct {
		VerifyOTPRequest
		Role string `json:"role" validate:"required,oneof=client runner"`
	}

	// Re-decode into extended body to capture role.
	var body verifyBody
	body.VerifyOTPRequest = req
	// Role defaults to client if not present on re-use (session-based re-login).
	
	roleHint := r.URL.Query().Get("role")
	if roleHint == "" {
		roleHint = "client"
	}

	pair, err := h.svc.VerifyOTP(r.Context(), req, roleHint)
	if err != nil {
		switch {
		case errors.Is(err, ErrOTPInvalid):
			response.BadRequest(w, "OTP_INVALID", "The code you entered is incorrect.")
		case errors.Is(err, ErrOTPExpired):
			response.BadRequest(w, "OTP_EXPIRED", "Your code has expired. Please request a new one.")
		case errors.Is(err, ErrOTPLocked):
			response.Error(w, http.StatusTooManyRequests, "OTP_LOCKED", "Too many attempts. Please request a new code.")
		case errors.Is(err, ErrRoleMismatch):
			response.Error(w, http.StatusConflict, "ROLE_MISMATCH", "This number is registered as a different account type.")
		default:
			h.log.Error("VerifyOTP internal error", zap.Error(err))
			response.InternalError(w)
		}
		return
	}

	response.JSON(w, http.StatusOK, pair)
}

// Refresh godoc
// POST /api/v1/auth/refresh
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	pair, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrTokenInvalid), errors.Is(err, ErrSessionGone):
			response.Unauthorized(w, "Refresh token is invalid or expired. Please log in again.")
		default:
			h.log.Error("Refresh failed", zap.Error(err))
			response.InternalError(w)
		}
		return
	}

	response.JSON(w, http.StatusOK, pair)
}

// Logout godoc
// POST /api/v1/auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		h.log.Error("Logout failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
