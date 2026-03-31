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
		INSERT INTO users (id, name, email, password, role, phone, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := s.db.ExecContext(
		ctx,
		query,
		user.ID,
		user.Name,
		user.Email,
		user.Password,
		user.Role,
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
