package types

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// contextKey is a private type to avoid collisions in context values.
type contextKey string

const UserKey contextKey = "userID"

// UserIDFromContext extracts the userID from the context.
// Returns an error if not found or invalid.
func UserIDFromContext(ctx context.Context) (uuid.UUID, error) {
	userID, ok := ctx.Value(UserKey).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return uuid.Nil, errors.New("userID not found in context")
	}
	return userID, nil
}
