package response

import (
	"encoding/json"
	"net/http"
)

// Envelope wraps all API responses in a consistent shape.
type Envelope struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
}

// Meta carries pagination metadata on list responses.
type Meta struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
}

// APIError is the structured error payload returned on failures.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// JSON writes an HTTP response as JSON with the given status code.
func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{Success: true, Data: data})
}

// JSONList writes a paginated list response.
func JSONList(w http.ResponseWriter, status int, data interface{}, page, limit, total int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{
		Success: true,
		Data:    data,
		Meta:    &Meta{Page: page, Limit: limit, Total: total},
	})
}

// Error writes a structured error response.
func Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{
		Success: false,
		Error:   &APIError{Code: code, Message: message},
	})
}

// BadRequest is a shorthand for 400 errors.
func BadRequest(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusBadRequest, code, message)
}

// Unauthorized is a shorthand for 401 errors.
func Unauthorized(w http.ResponseWriter, message string) {
	Error(w, http.StatusUnauthorized, "UNAUTHORIZED", message)
}

// Forbidden is a shorthand for 403 errors.
func Forbidden(w http.ResponseWriter, message string) {
	Error(w, http.StatusForbidden, "FORBIDDEN", message)
}

// NotFound is a shorthand for 404 errors.
func NotFound(w http.ResponseWriter, code string) {
	Error(w, http.StatusNotFound, code, "resource not found")
}

// Conflict is a shorthand for 409 errors.
func Conflict(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusConflict, code, message)
}

// InternalError is a shorthand for 500 errors. Message is deliberately generic.
func InternalError(w http.ResponseWriter) {
	Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "an unexpected error occurred")
}

// UnprocessableEntity is a shorthand for 422 errors.
func UnprocessableEntity(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusUnprocessableEntity, code, message)
}
