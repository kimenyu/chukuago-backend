package client

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/kimenyu/chukuagobackend/types"
	"github.com/kimenyu/chukuagobackend/utils"
)

type Handler struct {
	store types.ProfileStore
}

func NewHandler(store types.ProfileStore) *Handler {
	return &Handler{store: store}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Get("/users/{clientId}/profile", h.handleGetClientProfile)
}

// get client profile
func (h *Handler) handleGetClientProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	str := chi.URLParam(r, "clientId")

	userId, err := uuid.Parse(str)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, fmt.Errorf("invalid client id"))
		return
	}

	clientProfile, err := h.store.GetClientProfile(ctx, userId)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, err)
		return
	}

	utils.WriteJSON(w, http.StatusOK, clientProfile)
}
