package user

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/kimenyu/chukuagobackend/types"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Create User
func (s *Store) CreateUser(ctx context.Context, user *types.User) error {
	query := `
		INSERT INTO users (id, name, email, password, role, status, phone, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8,$9)
	`

	_, err := s.db.ExecContext(
		ctx,
		query,
		user.ID,
		user.Name,
		user.Email,
		user.Password,
		user.Role,
		user.Status,
		user.Phone,
		user.CreatedAt,
		user.UpdatedAt,
	)

	return err
}

// Get User by Email
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*types.User, error) {
	query := `
		SELECT id, name, email, password, role, phone, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	user := new(types.User)

	err := s.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.Password,
		&user.Role,
		&user.Phone,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	return user, nil
}

func (s *Store) GetOrCreateWallet(ctx context.Context, userID uuid.UUID) (*types.Wallet, error) {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO wallets (user_id, currency)
        VALUES ($1, 'KES')
        ON CONFLICT (user_id) DO NOTHING
    `, userID)
	if err != nil {
		return nil, err
	}

	var w types.Wallet
	err = s.db.QueryRowContext(ctx, `
        SELECT id, user_id, currency, balance, created_at, updated_at
        FROM wallets WHERE user_id = $1
    `, userID).Scan(&w.ID, &w.UserID, &w.Currency, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	return &w, err
}

func (s *Store) GetOrCreateClientProfile(ctx context.Context, userID uuid.UUID) (*types.ClientProfile, error) {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO client_profiles (user_id, preferred_currency)
        VALUES ($1, 'KES')
        ON CONFLICT (user_id) DO NOTHING
    `, userID)
	if err != nil {
		return nil, err
	}

	var p types.ClientProfile
	err = s.db.QueryRowContext(ctx, `
        SELECT user_id, bio, preferred_currency, default_region_id,
               total_errands, active_errands, dispute_count, created_at, updated_at
        FROM client_profiles WHERE user_id = $1
    `, userID).Scan(
		&p.UserID, &p.Bio, &p.PreferredCurrency, &p.DefaultRegionID,
		&p.TotalErrands, &p.ActiveErrands, &p.DisputeCount,
		&p.CreatedAt, &p.UpdatedAt,
	)
	return &p, err
}

func (s *Store) GetOrCreateRunnerProfile(ctx context.Context, userID uuid.UUID) (*types.RunnerProfile, error) {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO runner_profiles (user_id, kyc_status, is_available, rating_avg, rating_count, completed_count, cancelled_count)
        VALUES ($1, 'unsubmitted', false, 0, 0, 0, 0)
        ON CONFLICT (user_id) DO NOTHING
    `, userID)
	if err != nil {
		return nil, err
	}

	var p types.RunnerProfile
	err = s.db.QueryRowContext(ctx, `
        SELECT user_id, bio, kyc_status, vehicle_type, vehicle_plate,
               is_available, rating_avg, rating_count, completed_count, cancelled_count,
               created_at, updated_at
        FROM runner_profiles WHERE user_id = $1
    `, userID).Scan(
		&p.UserID, &p.Bio, &p.KYCStatus, &p.VehicleType, &p.VehiclePlate,
		&p.IsAvailable, &p.RatingAvg, &p.RatingCount, &p.CompletedCount, &p.CancelledCount,
		&p.CreatedAt, &p.UpdatedAt,
	)
	return &p, err
}

// Get User by ID
func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (*types.User, error) {
	query := `
		SELECT id, name, email, password, role, phone, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	user := new(types.User)

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.Password,
		&user.Role,
		&user.Phone,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	return user, nil
}

func (s *Store) UpdateUserLocation(ctx context.Context, userID uuid.UUID, lat, lng float64) error {
	query := `
	UPDATE users
	SET 
		last_lat = $1,
		last_lng = $2,
		last_geo = ST_SetSRID(ST_MakePoint($2, $1), 4326),
		last_seen_at = NOW(),
		updated_at = NOW()
	WHERE id = $3;
	`

	_, err := s.db.ExecContext(ctx, query, lat, lng, userID)
	return err
}
