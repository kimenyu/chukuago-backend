package reviews

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

type CreateReviewRequest struct {
	Rating  int     `json:"rating"  validate:"required,min=1,max=5"`
	Comment *string `json:"comment" validate:"omitempty,max=1000"`
}

type ReviewResponse struct {
	ID           string     `json:"id"`
	ErrandID     string     `json:"errandId"`
	ReviewerID   string     `json:"reviewerId"`
	ReviewerName string     `json:"reviewerName,omitempty"`
	Rating       int        `json:"rating"`
	Comment      *string    `json:"comment,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// ---- Errors ----------------------------------------------------------------

var (
	ErrNotFound        = errors.New("review not found")
	ErrAlreadyReviewed = errors.New("you have already reviewed this errand")
	ErrNotCompleted    = errors.New("you can only review completed errands")
	ErrNotParticipant  = errors.New("you are not a participant in this errand")
)

// ---- Store -----------------------------------------------------------------

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Create inserts a review and recalculates the runner's average rating atomically.
func (s *Store) Create(ctx context.Context, errandID, reviewerID, revieweeID uuid.UUID, req CreateReviewRequest) (*pkgtypes.Review, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var review pkgtypes.Review
	err = tx.QueryRow(ctx,
		`INSERT INTO reviews (id, errand_id, reviewer_id, reviewee_id, rating, comment, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,NOW())
		 RETURNING id, errand_id, reviewer_id, reviewee_id, rating, comment, created_at`,
		uuid.New(), errandID, reviewerID, revieweeID, req.Rating, req.Comment,
	).Scan(
		&review.ID, &review.ErrandID, &review.ReviewerID,
		&review.RevieweeID, &review.Rating, &review.Comment, &review.CreatedAt,
	)
	if err != nil {
		// Unique constraint means already reviewed.
		return nil, fmt.Errorf("insert review: %w", err)
	}

	// Recalculate the runner's average rating in the same transaction.
	_, err = tx.Exec(ctx,
		`UPDATE runner_profiles SET
			rating_avg   = (SELECT AVG(rating) FROM reviews WHERE reviewee_id = $1),
			rating_count = (SELECT COUNT(*)    FROM reviews WHERE reviewee_id = $1),
			updated_at   = NOW()
		 WHERE user_id = $1`,
		revieweeID,
	)
	if err != nil {
		return nil, fmt.Errorf("update runner rating: %w", err)
	}

	return &review, tx.Commit(ctx)
}

// HasReviewed returns true if the reviewer already reviewed this errand.
func (s *Store) HasReviewed(ctx context.Context, errandID, reviewerID uuid.UUID) (bool, error) {
	var count int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM reviews WHERE errand_id=$1 AND reviewer_id=$2`,
		errandID, reviewerID,
	).Scan(&count)
	return count > 0, err
}

// List returns all reviews for an errand.
func (s *Store) List(ctx context.Context, errandID uuid.UUID) ([]ReviewResponse, error) {
	const query = `
		SELECT r.id, r.errand_id, r.reviewer_id, COALESCE(u.name,''),
		       r.rating, r.comment, r.created_at
		FROM reviews r
		JOIN users u ON u.id = r.reviewer_id
		WHERE r.errand_id = $1
		ORDER BY r.created_at ASC
	`
	rows, err := s.db.Query(ctx, query, errandID)
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}
	defer rows.Close()

	var reviews []ReviewResponse
	for rows.Next() {
		var r ReviewResponse
		if err := rows.Scan(&r.ID, &r.ErrandID, &r.ReviewerID, &r.ReviewerName,
			&r.Rating, &r.Comment, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan review: %w", err)
		}
		reviews = append(reviews, r)
	}
	return reviews, rows.Err()
}

// ---- Service ---------------------------------------------------------------

type ErrandStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*pkgtypes.Errand, []pkgtypes.ErrandStop, error)
}

type RunnerStore interface {
	GetProfile(ctx context.Context, userID uuid.UUID) (*pkgtypes.RunnerProfile, error)
}

type Service struct {
	store       *Store
	errandStore ErrandStore
	log         *zap.Logger
}

func NewService(store *Store, errandStore ErrandStore, runnerStore RunnerStore, log *zap.Logger) *Service {
	return &Service{store: store, errandStore: errandStore, log: log}
}

func (s *Service) Create(ctx context.Context, errandID uuid.UUID, req CreateReviewRequest) (*ReviewResponse, error) {
	reviewerID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	errand, _, err := s.errandStore.GetByID(ctx, errandID)
	if err != nil {
		return nil, err
	}

	// Only allow reviews on completed errands.
	if errand.Status != pkgtypes.ErrandCompleted {
		return nil, ErrNotCompleted
	}

	// Reviewer must be either the client or the assigned runner.
	var revieweeID uuid.UUID
	switch reviewerID {
	case errand.ClientID:
		if errand.AssignedRunnerID == nil {
			return nil, ErrNotParticipant
		}
		revieweeID = *errand.AssignedRunnerID
	default:
		if errand.AssignedRunnerID == nil || *errand.AssignedRunnerID != reviewerID {
			return nil, ErrNotParticipant
		}
		revieweeID = errand.ClientID
	}

	exists, err := s.store.HasReviewed(ctx, errandID, reviewerID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrAlreadyReviewed
	}

	review, err := s.store.Create(ctx, errandID, reviewerID, revieweeID, req)
	if err != nil {
		return nil, err
	}

	return &ReviewResponse{
		ID:         review.ID.String(),
		ErrandID:   review.ErrandID.String(),
		ReviewerID: review.ReviewerID.String(),
		Rating:     review.Rating,
		Comment:    review.Comment,
		CreatedAt:  review.CreatedAt,
	}, nil
}

func (s *Service) List(ctx context.Context, errandID uuid.UUID) ([]ReviewResponse, error) {
	return s.store.List(ctx, errandID)
}

// ---- Handler ---------------------------------------------------------------

type Handler struct {
	svc *Service
	log *zap.Logger
}

func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Create godoc
// POST /api/v1/errands/:errandId/reviews
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	errandID, err := uuid.Parse(chi.URLParam(r, "errandId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "errand ID must be a valid UUID")
		return
	}

	var req CreateReviewRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	review, err := h.svc.Create(r.Context(), errandID, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotCompleted):
			response.Conflict(w, "ERRAND_NOT_COMPLETED", "You can only review completed errands.")
		case errors.Is(err, ErrAlreadyReviewed):
			response.Conflict(w, "ALREADY_REVIEWED", "You have already reviewed this errand.")
		case errors.Is(err, ErrNotParticipant):
			response.Forbidden(w, "you are not a participant in this errand")
		default:
			h.log.Error("Create review failed", zap.Error(err))
			response.InternalError(w)
		}
		return
	}

	response.JSON(w, http.StatusCreated, review)
}

// List godoc
// GET /api/v1/errands/:errandId/reviews
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	errandID, err := uuid.Parse(chi.URLParam(r, "errandId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "errand ID must be a valid UUID")
		return
	}

	reviews, err := h.svc.List(r.Context(), errandID)
	if err != nil {
		h.log.Error("List reviews failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.JSON(w, http.StatusOK, reviews)
}

// Ensure pgx is used (scan helper).
var _ = pgx.ErrNoRows
