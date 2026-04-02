package client

import (
	"net/http"

	"github.com/go-chi/chi/v5"
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
	router.With(auth.WithJWTAuth(h.userStore)).Get("/profile", h.handleGetClientProfile)
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
