package user

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/kimenyu/chukuagobackend/configs"
	"github.com/kimenyu/chukuagobackend/services/auth"
	"github.com/kimenyu/chukuagobackend/types"
	"github.com/kimenyu/chukuagobackend/utils"
)

type Handler struct {
	store types.UserStore
}

func NewHandler(store types.UserStore) *Handler {
	return &Handler{store: store}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Post("/login", h.handleLogin)
	router.Post("/register", h.handleRegister)
	router.Post("/register/runner", h.handleRegisterRunners)

	router.With(auth.WithJWTAuth(h.store)).Get("/users/{userID}", h.handleGetUser)
}

// LOGIN
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var payload types.LoginUserPayload

	if err := utils.ParseJSON(r, &payload); err != nil {
		utils.WriteError(w, http.StatusBadRequest, err)
		return
	}

	if err := utils.Validate.Struct(payload); err != nil {
		errors := err.(validator.ValidationErrors)
		utils.WriteError(w, http.StatusBadRequest, fmt.Errorf("invalid payload: %v", errors))
		return
	}

	user, err := h.store.GetUserByEmail(ctx, payload.Email)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, fmt.Errorf("invalid email or password"))
		return
	}

	if !auth.ComparePasswords(user.Password, []byte(payload.Password)) {
		utils.WriteError(w, http.StatusBadRequest, fmt.Errorf("invalid email or password"))
		return
	}

	secret := []byte(configs.Envs.JWTSecret)

	token, err := auth.CreateJWT(secret, user.ID.String())
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err)
		return
	}

	utils.WriteJSON(w, http.StatusOK, map[string]string{
		"token": token,
	})
}

// REGISTER
func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var payload types.RegisterUserPayload

	if err := utils.ParseJSON(r, &payload); err != nil {
		utils.WriteError(w, http.StatusBadRequest, err)
		return
	}

	if err := utils.Validate.Struct(payload); err != nil {
		errors := err.(validator.ValidationErrors)
		utils.WriteError(w, http.StatusBadRequest, fmt.Errorf("invalid payload: %v", errors))
		return
	}

	_, err := h.store.GetUserByEmail(ctx, payload.Email)
	if err == nil {
		utils.WriteError(w, http.StatusBadRequest, fmt.Errorf("user already exists"))
		return
	}
	if err.Error() != "user not found" {
		utils.WriteError(w, http.StatusInternalServerError, err)
		return
	}

	hashedPassword, err := auth.HashPassword(payload.Password)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err)
		return
	}

	user := &types.User{
		ID:        uuid.New(),
		Name:      payload.Name,
		Email:     payload.Email,
		Role:      types.UserRoleClient,
		Status:    types.UserStatusActive,
		Phone:     payload.Phone,
		Password:  hashedPassword,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.store.CreateUser(ctx, user); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err)
		return
	}

	// Auto-create client profile
	if _, err := h.store.GetOrCreateClientProfile(ctx, user.ID); err != nil {
		// Non-fatal: log it but don't fail the registration
		fmt.Printf("warn: failed to create client profile for %s: %v\n", user.ID, err)
	}
	if _, err := h.store.GetOrCreateWallet(ctx, user.ID); err != nil {
		fmt.Printf("warn: failed to create wallet for %s: %v\n", user.ID, err)
	}

	utils.WriteJSON(w, http.StatusCreated, map[string]string{
		"message": "user created successfully",
	})
}

// REGISTER RUNNER
func (h *Handler) handleRegisterRunners(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var payload types.RegisterUserPayload

	if err := utils.ParseJSON(r, &payload); err != nil {
		utils.WriteError(w, http.StatusBadRequest, err)
		return
	}

	if err := utils.Validate.Struct(payload); err != nil {
		errors := err.(validator.ValidationErrors)
		utils.WriteError(w, http.StatusBadRequest, fmt.Errorf("invalid payload: %v", errors))
		return
	}

	_, err := h.store.GetUserByEmail(ctx, payload.Email)
	if err == nil {
		utils.WriteError(w, http.StatusBadRequest, fmt.Errorf("user already exists"))
		return
	}
	if err.Error() != "user not found" {
		utils.WriteError(w, http.StatusInternalServerError, err)
		return
	}

	hashedPassword, err := auth.HashPassword(payload.Password)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err)
		return
	}

	user := &types.User{
		ID:        uuid.New(),
		Name:      payload.Name,
		Email:     payload.Email,
		Role:      types.UserRoleRunner,
		Status:    types.UserStatusPending, // runners start pending (KYC)
		Phone:     payload.Phone,
		Password:  hashedPassword,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.store.CreateUser(ctx, user); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err)
		return
	}

	// Auto-create runner profile
	if _, err := h.store.GetOrCreateRunnerProfile(ctx, user.ID); err != nil {
		fmt.Printf("warn: failed to create runner profile for %s: %v\n", user.ID, err)
	}

	if _, err := h.store.GetOrCreateWallet(ctx, user.ID); err != nil {
		fmt.Printf("warn: failed to create wallet for %s: %v\n", user.ID, err)
	}

	utils.WriteJSON(w, http.StatusCreated, map[string]string{
		"message": "runner registered successfully, pending KYC approval",
	})
}

// GET USER (Protected)
func (h *Handler) handleGetUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	str := chi.URLParam(r, "userID")

	userID, err := uuid.Parse(str)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, fmt.Errorf("invalid user ID"))
		return
	}

	user, err := h.store.GetUserByID(ctx, userID)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, err)
		return
	}

	utils.WriteJSON(w, http.StatusOK, user)
}
