package types

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

type contextKey string

const (
	UserKey     contextKey = "userID"
	UserRoleKey contextKey = "userRole" // ✅ add this
)

func UserIDFromContext(ctx context.Context) (uuid.UUID, error) {
	userID, ok := ctx.Value(UserKey).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return uuid.Nil, errors.New("userID not found in context")
	}
	return userID, nil
}

func UserRoleFromContext(ctx context.Context) (string, error) {
	role, ok := ctx.Value(UserRoleKey).(string)
	if !ok || role == "" {
		return "", errors.New("role not found in context")
	}
	return role, nil
}
