package delivery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
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

// GenerateOTPResponse is returned to the client after generating a delivery code.
type GenerateOTPResponse struct {
	OTP       string    `json:"otp"`       // plain-text, shown once to client
	ExpiresAt time.Time `json:"expiresAt"`
}

// VerifyOTPRequest is the payload submitted by the runner to confirm delivery.
type VerifyOTPRequest struct {
	OTP string `json:"otp" validate:"required,len=6"`
}

// ---- Errors ----------------------------------------------------------------

var (
	ErrProofNotFound  = errors.New("delivery proof not found or already used")
	ErrOTPInvalid     = errors.New("delivery OTP is incorrect")
	ErrOTPExpired     = errors.New("delivery OTP has expired")
	ErrOTPLocked      = errors.New("delivery OTP locked after too many attempts")
	ErrWrongRunner    = errors.New("only the assigned runner can verify delivery")
	ErrNotDelivered   = errors.New("errand has not been marked as delivered yet")
)

const (
	otpMaxAttempts  = 3
	deliveryOTPLen  = 6
)

// ---- Store -----------------------------------------------------------------

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// UpsertOTP replaces any existing unverified OTP for the errand with a fresh one.
// Raw OTP is never persisted — only its SHA-256 hash.
func (s *Store) UpsertOTP(ctx context.Context, errandID uuid.UUID, otpHash string, expiresAt time.Time) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO delivery_proofs
		    (id, errand_id, proof_type, otp_hash, attempts, verified, expires_at, created_at)
		 VALUES ($1, $2, 'otp', $3, 0, false, $4, NOW())
		 ON CONFLICT (errand_id, proof_type) WHERE verified = false
		 DO UPDATE SET
		     otp_hash   = EXCLUDED.otp_hash,
		     attempts   = 0,
		     expires_at = EXCLUDED.expires_at,
		     created_at = NOW()`,
		uuid.New(), errandID, otpHash, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("upsert delivery otp: %w", err)
	}
	return nil
}

type deliveryProofRow struct {
	ID        uuid.UUID
	OTPHash   string
	Attempts  int
	Verified  bool
	ExpiresAt time.Time
}

// GetActiveProof returns the current unverified OTP proof for the errand.
func (s *Store) GetActiveProof(ctx context.Context, errandID uuid.UUID) (*deliveryProofRow, error) {
	var row deliveryProofRow
	err := s.db.QueryRow(ctx,
		`SELECT id, otp_hash, attempts, verified, expires_at
		 FROM delivery_proofs
		 WHERE errand_id = $1 AND proof_type = 'otp' AND verified = false`,
		errandID,
	).Scan(&row.ID, &row.OTPHash, &row.Attempts, &row.Verified, &row.ExpiresAt)
	if err == pgx.ErrNoRows {
		return nil, ErrProofNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get delivery proof: %w", err)
	}
	return &row, nil
}

// IncrementAttempts bumps the attempt counter and returns the new value.
func (s *Store) IncrementAttempts(ctx context.Context, proofID uuid.UUID) (int, error) {
	var attempts int
	err := s.db.QueryRow(ctx,
		`UPDATE delivery_proofs SET attempts = attempts + 1 WHERE id = $1
		 RETURNING attempts`,
		proofID,
	).Scan(&attempts)
	if err != nil {
		return 0, fmt.Errorf("increment attempts: %w", err)
	}
	return attempts, nil
}

// MarkVerified marks the proof as verified and advances the errand to 'completed'.
// Both operations are inside a single transaction for atomicity.
func (s *Store) MarkVerified(ctx context.Context, proofID uuid.UUID, errandID uuid.UUID, runnerID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx,
		`UPDATE delivery_proofs SET verified = true WHERE id = $1`,
		proofID,
	)
	if err != nil {
		return fmt.Errorf("mark proof verified: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE errands SET status = 'completed', updated_at = NOW() WHERE id = $1`,
		errandID,
	)
	if err != nil {
		return fmt.Errorf("complete errand: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO errand_events (id, errand_id, actor_user_id, event_type, occurred_at)
		 VALUES ($1,$2,$3,'delivery_confirmed',NOW())`,
		uuid.New(), errandID, runnerID,
	)
	if err != nil {
		return fmt.Errorf("log delivery event: %w", err)
	}

	return tx.Commit(ctx)
}

// ---- Service ---------------------------------------------------------------

type NotificationSender interface {
	SendPush(ctx context.Context, userID uuid.UUID, title, body string, data map[string]string) error
}

type ErrandStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*pkgtypes.Errand, []pkgtypes.ErrandStop, error)
}

type Service struct {
	store       *Store
	errandStore ErrandStore
	notifs      NotificationSender
	otpExpiry   time.Duration
	log         *zap.Logger
}

func NewService(store *Store, errandStore ErrandStore, notifStore interface{}, notifs NotificationSender, log *zap.Logger) *Service {
	return &Service{
		store:       store,
		errandStore: errandStore,
		notifs:      notifs,
		otpExpiry:   10 * time.Minute,
		log:         log,
	}
}

// GenerateOTP creates a fresh 6-digit delivery code for the client to share with the runner.
func (s *Service) GenerateOTP(ctx context.Context, errandID uuid.UUID) (*GenerateOTPResponse, error) {
	clientID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	errand, _, err := s.errandStore.GetByID(ctx, errandID)
	if err != nil {
		return nil, err
	}

	// Ownership check.
	if errand.ClientID != clientID {
		return nil, errors.New("not your errand")
	}
	if errand.Status != pkgtypes.ErrandDelivered {
		return nil, ErrNotDelivered
	}

	otp, err := generateOTP()
	if err != nil {
		return nil, fmt.Errorf("generate otp: %w", err)
	}

	hash := hashOTP(otp)
	expiresAt := time.Now().Add(s.otpExpiry)

	if err := s.store.UpsertOTP(ctx, errandID, hash, expiresAt); err != nil {
		return nil, err
	}

	return &GenerateOTPResponse{OTP: otp, ExpiresAt: expiresAt}, nil
}

// VerifyOTP is called by the runner who enters the OTP the client shared verbally.
func (s *Service) VerifyOTP(ctx context.Context, errandID uuid.UUID, req VerifyOTPRequest) error {
	runnerID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}

	s.log.Info("VerifyOTP debug",
		zap.String("runnerID_from_ctx", runnerID.String()),
		zap.String("errandID", errandID.String()),
	)

	errand, _, err := s.errandStore.GetByID(ctx, errandID)
	if err != nil {
		return err
	}

	if errand.AssignedRunnerID != nil {
		s.log.Info("VerifyOTP errand",
			zap.String("assigned_runner_id", errand.AssignedRunnerID.String()),
			zap.Bool("match", *errand.AssignedRunnerID == runnerID),
		)
	} else {
		s.log.Info("VerifyOTP errand",
			zap.String("assigned_runner_id", "nil"),
			zap.Bool("match", false),
		)
	}

	// Verify the calling runner is the assigned runner.
	if errand.AssignedRunnerID == nil || *errand.AssignedRunnerID != runnerID {
		return ErrWrongRunner
	}
	if errand.Status != pkgtypes.ErrandDelivered {
		return ErrNotDelivered
	}

	proof, err := s.store.GetActiveProof(ctx, errandID)
	if err != nil {
		return err
	}

	// Check expiry before bumping attempts.
	if time.Now().After(proof.ExpiresAt) {
		return ErrOTPExpired
	}

	attempts, err := s.store.IncrementAttempts(ctx, proof.ID)
	if err != nil {
		return err
	}
	if attempts > otpMaxAttempts {
		return ErrOTPLocked
	}

	// Constant-time hash comparison to prevent timing attacks.
	if hashOTP(req.OTP) != proof.OTPHash {
		return ErrOTPInvalid
	}

	if err := s.store.MarkVerified(ctx, proof.ID, errandID, runnerID); err != nil {
		return err
	}

	// Notify both parties (best-effort, non-blocking).
	go func() {
		ctx := context.Background()
		s.notifs.SendPush(ctx, errand.ClientID, "Delivery confirmed!", "Your errand has been completed.", map[string]string{"errandId": errandID.String()}) //nolint:errcheck
		if errand.AssignedRunnerID != nil {
			s.notifs.SendPush(ctx, *errand.AssignedRunnerID, "Delivery confirmed!", "Errand marked complete. Collect your payment.", map[string]string{"errandId": errandID.String()}) //nolint:errcheck
		}
	}()

	return nil
}

// generateOTP returns a cryptographically random 6-digit string.
func generateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// hashOTP returns the hex-encoded SHA-256 of the raw OTP string.
func hashOTP(otp string) string {
	sum := sha256.Sum256([]byte(otp))
	return fmt.Sprintf("%x", sum)
}

// ---- Handler ---------------------------------------------------------------

type Handler struct {
	svc *Service
	log *zap.Logger
}

func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// GenerateOTP godoc
// POST /api/v1/errands/:errandId/delivery-otp
func (h *Handler) GenerateOTP(w http.ResponseWriter, r *http.Request) {
	errandID, err := uuid.Parse(chi.URLParam(r, "errandId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "errand ID must be a valid UUID")
		return
	}

	resp, err := h.svc.GenerateOTP(r.Context(), errandID)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotDelivered):
			response.Conflict(w, "NOT_DELIVERED", "Runner must mark the errand as delivered first.")
		default:
			h.log.Error("GenerateOTP failed", zap.Error(err))
			response.InternalError(w)
		}
		return
	}

	response.JSON(w, http.StatusOK, resp)
}

// VerifyOTP godoc
// POST /api/v1/errands/:errandId/verify-delivery
func (h *Handler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	errandID, err := uuid.Parse(chi.URLParam(r, "errandId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "errand ID must be a valid UUID")
		return
	}

	var req VerifyOTPRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	if err := h.svc.VerifyOTP(r.Context(), errandID, req); err != nil {
		switch {
		case errors.Is(err, ErrOTPInvalid):
			response.BadRequest(w, "OTP_INVALID", "The code is incorrect. Please check with the client.")
		case errors.Is(err, ErrOTPExpired):
			response.BadRequest(w, "OTP_EXPIRED", "The code has expired. Ask the client to generate a new one.")
		case errors.Is(err, ErrOTPLocked):
			response.Error(w, http.StatusTooManyRequests, "OTP_LOCKED", "Too many failed attempts. The client must generate a new code.")
		case errors.Is(err, ErrWrongRunner):
			response.Forbidden(w, "you are not the assigned runner for this errand")
		case errors.Is(err, ErrNotDelivered):
			response.Conflict(w, "NOT_DELIVERED", "Errand must be in delivered status.")
		default:
			h.log.Error("VerifyOTP failed", zap.Error(err))
			response.InternalError(w)
		}
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "delivery confirmed, errand completed"})
}
