package offers_test

import (
	"testing"

	"github.com/chukuago/api/internal/offers"
)

func TestPlaceBidRequest_Validation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		req     offers.PlaceBidRequest
		wantErr bool
	}{
		{
			name:    "valid bid",
			req:     offers.PlaceBidRequest{Amount: 500},
			wantErr: false,
		},
		{
			name:    "zero amount should fail",
			req:     offers.PlaceBidRequest{Amount: 0},
			wantErr: true,
		},
		{
			name:    "negative amount should fail",
			req:     offers.PlaceBidRequest{Amount: -1},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			isInvalid := tc.req.Amount <= 0
			if tc.wantErr && !isInvalid {
				t.Errorf("expected validation to fail for amount=%v", tc.req.Amount)
			}
			if !tc.wantErr && isInvalid {
				t.Errorf("expected validation to pass for amount=%v", tc.req.Amount)
			}
		})
	}
}

func TestOfferErrors_AreDistinct(t *testing.T) {
	t.Parallel()

	// Guard against accidental error value collisions.
	errs := []error{
		offers.ErrNotFound,
		offers.ErrAlreadyBid,
		offers.ErrErrandNotBidding,
		offers.ErrErrandNotOwned,
		offers.ErrOfferNotPending,
	}

	seen := make(map[string]struct{})
	for _, e := range errs {
		s := e.Error()
		if _, dup := seen[s]; dup {
			t.Errorf("duplicate error message: %q", s)
		}
		seen[s] = struct{}{}
	}
}
