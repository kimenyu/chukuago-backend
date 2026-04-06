package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/chukuago/api/pkg/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the auth-specific data access layer.
type Store struct {
	db *pgxpool.Pool
}

// NewStore creates an auth Store.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// GetOrCreateUserByPhone fetches an existing user by phone, or inserts a new one.
// The operation is atomic via an ON CONFLICT clause.
func (s *Store) GetOrCreateUserByPhone(ctx context.Context, phone, role string) (*types.User, error) {
	query := `
		INSERT INTO users (id, phone, role, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'active', NOW(), NOW())
		ON CONFLICT (phone) DO UPDATE SET updated_at = NOW()
		RETURNING id, phone, COALESCE(email, ''), COALESCE(name, ''), role, status, created_at, updated_at
	`

	row := s.db.QueryRow(ctx, query, uuid.New(), phone, role)
	return scanUser(row)
}

// GetUserByID fetches a user by primary key.
func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (*types.User, error) {
	query := `
		SELECT id, phone, COALESCE(email, ''), COALESCE(name, ''), role, status, created_at, updated_at
		FROM users WHERE id = $1 AND status != 'deleted'
	`
	row := s.db.QueryRow(ctx, query, id)
	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, ErrUserNotFound
	}
	return u, err
}

// CreateSession persists a new refresh token session.
func (s *Store) CreateSession(ctx context.Context, userID uuid.UUID, refreshToken string, expiresAt time.Time) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO user_sessions (id, user_id, refresh_token, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, NOW())`,
		uuid.New(), userID, refreshToken, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// GetSessionByToken retrieves a non-revoked, non-expired session by refresh token.
func (s *Store) GetSessionByToken(ctx context.Context, refreshToken string) (*types.UserSession, error) {
	query := `
		SELECT id, user_id, refresh_token, expires_at, revoked_at, created_at
		FROM user_sessions
		WHERE refresh_token = $1 AND revoked_at IS NULL AND expires_at > NOW()
	`
	var sess types.UserSession
	err := s.db.QueryRow(ctx, query, refreshToken).Scan(
		&sess.ID, &sess.UserID, &sess.RefreshToken,
		&sess.ExpiresAt, &sess.RevokedAt, &sess.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, ErrSessionGone
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &sess, nil
}

// RevokeSession marks a session as revoked by ID.
func (s *Store) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`UPDATE user_sessions SET revoked_at = NOW() WHERE id = $1`,
		sessionID,
	)
	return err
}

// RevokeAllUserSessions revokes every session for a given user — used on logout-all.
func (s *Store) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`UPDATE user_sessions SET revoked_at = NOW()
		 WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
	)
	return err
}

func scanUser(row pgx.Row) (*types.User, error) {
	var u types.User
	err := row.Scan(
		&u.ID, &u.Phone, &u.Email, &u.Name,
		&u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
