package offers

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

// PlaceBidRequest is the payload for a runner making an offer on an errand.
type PlaceBidRequest struct {
	Amount     float64 `json:"amount"      validate:"required,gt=0"`
	ETAMinutes *int    `json:"etaMinutes"  validate:"omitempty,min=1,max=480"`
	Message    *string `json:"message"     validate:"omitempty,max=500"`
}

// OfferResponse is the serialised offer returned to clients.
type OfferResponse struct {
	ID         string     `json:"id"`
	ErrandID   string     `json:"errandId"`
	RunnerID   string     `json:"runnerId"`
	Amount     float64    `json:"amount"`
	Currency   string     `json:"currency"`
	ETAMinutes *int       `json:"etaMinutes,omitempty"`
	Message    *string    `json:"message,omitempty"`
	Status     string     `json:"status"`
	RunnerName string     `json:"runnerName,omitempty"`
	RatingAvg  float64    `json:"ratingAvg"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// ---- Errors ----------------------------------------------------------------

var (
	ErrNotFound         = errors.New("offer not found")
	ErrAlreadyBid       = errors.New("runner has already placed a bid on this errand")
	ErrErrandNotBidding = errors.New("errand is not accepting bids")
	ErrErrandNotOwned   = errors.New("only the errand owner can accept an offer")
	ErrOfferNotPending  = errors.New("offer is no longer pending")
)

// ---- Store -----------------------------------------------------------------

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// PlaceBid inserts a new offer for the errand.
// A runner may only have one active (pending) offer per errand.
func (s *Store) PlaceBid(ctx context.Context, errandID, runnerID uuid.UUID, req PlaceBidRequest, currency string) (*pkgtypes.ErrandOffer, error) {
	id := uuid.New()
	var offer pkgtypes.ErrandOffer
	err := s.db.QueryRow(ctx,
		`INSERT INTO errand_offers (id, errand_id, runner_id, amount, currency, eta_minutes, message, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'pending',NOW(),NOW())
		 RETURNING id, errand_id, runner_id, amount, currency, eta_minutes, message, status, created_at, updated_at`,
		id, errandID, runnerID, req.Amount, currency, req.ETAMinutes, req.Message,
	).Scan(
		&offer.ID, &offer.ErrandID, &offer.RunnerID, &offer.Amount, &offer.Currency,
		&offer.ETAMinutes, &offer.Message, &offer.Status, &offer.CreatedAt, &offer.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert offer: %w", err)
	}
	return &offer, nil
}

// ListForErrand returns all offers on an errand, joined with runner rating info.
func (s *Store) ListForErrand(ctx context.Context, errandID uuid.UUID) ([]OfferResponse, error) {
	const query = `
		SELECT o.id, o.errand_id, o.runner_id, o.amount, o.currency,
		       o.eta_minutes, o.message, o.status,
		       COALESCE(u.name,'') AS runner_name,
		       COALESCE(rp.rating_avg,0) AS rating_avg,
		       o.created_at
		FROM errand_offers o
		JOIN users u ON u.id = o.runner_id
		LEFT JOIN runner_profiles rp ON rp.user_id = o.runner_id
		WHERE o.errand_id = $1
		ORDER BY o.created_at ASC
	`
	rows, err := s.db.Query(ctx, query, errandID)
	if err != nil {
		return nil, fmt.Errorf("list offers: %w", err)
	}
	defer rows.Close()

	var offers []OfferResponse
	for rows.Next() {
		var o OfferResponse
		if err := rows.Scan(
			&o.ID, &o.ErrandID, &o.RunnerID, &o.Amount, &o.Currency,
			&o.ETAMinutes, &o.Message, &o.Status,
			&o.RunnerName, &o.RatingAvg, &o.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan offer: %w", err)
		}
		offers = append(offers, o)
	}
	return offers, rows.Err()
}

// Accept accepts one offer and rejects all others atomically.
// Uses SELECT FOR UPDATE on the errand row to prevent double-assignment.
func (s *Store) Accept(ctx context.Context, db *pgxpool.Pool, errandID, offerID, clientID uuid.UUID) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Lock the errand row to prevent concurrent accepts.
	var status pkgtypes.ErrandStatus
	var ownerID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT status, client_id FROM errands WHERE id = $1 FOR UPDATE`,
		errandID,
	).Scan(&status, &ownerID)
	if err == pgx.ErrNoRows {
		return errors.New("errand not found")
	}
	if err != nil {
		return fmt.Errorf("lock errand: %w", err)
	}

	if ownerID != clientID {
		return ErrErrandNotOwned
	}
	if status == pkgtypes.ErrandAssigned || status == pkgtypes.ErrandInProgress {
		return ErrAlreadyBid // already assigned
	}

	// Fetch the winning offer and verify it's still pending.
	var runnerID uuid.UUID
	var offerStatus pkgtypes.OfferStatus
	err = tx.QueryRow(ctx,
		`SELECT runner_id, status FROM errand_offers WHERE id = $1 AND errand_id = $2`,
		offerID, errandID,
	).Scan(&runnerID, &offerStatus)
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	if offerStatus != pkgtypes.OfferPending {
		return ErrOfferNotPending
	}

	// Mark winner as accepted, reject all others.
	_, err = tx.Exec(ctx,
		`UPDATE errand_offers SET status='accepted', updated_at=NOW() WHERE id=$1`,
		offerID,
	)
	if err != nil {
		return fmt.Errorf("accept offer: %w", err)
	}
	_, err = tx.Exec(ctx,
		`UPDATE errand_offers SET status='rejected', updated_at=NOW()
		 WHERE errand_id=$1 AND id != $2 AND status='pending'`,
		errandID, offerID,
	)
	if err != nil {
		return fmt.Errorf("reject other offers: %w", err)
	}

	// Assign the runner and advance errand status.
	_, err = tx.Exec(ctx,
		`UPDATE errands SET status='assigned', assigned_runner_id=$2, accepted_offer_id=$3, updated_at=NOW()
		 WHERE id=$1`,
		errandID, runnerID, offerID,
	)
	if err != nil {
		return fmt.Errorf("assign runner: %w", err)
	}

	// Immutable event log.
	_, err = tx.Exec(ctx,
		`INSERT INTO errand_events (id, errand_id, actor_user_id, event_type, occurred_at)
		 VALUES ($1,$2,$3,'offer_accepted',NOW())`,
		uuid.New(), errandID, clientID,
	)
	if err != nil {
		return fmt.Errorf("log event: %w", err)
	}

	return tx.Commit(ctx)
}

// HasActiveBid returns true if the runner already has a pending offer on this errand.
func (s *Store) HasActiveBid(ctx context.Context, errandID, runnerID uuid.UUID) (bool, error) {
	var count int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM errand_offers WHERE errand_id=$1 AND runner_id=$2 AND status='pending'`,
		errandID, runnerID,
	).Scan(&count)
	return count > 0, err
}

// ---- Service ---------------------------------------------------------------

type NotificationSender interface {
	SendPush(ctx context.Context, userID uuid.UUID, title, body string, data map[string]string) error
}

type ErrandStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*pkgtypes.Errand, []pkgtypes.ErrandStop, error)
}

type RunnerStore interface {
	GetProfile(ctx context.Context, userID uuid.UUID) (*pkgtypes.RunnerProfile, error)
}

type Service struct {
	store       *Store
	errandStore ErrandStore
	runnerStore RunnerStore
	notifs      NotificationSender
	db          *pgxpool.Pool
	log         *zap.Logger
}

func NewService(store *Store, errandStore ErrandStore, runnerStore RunnerStore, notifStore interface{}, notifs NotificationSender, db *pgxpool.Pool, log *zap.Logger) *Service {
	return &Service{
		store:       store,
		errandStore: errandStore,
		runnerStore: runnerStore,
		notifs:      notifs,
		db:          db,
		log:         log,
	}
}

// PlaceBid is called by a runner to submit an offer.
func (s *Service) PlaceBid(ctx context.Context, errandID uuid.UUID, req PlaceBidRequest) (*OfferResponse, error) {
	runnerID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	errand, _, err := s.errandStore.GetByID(ctx, errandID)
	if err != nil {
		return nil, err
	}

	// Only posted/bidding errands accept new bids.
	if errand.Status != pkgtypes.ErrandPosted && errand.Status != pkgtypes.ErrandBidding {
		return nil, ErrErrandNotBidding
	}
	if !errand.AllowBids {
		return nil, ErrErrandNotBidding
	}

	// Prevent duplicate bids.
	exists, err := s.store.HasActiveBid(ctx, errandID, runnerID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrAlreadyBid
	}

	offer, err := s.store.PlaceBid(ctx, errandID, runnerID, req, errand.Currency)
	if err != nil {
		return nil, err
	}

	// Notify client of the new bid (best-effort).
	go s.notifs.SendPush(context.Background(), errand.ClientID, //nolint:errcheck
		"New bid received",
		"A runner placed a bid on your errand.",
		map[string]string{"errandId": errandID.String()},
	)

	return &OfferResponse{
		ID: offer.ID.String(), ErrandID: offer.ErrandID.String(),
		RunnerID: offer.RunnerID.String(), Amount: offer.Amount,
		Currency: offer.Currency, ETAMinutes: offer.ETAMinutes,
		Message: offer.Message, Status: string(offer.Status),
		CreatedAt: offer.CreatedAt,
	}, nil
}

// ListForErrand returns offers on a given errand for the owning client.
func (s *Service) ListForErrand(ctx context.Context, errandID uuid.UUID) ([]OfferResponse, error) {
	return s.store.ListForErrand(ctx, errandID)
}

// Accept accepts an offer as the client.
func (s *Service) Accept(ctx context.Context, errandID, offerID uuid.UUID) error {
	clientID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}
	return s.store.Accept(ctx, s.db, errandID, offerID, clientID)
}

// ---- Handler ---------------------------------------------------------------

type Handler struct {
	svc *Service
	log *zap.Logger
}

func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// PlaceBid godoc
// POST /api/v1/runner/errands/:errandId/offers
func (h *Handler) PlaceBid(w http.ResponseWriter, r *http.Request) {
	errandID, err := uuid.Parse(chi.URLParam(r, "errandId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "errand ID must be a valid UUID")
		return
	}

	var req PlaceBidRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	offer, err := h.svc.PlaceBid(r.Context(), errandID, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrErrandNotBidding):
			response.Conflict(w, "ERRAND_NOT_BIDDING", "This errand is not accepting bids.")
		case errors.Is(err, ErrAlreadyBid):
			response.Conflict(w, "ALREADY_BID", "You already have an active bid on this errand.")
		default:
			h.log.Error("PlaceBid failed", zap.Error(err))
			response.InternalError(w)
		}
		return
	}

	response.JSON(w, http.StatusCreated, offer)
}

// ListForErrand godoc
// GET /api/v1/errands/:errandId/offers
func (h *Handler) ListForErrand(w http.ResponseWriter, r *http.Request) {
	errandID, err := uuid.Parse(chi.URLParam(r, "errandId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "errand ID must be a valid UUID")
		return
	}

	offers, err := h.svc.ListForErrand(r.Context(), errandID)
	if err != nil {
		h.log.Error("ListForErrand failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.JSON(w, http.StatusOK, offers)
}

// Accept godoc
// POST /api/v1/errands/:errandId/offers/:offerId/accept
func (h *Handler) Accept(w http.ResponseWriter, r *http.Request) {
	errandID, err := uuid.Parse(chi.URLParam(r, "errandId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "errand ID must be a valid UUID")
		return
	}

	offerID, err := uuid.Parse(chi.URLParam(r, "offerId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "offer ID must be a valid UUID")
		return
	}

	if err := h.svc.Accept(r.Context(), errandID, offerID); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			response.NotFound(w, "OFFER_NOT_FOUND")
		case errors.Is(err, ErrErrandNotOwned):
			response.Forbidden(w, "you do not own this errand")
		case errors.Is(err, ErrOfferNotPending):
			response.Conflict(w, "OFFER_NOT_PENDING", "This offer is no longer available.")
		case errors.Is(err, ErrAlreadyBid):
			response.Conflict(w, "ALREADY_ASSIGNED", "This errand is already assigned to a runner.")
		default:
			h.log.Error("Accept offer failed", zap.Error(err))
			response.InternalError(w)
		}
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "offer accepted, runner assigned"})
}
