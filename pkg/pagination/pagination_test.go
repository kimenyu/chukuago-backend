package pagination_test

import (
	"net/http/httptest"
	"testing"

	"github.com/chukuago/api/pkg/pagination"
)

func TestParse_Defaults(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest("GET", "/", nil)
	p := pagination.Parse(r)

	if p.Page != pagination.DefaultPage {
		t.Errorf("Page: got %d, want %d", p.Page, pagination.DefaultPage)
	}
	if p.Limit != pagination.DefaultLimit {
		t.Errorf("Limit: got %d, want %d", p.Limit, pagination.DefaultLimit)
	}
	if p.Offset != 0 {
		t.Errorf("Offset: got %d, want 0", p.Offset)
	}
}

func TestParse_CustomValues(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest("GET", "/?page=3&limit=10", nil)
	p := pagination.Parse(r)

	if p.Page != 3 {
		t.Errorf("Page: got %d, want 3", p.Page)
	}
	if p.Limit != 10 {
		t.Errorf("Limit: got %d, want 10", p.Limit)
	}
	if p.Offset != 20 {
		t.Errorf("Offset: got %d, want 20", p.Offset)
	}
}

func TestParse_CapAtMaxLimit(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest("GET", "/?limit=999", nil)
	p := pagination.Parse(r)

	if p.Limit > pagination.MaxLimit {
		t.Errorf("Limit %d exceeds max %d", p.Limit, pagination.MaxLimit)
	}
}

func TestParse_NegativePage_FallsBackToDefault(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest("GET", "/?page=-5", nil)
	p := pagination.Parse(r)

	if p.Page != pagination.DefaultPage {
		t.Errorf("expected default page, got %d", p.Page)
	}
}

func TestParse_GarbageValues_DoNotPanic(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest("GET", "/?page=abc&limit=xyz", nil)
	p := pagination.Parse(r)

	if p.Page < 1 {
		t.Errorf("Page must be >= 1, got %d", p.Page)
	}
	if p.Limit < 1 {
		t.Errorf("Limit must be >= 1, got %d", p.Limit)
	}
}
