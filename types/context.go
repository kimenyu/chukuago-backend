package types

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const UserKey contextKey = "userID"

func UserIDFromContext(ctx context.Context) uuid.UUID {
	userID, ok := ctx.Value(UserKey).(uuid.UUID)
	if !ok {
		return uuid.Nil
	}
	return userID
}
