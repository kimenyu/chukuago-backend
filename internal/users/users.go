package users

import (
	"context"
	"errors"
	"net/http"

	"github.com/chukuago/api/pkg/response"
	pkgtypes "github.com/chukuago/api/pkg/types"
	"github.com/chukuago/api/pkg/validator"
	"go.uber.org/zap"
)

// ---- Errors ----------------------------------------------------------------

var ErrNotFound = errors.New("user not found")

// ---- Service ---------------------------------------------------------------

type Service struct {
	store *Store
	log   *zap.Logger
}

func NewService(store *Store, log *zap.Logger) *Service {
	return &Service{store: store, log: log}
}

func (s *Service) GetProfile(ctx context.Context) (*ProfileResponse, error) {
	userID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	user, err := s.store.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	resp := &ProfileResponse{
		ID:    user.ID.String(),
		Phone: user.Phone,
		Email: user.Email,
		Name:  user.Name,
		Role:  string(user.Role),
	}

	switch user.Role {
	case pkgtypes.UserRoleClient:
		cp, _ := s.store.GetClientProfile(ctx, userID)
		if cp != nil {
			resp.ClientProfile = &ClientProfileDTO{
				Bio:               cp.Bio,
				ProfilePic:        cp.ProfilePic,
				PreferredCurrency: cp.PreferredCurrency,
				TotalErrands:      cp.TotalErrands,
				ActiveErrands:     cp.ActiveErrands,
			}
		}
	case pkgtypes.UserRoleRunner:
		rp, _ := s.store.GetRunnerProfile(ctx, userID)
		if rp != nil {
			resp.RunnerProfile = &RunnerProfileDTO{
				Bio:            rp.Bio,
				KYCStatus:      string(rp.KYCStatus),
				VehicleType:    rp.VehicleType,
				IsAvailable:    rp.IsAvailable,
				RatingAvg:      rp.RatingAvg,
				RatingCount:    rp.RatingCount,
				CompletedCount: rp.CompletedCount,
			}
		}
	}

	return resp, nil
}

func (s *Service) UpdateProfile(ctx context.Context, req UpdateProfileRequest) error {
	userID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}
	return s.store.UpdateProfile(ctx, userID, req)
}

func (s *Service) UpdateLocation(ctx context.Context, req UpdateLocationRequest) error {
	userID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}
	return s.store.UpdateLocation(ctx, userID, req.Lat, req.Lng)
}

// ---- Handler ---------------------------------------------------------------

type Handler struct {
	svc *Service
	log *zap.Logger
}

func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// GetProfile godoc
// GET /api/v1/profile
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := h.svc.GetProfile(r.Context())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.NotFound(w, "USER_NOT_FOUND")
			return
		}
		h.log.Error("GetProfile failed", zap.Error(err))
		response.InternalError(w)
		return
	}
	response.JSON(w, http.StatusOK, profile)
}

// UpdateProfile godoc
// PATCH /api/v1/profile
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req UpdateProfileRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	if err := h.svc.UpdateProfile(r.Context(), req); err != nil {
		h.log.Error("UpdateProfile failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "profile updated"})
}

// UpdateLocation godoc
// PATCH /api/v1/profile/location
func (h *Handler) UpdateLocation(w http.ResponseWriter, r *http.Request) {
	var req UpdateLocationRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	if err := h.svc.UpdateLocation(r.Context(), req); err != nil {
		h.log.Error("UpdateLocation failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
