package client

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

// get client profile
func (s *Store) GetClientProfile(ctx context.Context, userID uuid.UUID) (*types.ClientProfile, error) {
	query := `
		SELECT 
			user_id,
			bio,
			preferred_currency,
			default_region_id,
			total_errands,
			active_errands,
			dispute_count,
			created_at,
			updated_at
		FROM client_profiles
		WHERE user_id = $1
	`

	clientProfile := &types.ClientProfile{}

	err := s.db.QueryRowContext(ctx, query, userID).Scan(
		&clientProfile.UserID,
		&clientProfile.Bio,
		&clientProfile.PreferredCurrency,
		&clientProfile.DefaultRegionID,
		&clientProfile.TotalErrands,
		&clientProfile.ActiveErrands,
		&clientProfile.DisputeCount,
		&clientProfile.CreatedAt,
		&clientProfile.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("client profile not found")
		}
		return nil, err
	}

	return clientProfile, nil
}

// update client profile
func (s *Store) UpdateClientProfile(ctx context.Context, profile *types.ClientProfile) error {
	query := `
		UPDATE client_profiles
		SET 
			bio = $1,
			profile_picture = $2,
			updated_at = $3
		WHERE user_id = $4
	`

	_, err := s.db.ExecContext(
		ctx,
		query,
		profile.Bio,
		profile.ProfilePic,
		profile.UpdatedAt,
		profile.UserID,
	)

	return err
}
