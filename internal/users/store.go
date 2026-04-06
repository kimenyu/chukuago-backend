package users

import (
	"context"
	"fmt"

	"github.com/chukuago/api/pkg/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the users data access layer.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

func (s *Store) GetByID(ctx context.Context, id uuid.UUID) (*types.User, error) {
	const query = `
		SELECT id, phone, COALESCE(email,''), COALESCE(name,''), role, status, created_at, updated_at
		FROM users WHERE id = $1 AND status != 'deleted'
	`
	var u types.User
	err := s.db.QueryRow(ctx, query, id).Scan(
		&u.ID, &u.Phone, &u.Email, &u.Name, &u.Role, &u.Status,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return &u, nil
}

// UpdateProfile applies partial updates to both the users table and the
// appropriate profile table (client_profiles / runner_profiles).
// The whole operation runs in a single transaction.
func (s *Store) UpdateProfile(ctx context.Context, id uuid.UUID, req UpdateProfileRequest) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if req.Name != nil || req.Email != nil {
		_, err = tx.Exec(ctx,
			`UPDATE users SET
				name        = COALESCE($2, name),
				email       = COALESCE($3, email),
				updated_at  = NOW()
			 WHERE id = $1`,
			id, req.Name, req.Email,
		)
		if err != nil {
			return fmt.Errorf("update user row: %w", err)
		}
	}

	if req.Bio != nil || req.ProfilePicture != nil {
		_, err = tx.Exec(ctx,
			`INSERT INTO client_profiles (user_id, bio, profile_pic, updated_at)
			 VALUES ($1, $2, $3, NOW())
			 ON CONFLICT (user_id) DO UPDATE SET
				bio         = COALESCE(EXCLUDED.bio, client_profiles.bio),
				profile_pic = COALESCE(EXCLUDED.profile_pic, client_profiles.profile_pic),
				updated_at  = NOW()`,
			id, req.Bio, req.ProfilePicture,
		)
		if err != nil {
			return fmt.Errorf("upsert client profile: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// UpdateLocation updates the user's last-known GPS coordinates.
func (s *Store) UpdateLocation(ctx context.Context, id uuid.UUID, lat, lng float64) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET last_lat = $2, last_lng = $3, last_seen_at = NOW(), updated_at = NOW()
		 WHERE id = $1`,
		id, lat, lng,
	)
	if err != nil {
		return fmt.Errorf("update location: %w", err)
	}
	return nil
}

// GetClientProfile fetches or initialises a client's profile row.
func (s *Store) GetClientProfile(ctx context.Context, userID uuid.UUID) (*types.ClientProfile, error) {
	const query = `
		SELECT user_id, COALESCE(bio,''), COALESCE(profile_pic,''),
		       COALESCE(preferred_currency,'KES'), total_errands, active_errands,
		       dispute_count, created_at, updated_at
		FROM client_profiles WHERE user_id = $1
	`
	var p types.ClientProfile
	err := s.db.QueryRow(ctx, query, userID).Scan(
		&p.UserID, &p.Bio, &p.ProfilePic, &p.PreferredCurrency,
		&p.TotalErrands, &p.ActiveErrands, &p.DisputeCount,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil // profile not yet created
	}
	if err != nil {
		return nil, fmt.Errorf("get client profile: %w", err)
	}
	return &p, nil
}

// GetRunnerProfile fetches a runner's profile row.
func (s *Store) GetRunnerProfile(ctx context.Context, userID uuid.UUID) (*types.RunnerProfile, error) {
	const query = `
		SELECT user_id, bio, kyc_status, vehicle_type, vehicle_plate,
		       is_available, rating_avg, rating_count, completed_count, cancelled_count,
		       created_at, updated_at
		FROM runner_profiles WHERE user_id = $1
	`
	var p types.RunnerProfile
	err := s.db.QueryRow(ctx, query, userID).Scan(
		&p.UserID, &p.Bio, &p.KYCStatus, &p.VehicleType, &p.VehiclePlate,
		&p.IsAvailable, &p.RatingAvg, &p.RatingCount, &p.CompletedCount, &p.CancelledCount,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get runner profile: %w", err)
	}
	return &p, nil
}
