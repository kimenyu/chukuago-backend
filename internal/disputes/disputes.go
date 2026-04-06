package disputes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

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

type OpenDisputeRequest struct {
	Reason string `json:"reason" validate:"required,min=20,max=2000"`
}

type ResolveDisputeRequest struct {
	Resolution      string   `json:"resolution"      validate:"required,oneof=refund_client pay_runner split no_action"`
	ResolutionNotes *string  `json:"resolutionNotes" validate:"omitempty,max=2000"`
	AmountClient    *float64 `json:"amountClient"    validate:"omitempty,gte=0"`
	AmountRunner    *float64 `json:"amountRunner"    validate:"omitempty,gte=0"`
}

type DisputeResponse struct {
	ID              string     `json:"id"`
	ErrandID        string     `json:"errandId"`
	OpenedByUserID  string     `json:"openedByUserId"`
	Reason          string     `json:"reason"`
	Status          string     `json:"status"`
	Resolution      *string    `json:"resolution,omitempty"`
	ResolutionNotes *string    `json:"resolutionNotes,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	ResolvedAt      *time.Time `json:"resolvedAt,omitempty"`
}

// ---- Errors ----------------------------------------------------------------

var (
	ErrNotFound         = errors.New("dispute not found")
	ErrAlreadyDisputed  = errors.New("a dispute is already open for this errand")
	ErrNotDisputable    = errors.New("only delivered or in-progress errands can be disputed")
	ErrNotParticipant   = errors.New("only errand participants can open a dispute")
	ErrAlreadyResolved  = errors.New("dispute is already resolved")
)

// ---- Store -----------------------------------------------------------------

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Open creates a dispute and flips the errand status to 'disputed' atomically.
func (s *Store) Open(ctx context.Context, errandID, openerID uuid.UUID, reason string) (*pkgtypes.Dispute, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	id := uuid.New()
	var d pkgtypes.Dispute
	err = tx.QueryRow(ctx,
		`INSERT INTO disputes (id, errand_id, opened_by_user_id, reason, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,'open',NOW(),NOW())
		 RETURNING id, errand_id, opened_by_user_id, reason, status, created_at, updated_at`,
		id, errandID, openerID, reason,
	).Scan(
		&d.ID, &d.ErrandID, &d.OpenedByUserID, &d.Reason,
		&d.Status, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert dispute: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE errands SET status='disputed', updated_at=NOW() WHERE id=$1`,
		errandID,
	)
	if err != nil {
		return nil, fmt.Errorf("flip errand to disputed: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO errand_events (id, errand_id, actor_user_id, event_type, occurred_at)
		 VALUES ($1,$2,$3,'dispute_opened',NOW())`,
		uuid.New(), errandID, openerID,
	)
	if err != nil {
		return nil, fmt.Errorf("log event: %w", err)
	}

	return &d, tx.Commit(ctx)
}

// GetByErrand fetches the active dispute for an errand.
func (s *Store) GetByErrand(ctx context.Context, errandID uuid.UUID) (*pkgtypes.Dispute, error) {
	var d pkgtypes.Dispute
	err := s.db.QueryRow(ctx,
		`SELECT id, errand_id, opened_by_user_id, reason, status,
		        resolution, resolution_notes, amount_client, amount_runner,
		        created_at, updated_at, resolved_at, resolved_by
		 FROM disputes WHERE errand_id=$1 ORDER BY created_at DESC LIMIT 1`,
		errandID,
	).Scan(
		&d.ID, &d.ErrandID, &d.OpenedByUserID, &d.Reason, &d.Status,
		&d.Resolution, &d.ResolutionNotes, &d.AmountClient, &d.AmountRunner,
		&d.CreatedAt, &d.UpdatedAt, &d.ResolvedAt, &d.ResolvedBy,
	)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get dispute: %w", err)
	}
	return &d, nil
}

// Resolve marks the dispute resolved by an admin.
func (s *Store) Resolve(ctx context.Context, disputeID, adminID uuid.UUID, req ResolveDisputeRequest) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	res := pkgtypes.DisputeResolution(req.Resolution)
	tag, err := tx.Exec(ctx,
		`UPDATE disputes SET
			status           = 'resolved',
			resolution       = $2,
			resolution_notes = $3,
			amount_client    = $4,
			amount_runner    = $5,
			resolved_at      = NOW(),
			resolved_by      = $6,
			updated_at       = NOW()
		 WHERE id = $1 AND status NOT IN ('resolved','rejected')`,
		disputeID, res, req.ResolutionNotes, req.AmountClient, req.AmountRunner, adminID,
	)
	if err != nil {
		return fmt.Errorf("resolve dispute: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyResolved
	}

	return tx.Commit(ctx)
}

// ListAll returns all disputes — admin only.
func (s *Store) ListAll(ctx context.Context, status *string, page, limit int) ([]pkgtypes.Dispute, int, error) {
	offset := (page - 1) * limit
	var rows interface{ Close() }
	var pgErr error
	var total int

	var pgRows interface {
		Next() bool
		Scan(...interface{}) error
		Err() error
		Close()
	}

	if status != nil {
		r, err := s.db.Query(ctx,
			`SELECT id, errand_id, opened_by_user_id, reason, status,
			        resolution, resolution_notes, amount_client, amount_runner,
			        created_at, updated_at, resolved_at, resolved_by
			 FROM disputes WHERE status=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
			*status, limit, offset,
		)
		pgRows, pgErr = r, err
		s.db.QueryRow(ctx, `SELECT COUNT(*) FROM disputes WHERE status=$1`, *status).Scan(&total) //nolint:errcheck
	} else {
		r, err := s.db.Query(ctx,
			`SELECT id, errand_id, opened_by_user_id, reason, status,
			        resolution, resolution_notes, amount_client, amount_runner,
			        created_at, updated_at, resolved_at, resolved_by
			 FROM disputes ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
			limit, offset,
		)
		pgRows, pgErr = r, err
		s.db.QueryRow(ctx, `SELECT COUNT(*) FROM disputes`).Scan(&total) //nolint:errcheck
	}

	_ = rows
	if pgErr != nil {
		return nil, 0, fmt.Errorf("list disputes: %w", pgErr)
	}
	defer pgRows.Close()

	var disputes []pkgtypes.Dispute
	for pgRows.Next() {
		var d pkgtypes.Dispute
		if err := pgRows.Scan(
			&d.ID, &d.ErrandID, &d.OpenedByUserID, &d.Reason, &d.Status,
			&d.Resolution, &d.ResolutionNotes, &d.AmountClient, &d.AmountRunner,
			&d.CreatedAt, &d.UpdatedAt, &d.ResolvedAt, &d.ResolvedBy,
		); err != nil {
			return nil, 0, fmt.Errorf("scan dispute: %w", err)
		}
		disputes = append(disputes, d)
	}
	return disputes, total, pgRows.Err()
}

// ---- Service ---------------------------------------------------------------

type ErrandStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*pkgtypes.Errand, []pkgtypes.ErrandStop, error)
}

type Service struct {
	store       *Store
	errandStore ErrandStore
	log         *zap.Logger
}

func NewService(store *Store, errandStore ErrandStore, log *zap.Logger) *Service {
	return &Service{store: store, errandStore: errandStore, log: log}
}

func (s *Service) Open(ctx context.Context, errandID uuid.UUID, req OpenDisputeRequest) (*DisputeResponse, error) {
	openerID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	errand, _, err := s.errandStore.GetByID(ctx, errandID)
	if err != nil {
		return nil, err
	}

	// Must be a participant.
	isClient := errand.ClientID == openerID
	isRunner := errand.AssignedRunnerID != nil && *errand.AssignedRunnerID == openerID
	if !isClient && !isRunner {
		return nil, ErrNotParticipant
	}

	// Only delivered or in-progress errands can be disputed.
	if errand.Status != pkgtypes.ErrandDelivered && errand.Status != pkgtypes.ErrandInProgress {
		return nil, ErrNotDisputable
	}

	dispute, err := s.store.Open(ctx, errandID, openerID, req.Reason)
	if err != nil {
		return nil, err
	}

	return toDisputeResponse(dispute), nil
}

func (s *Service) Resolve(ctx context.Context, disputeID uuid.UUID, req ResolveDisputeRequest) error {
	adminID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}
	return s.store.Resolve(ctx, disputeID, adminID, req)
}

func toDisputeResponse(d *pkgtypes.Dispute) *DisputeResponse {
	resp := &DisputeResponse{
		ID:              d.ID.String(),
		ErrandID:        d.ErrandID.String(),
		OpenedByUserID:  d.OpenedByUserID.String(),
		Reason:          d.Reason,
		Status:          string(d.Status),
		ResolutionNotes: d.ResolutionNotes,
		CreatedAt:       d.CreatedAt,
		ResolvedAt:      d.ResolvedAt,
	}
	if d.Resolution != nil {
		s := string(*d.Resolution)
		resp.Resolution = &s
	}
	return resp
}

// ---- Handler ---------------------------------------------------------------

type Handler struct {
	svc *Service
	log *zap.Logger
}

func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Open godoc
// POST /api/v1/errands/:errandId/disputes
func (h *Handler) Open(w http.ResponseWriter, r *http.Request) {
	errandID, err := uuid.Parse(chi.URLParam(r, "errandId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "errand ID must be a valid UUID")
		return
	}

	var req OpenDisputeRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	dispute, err := h.svc.Open(r.Context(), errandID, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotParticipant):
			response.Forbidden(w, "you are not a participant in this errand")
		case errors.Is(err, ErrNotDisputable):
			response.Conflict(w, "NOT_DISPUTABLE", "Only delivered or in-progress errands can be disputed.")
		case errors.Is(err, ErrAlreadyDisputed):
			response.Conflict(w, "ALREADY_DISPUTED", "A dispute is already open for this errand.")
		default:
			h.log.Error("Open dispute failed", zap.Error(err))
			response.InternalError(w)
		}
		return
	}

	response.JSON(w, http.StatusCreated, dispute)
}
