package location

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/chukuago/api/pkg/response"
	pkgtypes "github.com/chukuago/api/pkg/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// DTOs

type UpdateLocationRequest struct {
	Lat float64 `json:"lat" validate:"required,min=-90,max=90"`
	Lng float64 `json:"lng" validate:"required,min=-180,max=180"`
}

type NearbyRunner struct {
	RunnerID    string    `json:"runnerId"`
	Name        string    `json:"name"`
	RatingAvg   float64   `json:"ratingAvg"`
	RatingCount int       `json:"ratingCount"`
	VehicleType *string   `json:"vehicleType,omitempty"`
	IsAvailable bool      `json:"isAvailable"`
	Lat         float64   `json:"lat"`
	Lng         float64   `json:"lng"`
	DistanceKm  float64   `json:"distanceKm"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
}

// Errors

var (
	ErrNotRunner      = errors.New("only runners can update location")
	ErrErrandNotFound = errors.New("errand not found")
	ErrNoPickupStop   = errors.New("errand has no pickup stop with coordinates")
	ErrStaleLocation  = errors.New("location timestamp is too old")
)

const (
	defaultRadiusKm = 5.0
	maxRadiusKm     = 20.0
	staleThreshold  = 15 * time.Minute // runners not seen in 15min are excluded
)

// Store

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// UpsertLocation saves or updates a runner's current position.
func (s *Store) UpsertLocation(ctx context.Context, runnerID uuid.UUID, lat, lng float64) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO runner_locations (user_id, lat, lng, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (user_id)
		 DO UPDATE SET lat = EXCLUDED.lat,
		               lng = EXCLUDED.lng,
		               updated_at = NOW()`,
		runnerID, lat, lng,
	)
	if err != nil {
		return fmt.Errorf("upsert runner location: %w", err)
	}
	return nil
}

// NearbyRunners returns KYC-approved, available runners within radiusKm of
// the given coordinates, excluding runners whose location is stale.
func (s *Store) NearbyRunners(ctx context.Context, lat, lng, radiusKm float64) ([]NearbyRunner, error) {
	const query = `
		SELECT
			u.id,
			u.name,
			COALESCE(rp.rating_avg, 0)   AS rating_avg,
			COALESCE(rp.rating_count, 0) AS rating_count,
			rp.vehicle_type,
			COALESCE(rp.is_available, false) AS is_available,
			rl.lat,
			rl.lng,
			ST_Distance(
				rl.geog,
				ST_MakePoint($2, $1)::geography
			) / 1000.0 AS distance_km,
			rl.updated_at
		FROM runner_locations rl
		JOIN users u ON u.id = rl.user_id
		JOIN runner_profiles rp ON rp.user_id = rl.user_id
		WHERE
			rp.kyc_status = 'approved'
			AND rl.updated_at > NOW() - INTERVAL '15 minutes'
			AND ST_DWithin(
				rl.geog,
				ST_MakePoint($2, $1)::geography,
				$3 * 1000  -- convert km to metres
			)
		ORDER BY distance_km ASC
		LIMIT 50
	`

	rows, err := s.db.Query(ctx, query, lat, lng, radiusKm)
	if err != nil {
		return nil, fmt.Errorf("nearby runners query: %w", err)
	}
	defer rows.Close()

	var runners []NearbyRunner
	for rows.Next() {
		var r NearbyRunner
		var id uuid.UUID
		if err := rows.Scan(
			&id, &r.Name, &r.RatingAvg, &r.RatingCount,
			&r.VehicleType, &r.IsAvailable,
			&r.Lat, &r.Lng, &r.DistanceKm, &r.LastSeenAt,
		); err != nil {
			return nil, fmt.Errorf("scan nearby runner: %w", err)
		}
		r.RunnerID = id.String()
		r.DistanceKm = math.Round(r.DistanceKm*100) / 100 // 2 decimal places
		runners = append(runners, r)
	}
	return runners, rows.Err()
}

// PickupCoords returns the lat/lng of the first pickup stop for an errand.
func (s *Store) PickupCoords(ctx context.Context, errandID uuid.UUID) (lat, lng float64, err error) {
	err = s.db.QueryRow(ctx,
		`SELECT lat, lng FROM errand_stops
		 WHERE errand_id = $1 AND stop_type = 'pickup' AND lat IS NOT NULL AND lng IS NOT NULL
		 ORDER BY seq ASC LIMIT 1`,
		errandID,
	).Scan(&lat, &lng)
	if err != nil {
		return 0, 0, ErrNoPickupStop
	}
	return lat, lng, nil
}

// Service

type NotificationSender interface {
	SendPush(ctx context.Context, userID uuid.UUID, title, body string, data map[string]string) error
}

type Service struct {
	store *Store
	notif NotificationSender
	log   *zap.Logger
}

func NewService(store *Store, notif NotificationSender, log *zap.Logger) *Service {
	return &Service{store: store, notif: notif, log: log}
}

// UpdateLocation is called by the runner app on a timer.
func (s *Service) UpdateLocation(ctx context.Context, req UpdateLocationRequest) error {
	userID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}
	role, err := pkgtypes.UserRoleFromContext(ctx)
	if err != nil || role != string(pkgtypes.UserRoleRunner) {
		return ErrNotRunner
	}
	return s.store.UpsertLocation(ctx, userID, req.Lat, req.Lng)
}

// NearbyRunners returns runners near an errand's pickup stop.
func (s *Service) NearbyRunners(ctx context.Context, errandID uuid.UUID, radiusKm float64) ([]NearbyRunner, error) {
	if radiusKm <= 0 || radiusKm > maxRadiusKm {
		radiusKm = defaultRadiusKm
	}

	lat, lng, err := s.store.PickupCoords(ctx, errandID)
	if err != nil {
		return nil, err
	}

	return s.store.NearbyRunners(ctx, lat, lng, radiusKm)
}

// NotifyNearbyRunners sends a push to all runners near an errand's pickup.
// Called by errands.Service after a new errand is posted — best effort.
func (s *Service) NotifyNearbyRunners(ctx context.Context, errandID uuid.UUID, errandTitle string) {
	lat, lng, err := s.store.PickupCoords(ctx, errandID)
	if err != nil {
		s.log.Warn("NotifyNearbyRunners: no pickup coords", zap.String("errandId", errandID.String()))
		return
	}

	runners, err := s.store.NearbyRunners(ctx, lat, lng, defaultRadiusKm)
	if err != nil {
		s.log.Warn("NotifyNearbyRunners: query failed", zap.Error(err))
		return
	}

	for _, r := range runners {
		runnerID, _ := uuid.Parse(r.RunnerID)
		go func(id uuid.UUID) {
			if err := s.notif.SendPush(
				context.Background(), id,
				"New errand nearby!",
				errandTitle+" is available near you.",
				map[string]string{"errandId": errandID.String()},
			); err != nil {
				s.log.Warn("FCM push to runner failed",
					zap.String("runnerId", id.String()), zap.Error(err))
			}
		}(runnerID)
	}

	s.log.Info("NotifyNearbyRunners sent",
		zap.Int("count", len(runners)),
		zap.String("errandId", errandID.String()),
	)
}

// Handler

type Handler struct {
	svc *Service
	log *zap.Logger
}

func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// UpdateLocation godoc
// PATCH /api/v1/runner/location
func (h *Handler) UpdateLocation(w http.ResponseWriter, r *http.Request) {
	var req UpdateLocationRequest

	// Support both JSON body and query params (battery-friendly background upload)
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_BODY", "could not parse request body")
			return
		}
	} else {
		q := r.URL.Query()
		lat, err1 := strconv.ParseFloat(q.Get("lat"), 64)
		lng, err2 := strconv.ParseFloat(q.Get("lng"), 64)
		if err1 != nil || err2 != nil {
			response.BadRequest(w, "MISSING_COORDS", "lat and lng are required")
			return
		}
		req = UpdateLocationRequest{Lat: lat, Lng: lng}
	}

	if err := h.svc.UpdateLocation(r.Context(), req); err != nil {
		switch {
		case errors.Is(err, ErrNotRunner):
			response.Forbidden(w, "only runners can update location")
		default:
			h.log.Error("UpdateLocation failed", zap.Error(err))
			response.InternalError(w)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// NearbyRunners godoc
// GET /api/v1/errands/:errandId/nearby-runners?radius=5
func (h *Handler) NearbyRunners(w http.ResponseWriter, r *http.Request) {
	errandID, err := uuid.Parse(chi.URLParam(r, "errandId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "errand ID must be a valid UUID")
		return
	}

	radius := defaultRadiusKm
	if rStr := r.URL.Query().Get("radius"); rStr != "" {
		if v, err := strconv.ParseFloat(rStr, 64); err == nil {
			radius = v
		}
	}

	runners, err := h.svc.NearbyRunners(r.Context(), errandID, radius)
	if err != nil {
		switch {
		case errors.Is(err, ErrNoPickupStop):
			response.BadRequest(w, "NO_PICKUP_COORDS",
				"This errand has no pickup location with coordinates.")
		default:
			h.log.Error("NearbyRunners failed", zap.Error(err))
			response.InternalError(w)
		}
		return
	}

	response.JSON(w, http.StatusOK, map[string]any{
		"runners": runners,
		"total":   len(runners),
	})
}
