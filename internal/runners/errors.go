package runners

import "errors"

var (
	ErrNotFound        = errors.New("runner not found")
	ErrKYCAlreadyDone  = errors.New("KYC already submitted or approved")
	ErrAreaNotFound    = errors.New("service area not found")
	ErrAreaNotOwned    = errors.New("service area belongs to a different runner")
)
