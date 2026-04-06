package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chukuago/api/pkg/response"
)

func TestJSON_StatusAndBody(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	response.JSON(rr, http.StatusOK, map[string]string{"hello": "world"})

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rr.Code)
	}

	var env response.Envelope
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !env.Success {
		t.Error("expected success=true")
	}
}

func TestError_Shape(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	response.Error(rr, http.StatusBadRequest, "VALIDATION_ERROR", "name is required")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rr.Code)
	}

	var env response.Envelope
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Success {
		t.Error("expected success=false on error")
	}
	if env.Error == nil {
		t.Fatal("expected error object in response")
	}
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code: got %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

func TestJSONList_Meta(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	response.JSONList(rr, http.StatusOK, []string{"a", "b"}, 2, 10, 25)

	var env response.Envelope
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Meta == nil {
		t.Fatal("expected meta object in list response")
	}
	if env.Meta.Page != 2 {
		t.Errorf("meta.page: got %d, want 2", env.Meta.Page)
	}
	if env.Meta.Total != 25 {
		t.Errorf("meta.total: got %d, want 25", env.Meta.Total)
	}
}

func TestInternalError_IsGeneric(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	response.InternalError(rr)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rr.Code)
	}

	var env response.Envelope
	json.NewDecoder(rr.Body).Decode(&env) //nolint:errcheck
	if env.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("internal error code: got %q", env.Error.Code)
	}
}
