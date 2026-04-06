package errands

import (
	"errors"

	"github.com/chukuago/api/pkg/types"
)

var (
	ErrNotFound          = errors.New("errand not found")
	ErrForbidden         = errors.New("you do not own this errand")
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrAlreadyAssigned   = errors.New("errand is already assigned")
	ErrNotCancellable    = errors.New("errand cannot be cancelled at this stage")
)

// validTransitions defines the allowed FSM edges for errand status.
// Clients drive: posted → bidding → assigned (via offer accept).
// Runners drive: assigned → in_progress → delivered.
// System drives: delivered → completed (via OTP verify), → disputed, → expired.
var validTransitions = map[types.ErrandStatus][]types.ErrandStatus{
	types.ErrandDraft:      {types.ErrandPosted, types.ErrandCancelled},
	types.ErrandPosted:     {types.ErrandBidding, types.ErrandAssigned, types.ErrandCancelled, types.ErrandExpired},
	types.ErrandBidding:    {types.ErrandAssigned, types.ErrandCancelled, types.ErrandExpired},
	types.ErrandAssigned:   {types.ErrandInProgress, types.ErrandCancelled},
	types.ErrandInProgress: {types.ErrandDelivered},
	types.ErrandDelivered:  {types.ErrandCompleted, types.ErrandDisputed},
	types.ErrandCompleted:  {}, // terminal
	types.ErrandCancelled:  {}, // terminal
	types.ErrandDisputed:   {types.ErrandCompleted, types.ErrandCancelled},
	types.ErrandExpired:    {}, // terminal
}

// IsValidTransition returns true if moving from → to is a permitted FSM edge.
func IsValidTransition(from, to types.ErrandStatus) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}
