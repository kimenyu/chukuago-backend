package client

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/kimenyu/chukuagobackend/services/auth"
	"github.com/kimenyu/chukuagobackend/types"
	"github.com/kimenyu/chukuagobackend/utils"
)

type Handler struct {
	profileStore types.ProfileStore
	userStore    types.UserStore
}

func NewHandler(profile types.ProfileStore, user types.UserStore) *Handler {
	return &Handler{
		profileStore: profile,
		userStore:    user,
	}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	// pass the userStore to JWT middleware
	router.With(auth.WithJWTAuth(h.userStore)).Get("/profile/me", h.handleGetClientProfile)
}

// get client profile
func (h *Handler) handleGetClientProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, err := types.UserIDFromContext(ctx)
	if err != nil {
		utils.WriteError(w, http.StatusUnauthorized, err)
		return
	}

	clientProfile, err := h.profileStore.GetClientProfile(ctx, userID)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, err)
		return
	}

	utils.WriteJSON(w, http.StatusOK, clientProfile)
}

// update client profile
func (h *Handler) handleUpdateClientProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, _ := types.UserIDFromContext(ctx)

	if userID == uuid.Nil {
		utils.WriteError(w, http.StatusUnauthorized, fmt.Errorf("unauthorized"))
		return
	}

	var input types.ClientProfilePayload
	if err := utils.ParseJSON(r, &input); err != nil {
		utils.WriteError(w, http.StatusBadRequest, err)
		return
	}

	if err := utils.Validate.Struct(&input); err != nil {
		utils.WriteError(w, http.StatusBadRequest, err)
		return
	}

	clientProfile := &types.ClientProfile{
		UserID:     userID,
		Bio:        input.Bio,
		ProfilePic: input.ProfilePicture,
		UpdatedAt:  time.Now(),
	}

	if err := h.profileStore.UpdateClientProfile(ctx, clientProfile); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err)
		return
	}

	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "update successful"})
}
