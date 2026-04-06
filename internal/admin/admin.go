package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/chukuago/api/internal/disputes"
	"github.com/chukuago/api/pkg/response"
	pkgtypes "github.com/chukuago/api/pkg/types"
	"github.com/chukuago/api/pkg/validator"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// ---- DTOs ------------------------------------------------------------------

type UpdateUserStatusRequest struct {
	Status string `json:"status" validate:"required,oneof=active suspended deleted"`
	Reason string `json:"reason" validate:"omitempty,max=500"`
}

type ReviewKYCRequest struct {
	Decision string  `json:"decision" validate:"required,oneof=approved rejected"`
	Notes    *string `json:"notes"    validate:"omitempty,max=1000"`
}

type UserSummary struct {
	ID        string    `json:"id"`
	Phone     string    `json:"phone"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type KYCQueueItem struct {
	DocumentID   string    `json:"documentId"`
	UserID       string    `json:"userId"`
	UserName     string    `json:"userName"`
	UserPhone    string    `json:"userPhone"`
	DocType      string    `json:"docType"`
	DocURL       string    `json:"docUrl"`
	Status       string    `json:"status"`
	SubmittedAt  time.Time `json:"submittedAt"`
}

// ---- Errors ----------------------------------------------------------------

var (
	ErrNotFound    = errors.New("resource not found")
	ErrBadDecision = errors.New("invalid KYC decision")
)

// ---- Store -----------------------------------------------------------------

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// ListUsers returns all users with optional status filter, paginated.
func (s *Store) ListUsers(ctx context.Context, statusFilter *string, page, limit int) ([]UserSummary, int, error) {
	offset := (page - 1) * limit

	var pgRows interface {
		Next() bool
		Scan(...interface{}) error
		Err() error
		Close()
	}
	var total int

	if statusFilter != nil {
		r, err := s.db.Query(ctx,
			`SELECT id, phone, COALESCE(name,''), role, status, created_at
			 FROM users WHERE status=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
			*statusFilter, limit, offset,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("list users: %w", err)
		}
		pgRows = r
		s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE status=$1`, *statusFilter).Scan(&total) //nolint:errcheck
	} else {
		r, err := s.db.Query(ctx,
			`SELECT id, phone, COALESCE(name,''), role, status, created_at
			 FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
			limit, offset,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("list users: %w", err)
		}
		pgRows = r
		s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&total) //nolint:errcheck
	}
	defer pgRows.Close()

	var users []UserSummary
	for pgRows.Next() {
		var u UserSummary
		if err := pgRows.Scan(&u.ID, &u.Phone, &u.Name, &u.Role, &u.Status, &u.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, total, pgRows.Err()
}

// UpdateUserStatus suspends, activates, or soft-deletes a user.
// Also logs the action to audit_logs.
func (s *Store) UpdateUserStatus(ctx context.Context, userID, adminID uuid.UUID, status, reason string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx,
		`UPDATE users SET status=$2, updated_at=NOW() WHERE id=$1 AND status != 'deleted'`,
		userID, status,
	)
	if err != nil {
		return fmt.Errorf("update user status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO audit_logs (id, actor_id, action, entity_type, entity_id, metadata, created_at)
		 VALUES ($1,$2,'update_user_status','user',$3,$4::jsonb,NOW())`,
		uuid.New(), adminID, userID,
		fmt.Sprintf(`{"status":"%s","reason":"%s"}`, status, reason),
	)
	if err != nil {
		return fmt.Errorf("audit log: %w", err)
	}

	return tx.Commit(ctx)
}

// ListPendingKYC returns all KYC submissions awaiting review.
func (s *Store) ListPendingKYC(ctx context.Context, page, limit int) ([]KYCQueueItem, int, error) {
	offset := (page - 1) * limit
	rows, err := s.db.Query(ctx,
		`SELECT kd.id, kd.user_id, COALESCE(u.name,''), u.phone,
		        kd.doc_type, kd.doc_url, kd.status, kd.created_at
		 FROM runner_kyc_documents kd
		 JOIN users u ON u.id = kd.user_id
		 WHERE kd.status = 'pending'
		 ORDER BY kd.created_at ASC
		 LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list kyc: %w", err)
	}
	defer rows.Close()

	var items []KYCQueueItem
	for rows.Next() {
		var item KYCQueueItem
		if err := rows.Scan(
			&item.DocumentID, &item.UserID, &item.UserName, &item.UserPhone,
			&item.DocType, &item.DocURL, &item.Status, &item.SubmittedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan kyc item: %w", err)
		}
		items = append(items, item)
	}

	var total int
	s.db.QueryRow(ctx, `SELECT COUNT(*) FROM runner_kyc_documents WHERE status='pending'`).Scan(&total) //nolint:errcheck

	return items, total, rows.Err()
}

// ReviewKYC approves or rejects a KYC submission and updates the runner's profile status.
func (s *Store) ReviewKYC(ctx context.Context, docID, adminID uuid.UUID, decision string, notes *string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Fetch the doc to find the owning runner.
	var userID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT user_id FROM runner_kyc_documents WHERE id=$1 AND status='pending' FOR UPDATE`,
		docID,
	).Scan(&userID)
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock kyc doc: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE runner_kyc_documents
		 SET status=$2, notes=$3, reviewed_at=NOW(), reviewed_by=$4
		 WHERE id=$1`,
		docID, decision, notes, adminID,
	)
	if err != nil {
		return fmt.Errorf("update kyc doc: %w", err)
	}

	// Flip the runner's overall KYC status.
	_, err = tx.Exec(ctx,
		`UPDATE runner_profiles SET kyc_status=$2, updated_at=NOW() WHERE user_id=$1`,
		userID, decision,
	)
	if err != nil {
		return fmt.Errorf("update runner kyc_status: %w", err)
	}

	// Audit trail.
	_, err = tx.Exec(ctx,
		`INSERT INTO audit_logs (id, actor_id, action, entity_type, entity_id, metadata, created_at)
		 VALUES ($1,$2,'review_kyc','runner_kyc_documents',$3,$4::jsonb,NOW())`,
		uuid.New(), adminID, docID,
		fmt.Sprintf(`{"decision":"%s"}`, decision),
	)
	if err != nil {
		return fmt.Errorf("audit log: %w", err)
	}

	return tx.Commit(ctx)
}

// ListErrands returns all errands with optional status filter — for admin oversight.
func (s *Store) ListErrands(ctx context.Context, statusFilter *string, page, limit int) ([]pkgtypes.Errand, int, error) {
	offset := (page - 1) * limit

	var pgRows interface {
		Next() bool
		Scan(...interface{}) error
		Err() error
		Close()
	}
	var total int

	const cols = `id, client_id, title, description, category, currency,
	              allow_bids, instant_accept, budget_min, budget_max, fixed_price,
	              scheduled_at, expires_at, status, assigned_runner_id, accepted_offer_id,
	              region_id, created_at, updated_at`

	if statusFilter != nil {
		r, err := s.db.Query(ctx,
			`SELECT `+cols+` FROM errands WHERE status=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
			*statusFilter, limit, offset,
		)
		if err != nil {
			return nil, 0, err
		}
		pgRows = r
		s.db.QueryRow(ctx, `SELECT COUNT(*) FROM errands WHERE status=$1`, *statusFilter).Scan(&total) //nolint:errcheck
	} else {
		r, err := s.db.Query(ctx,
			`SELECT `+cols+` FROM errands ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
			limit, offset,
		)
		if err != nil {
			return nil, 0, err
		}
		pgRows = r
		s.db.QueryRow(ctx, `SELECT COUNT(*) FROM errands`).Scan(&total) //nolint:errcheck
	}
	defer pgRows.Close()

	var errands []pkgtypes.Errand
	for pgRows.Next() {
		var e pkgtypes.Errand
		if err := pgRows.Scan(
			&e.ID, &e.ClientID, &e.Title, &e.Description, &e.Category, &e.Currency,
			&e.AllowBids, &e.InstantAccept, &e.BudgetMin, &e.BudgetMax, &e.FixedPrice,
			&e.ScheduledAt, &e.ExpiresAt, &e.Status, &e.AssignedRunnerID, &e.AcceptedOfferID,
			&e.RegionID, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		errands = append(errands, e)
	}
	return errands, total, pgRows.Err()
}

// ---- Service ---------------------------------------------------------------

type RunnerStore interface {
	GetProfile(ctx context.Context, userID uuid.UUID) (*pkgtypes.RunnerProfile, error)
}

type Service struct {
	store       *Store
	runnerStore RunnerStore
	log         *zap.Logger
}

func NewService(store *Store, runnerStore RunnerStore, log *zap.Logger) *Service {
	return &Service{store: store, runnerStore: runnerStore, log: log}
}

func (s *Service) ListUsers(ctx context.Context, statusFilter *string, page, limit int) ([]UserSummary, int, error) {
	return s.store.ListUsers(ctx, statusFilter, page, limit)
}

func (s *Service) UpdateUserStatus(ctx context.Context, userID uuid.UUID, req UpdateUserStatusRequest) error {
	adminID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}
	return s.store.UpdateUserStatus(ctx, userID, adminID, req.Status, req.Reason)
}

func (s *Service) ListPendingKYC(ctx context.Context, page, limit int) ([]KYCQueueItem, int, error) {
	return s.store.ListPendingKYC(ctx, page, limit)
}

func (s *Service) ReviewKYC(ctx context.Context, docID uuid.UUID, req ReviewKYCRequest) error {
	adminID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}
	return s.store.ReviewKYC(ctx, docID, adminID, req.Decision, req.Notes)
}

func (s *Service) ListErrands(ctx context.Context, statusFilter *string, page, limit int) ([]pkgtypes.Errand, int, error) {
	return s.store.ListErrands(ctx, statusFilter, page, limit)
}

func (s *Service) ListDisputes(ctx context.Context, statusFilter *string, page, limit int) ([]pkgtypes.Dispute, int, error) {
	store := disputes.NewStore(s.store.db)
	return store.ListAll(ctx, statusFilter, page, limit)
}

func (s *Service) ResolveDispute(ctx context.Context, disputeID uuid.UUID, req disputes.ResolveDisputeRequest) error {
	store := disputes.NewStore(s.store.db)
	adminID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}
	return store.Resolve(ctx, disputeID, adminID, req)
}

// ---- Handler ---------------------------------------------------------------

type Handler struct {
	svc *Service
	log *zap.Logger
}

func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	var sf *string
	if s := q.Get("status"); s != "" {
		sf = &s
	}

	users, total, err := h.svc.ListUsers(r.Context(), sf, page, limit)
	if err != nil {
		h.log.Error("admin ListUsers failed", zap.Error(err))
		response.InternalError(w)
		return
	}
	response.JSONList(w, http.StatusOK, users, page, limit, total)
}

func (h *Handler) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "user ID must be a valid UUID")
		return
	}

	var req UpdateUserStatusRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	if err := h.svc.UpdateUserStatus(r.Context(), userID, req); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.NotFound(w, "USER_NOT_FOUND")
			return
		}
		h.log.Error("UpdateUserStatus failed", zap.Error(err))
		response.InternalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListPendingKYC(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	items, total, err := h.svc.ListPendingKYC(r.Context(), page, limit)
	if err != nil {
		h.log.Error("ListPendingKYC failed", zap.Error(err))
		response.InternalError(w)
		return
	}
	response.JSONList(w, http.StatusOK, items, page, limit, total)
}

func (h *Handler) ReviewKYC(w http.ResponseWriter, r *http.Request) {
	docID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "document ID must be a valid UUID")
		return
	}

	var req ReviewKYCRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	if err := h.svc.ReviewKYC(r.Context(), docID, req); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.NotFound(w, "KYC_DOC_NOT_FOUND")
			return
		}
		h.log.Error("ReviewKYC failed", zap.Error(err))
		response.InternalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListErrands(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	var sf *string
	if s := q.Get("status"); s != "" {
		sf = &s
	}

	errands, total, err := h.svc.ListErrands(r.Context(), sf, page, limit)
	if err != nil {
		h.log.Error("admin ListErrands failed", zap.Error(err))
		response.InternalError(w)
		return
	}
	response.JSONList(w, http.StatusOK, errands, page, limit, total)
}

func (h *Handler) ListDisputes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	var sf *string
	if s := q.Get("status"); s != "" {
		sf = &s
	}

	ds, total, err := h.svc.ListDisputes(r.Context(), sf, page, limit)
	if err != nil {
		h.log.Error("admin ListDisputes failed", zap.Error(err))
		response.InternalError(w)
		return
	}
	response.JSONList(w, http.StatusOK, ds, page, limit, total)
}

func (h *Handler) ResolveDispute(w http.ResponseWriter, r *http.Request) {
	disputeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "dispute ID must be a valid UUID")
		return
	}

	var req disputes.ResolveDisputeRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	if err := h.svc.ResolveDispute(r.Context(), disputeID, req); err != nil {
		if errors.Is(err, disputes.ErrAlreadyResolved) {
			response.Conflict(w, "ALREADY_RESOLVED", "This dispute has already been resolved.")
			return
		}
		h.log.Error("ResolveDispute failed", zap.Error(err))
		response.InternalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
