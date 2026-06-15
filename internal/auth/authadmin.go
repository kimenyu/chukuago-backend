package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/chukuago/api/pkg/response"
	"github.com/chukuago/api/pkg/validator"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

//  Errors 

var (
	ErrAdminNotFound    = errors.New("admin not found")
	ErrAdminInvalidPass = errors.New("invalid password")
	ErrAdminSuspended   = errors.New("admin account suspended")
)

//  DTOs 

// AdminLoginRequest is the payload for admin email/password sign-in.
type AdminLoginRequest struct {
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

// GetAdminByEmail fetches an admin user by email address.

func (s *Store) GetAdminByEmail(ctx context.Context, email string) (*AdminRow, error) {
	const q = `
		SELECT id, email, COALESCE(name,''), role, status, password_hash, created_at, updated_at
		FROM users
		WHERE email = $1 AND role = 'admin' AND status != 'deleted'
	`
	var a AdminRow
	err := s.db.QueryRow(ctx, q, email).Scan(
		&a.ID, &a.Email, &a.Name, &a.Role, &a.Status,
		&a.PasswordHash, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get admin by email: %w", err)
	}
	return &a, nil
}

// AdminRow holds the data returned from the admins query.
type AdminRow struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// CreateAdmin inserts a new admin user with a hashed password.
// Useful for seeding the first admin via a migration or CLI tool.
func (s *Store) CreateAdmin(ctx context.Context, email, name, plainPassword string) (*AdminRow, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	const q = `
		INSERT INTO users (id, phone, email, name, role, status, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'admin', 'active', $5, NOW(), NOW())
		ON CONFLICT (email) DO NOTHING
		RETURNING id, email, COALESCE(name,''), role, status, password_hash, created_at, updated_at
	`
	// phone is set to a placeholder for admins since the phone column is NOT NULL
	placeholderPhone := fmt.Sprintf("admin_%s", uuid.New().String()[:8])

	var a AdminRow
	err = s.db.QueryRow(ctx, q, uuid.New(), placeholderPhone, email, name, string(hash)).Scan(
		&a.ID, &a.Email, &a.Name, &a.Role, &a.Status,
		&a.PasswordHash, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create admin: %w", err)
	}
	return &a, nil
}

//  Service

// AdminService handles admin-specific authentication.
type AdminService struct {
	store    *Store
	tokenSvc *TokenService
	log      *zap.Logger
}

// NewAdminService creates an AdminService.
func NewAdminService(store *Store, tokenSvc *TokenService, log *zap.Logger) *AdminService {
	return &AdminService{store: store, tokenSvc: tokenSvc, log: log}
}

// Login validates admin credentials and issues a token pair.
func (s *AdminService) Login(ctx context.Context, req AdminLoginRequest) (*TokenPair, error) {
	admin, err := s.store.GetAdminByEmail(ctx, req.Email)
	if err != nil {
		// Mask "not found" to avoid email enumeration.
		s.log.Warn("admin login: user not found", zap.String("email", req.Email))
		return nil, ErrAdminInvalidPass
	}

	if admin.Status == "suspended" {
		return nil, ErrAdminSuspended
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password)); err != nil {
		s.log.Warn("admin login: wrong password", zap.String("email", req.Email))
		return nil, ErrAdminInvalidPass
	}

	access, err := s.tokenSvc.NewAccessToken(admin.ID, "admin")
	if err != nil {
		return nil, fmt.Errorf("mint access token: %w", err)
	}
	refresh, err := s.tokenSvc.NewRefreshToken(admin.ID, "admin")
	if err != nil {
		return nil, fmt.Errorf("mint refresh token: %w", err)
	}

	claims, err := s.tokenSvc.Validate(refresh)
	if err != nil {
		return nil, fmt.Errorf("validate refresh token: %w", err)
	}
	if err := s.store.CreateSession(ctx, admin.ID, refresh, claims.ExpiresAt.Time); err != nil {
		return nil, fmt.Errorf("persist session: %w", err)
	}

	s.log.Info("admin login success", zap.String("email", req.Email), zap.String("id", admin.ID.String()))
	return &TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

// Me returns the admin profile from a validated access token (used for /admin/me).
func (s *AdminService) Me(ctx context.Context, adminID uuid.UUID) (*AdminRow, error) {
	const q = `
		SELECT id, email, COALESCE(name,''), role, status, password_hash, created_at, updated_at
		FROM users WHERE id = $1 AND role = 'admin' AND status != 'deleted'
	`
	var a AdminRow
	err := s.store.db.QueryRow(ctx, q, adminID).Scan(
		&a.ID, &a.Email, &a.Name, &a.Role, &a.Status,
		&a.PasswordHash, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, ErrAdminNotFound
	}
	return &a, nil
}

//  Handler 

// AdminHandler exposes admin auth endpoints.
type AdminHandler struct {
	svc      *AdminService
	tokenSvc *TokenService
	log      *zap.Logger
}

// NewAdminHandler creates an AdminHandler.
func NewAdminHandler(svc *AdminService, tokenSvc *TokenService, log *zap.Logger) *AdminHandler {
	return &AdminHandler{svc: svc, tokenSvc: tokenSvc, log: log}
}

// Login godoc
// POST /api/v1/auth/admin/login
func (h *AdminHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req AdminLoginRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	pair, err := h.svc.Login(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, ErrAdminInvalidPass):
			response.Error(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Email or password is incorrect.")
		case errors.Is(err, ErrAdminSuspended):
			response.Error(w, http.StatusForbidden, "ACCOUNT_SUSPENDED", "Your admin account has been suspended.")
		default:
			h.log.Error("admin Login failed", zap.Error(err))
			response.InternalError(w)
		}
		return
	}

	response.JSON(w, http.StatusOK, pair)
}

// Me godoc
// GET /api/v1/auth/admin/me  (requires auth middleware + admin role)
func (h *AdminHandler) Me(w http.ResponseWriter, r *http.Request) {
	// Extract admin ID from Authorization header (already validated by middleware).
	authHeader := r.Header.Get("Authorization")
	if len(authHeader) < 8 {
		response.Unauthorized(w, "Missing token.")
		return
	}
	tokenStr := authHeader[7:] // strip "Bearer "

	claims, err := h.tokenSvc.Validate(tokenStr)
	if err != nil || claims.Role != "admin" {
		response.Unauthorized(w, "Invalid token.")
		return
	}

	admin, err := h.svc.Me(r.Context(), claims.UserID)
	if err != nil {
		response.NotFound(w, "ADMIN_NOT_FOUND")
		return
	}

	response.JSON(w, http.StatusOK, admin)
}
