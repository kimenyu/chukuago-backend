package pagination

import (
	"net/http"
	"strconv"
)

const (
	DefaultPage  = 1
	DefaultLimit = 20
	MaxLimit     = 100
)

// Params holds the parsed and validated pagination values from a request.
type Params struct {
	Page   int
	Limit  int
	Offset int
}

// Parse extracts and validates page/limit query parameters from an HTTP request.
// It always returns safe defaults and never returns an error — invalid values
// fall back to defaults rather than rejecting the request.
func Parse(r *http.Request) Params {
	q := r.URL.Query()

	page, err := strconv.Atoi(q.Get("page"))
	if err != nil || page < 1 {
		page = DefaultPage
	}

	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	return Params{
		Page:   page,
		Limit:  limit,
		Offset: (page - 1) * limit,
	}
}
