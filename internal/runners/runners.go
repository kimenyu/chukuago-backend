package runners

import (
	"context"
	"errors"
	"net/http"

	pkgtypes "github.com/chukuago/api/pkg/types"
	"github.com/chukuago/api/pkg/response"
	"github.com/chukuago/api/pkg/validator"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ---- Service ---------------------------------------------------------------

type Service struct {
	store *Store
	log   *zap.Logger
}

func NewService(store *Store, userStore interface{}, log *zap.Logger) *Service {
	return &Service{store: store, log: log}
}

func (s *Service) SubmitKYC(ctx context.Context, req SubmitKYCRequest) error {
	userID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.store.EnsureProfile(ctx, userID); err != nil {
		return err
	}

	// Guard: don't allow re-submission once approved.
	profile, err := s.store.GetProfile(ctx, userID)
	if err != nil {
		return err
	}
	if profile != nil && (profile.KYCStatus == pkgtypes.KYCApproved || profile.KYCStatus == pkgtypes.KYCPending) {
		return ErrKYCAlreadyDone
	}

	return s.store.SubmitKYC(ctx, userID, req)
}

func (s *Service) GetKYCStatus(ctx context.Context) (*KYCStatusResponse, error) {
	userID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	doc, err := s.store.GetKYCStatus(ctx, userID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return &KYCStatusResponse{Status: string(pkgtypes.KYCUnsubmitted)}, nil
	}
	return &KYCStatusResponse{Status: doc.Status, Notes: doc.Notes}, nil
}

func (s *Service) SetAvailability(ctx context.Context, req SetAvailabilityRequest) error {
	userID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}
	return s.store.SetAvailability(ctx, userID, req.IsAvailable)
}

func (s *Service) AddServiceArea(ctx context.Context, req AddServiceAreaRequest) (*ServiceAreaResponse, error) {
	userID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	area, err := s.store.AddServiceArea(ctx, userID, req)
	if err != nil {
		return nil, err
	}

	resp := &ServiceAreaResponse{ID: area.ID.String(), Label: area.Label}
	if area.RegionID != nil {
		s := area.RegionID.String()
		resp.RegionID = &s
	}
	return resp, nil
}

func (s *Service) RemoveServiceArea(ctx context.Context, areaID uuid.UUID) error {
	userID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}
	return s.store.RemoveServiceArea(ctx, userID, areaID)
}

// ---- Handler ---------------------------------------------------------------

type Handler struct {
	svc *Service
	log *zap.Logger
}

func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// SubmitKYC godoc
// POST /api/v1/runner/kyc
func (h *Handler) SubmitKYC(w http.ResponseWriter, r *http.Request) {
	var req SubmitKYCRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	if err := h.svc.SubmitKYC(r.Context(), req); err != nil {
		if errors.Is(err, ErrKYCAlreadyDone) {
			response.Conflict(w, "KYC_ALREADY_SUBMITTED", "KYC has already been submitted or approved.")
			return
		}
		h.log.Error("SubmitKYC failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.JSON(w, http.StatusAccepted, map[string]string{
		"message": "KYC submitted successfully. Review takes 24–48 hours.",
	})
}

// KYCStatus godoc
// GET /api/v1/runner/kyc/status
func (h *Handler) KYCStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetKYCStatus(r.Context())
	if err != nil {
		h.log.Error("KYCStatus failed", zap.Error(err))
		response.InternalError(w)
		return
	}
	response.JSON(w, http.StatusOK, resp)
}

// SetAvailability godoc
// PATCH /api/v1/runner/availability
func (h *Handler) SetAvailability(w http.ResponseWriter, r *http.Request) {
	var req SetAvailabilityRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	if err := h.svc.SetAvailability(r.Context(), req); err != nil {
		h.log.Error("SetAvailability failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AddServiceArea godoc
// POST /api/v1/runner/service-areas
func (h *Handler) AddServiceArea(w http.ResponseWriter, r *http.Request) {
	var req AddServiceAreaRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	area, err := h.svc.AddServiceArea(r.Context(), req)
	if err != nil {
		h.log.Error("AddServiceArea failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.JSON(w, http.StatusCreated, area)
}

// RemoveServiceArea godoc
// DELETE /api/v1/runner/service-areas/:id
func (h *Handler) RemoveServiceArea(w http.ResponseWriter, r *http.Request) {
	areaID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "service area ID must be a valid UUID")
		return
	}

	if err := h.svc.RemoveServiceArea(r.Context(), areaID); err != nil {
		if errors.Is(err, ErrAreaNotFound) {
			response.NotFound(w, "SERVICE_AREA_NOT_FOUND")
			return
		}
		h.log.Error("RemoveServiceArea failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
