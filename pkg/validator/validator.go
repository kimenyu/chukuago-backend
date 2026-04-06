package validator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// Decode decodes JSON from the request body and validates the result.
// Returns (false, errorMessage) if decoding or validation fails.
func Decode(r *http.Request, dst interface{}) (bool, string) {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return false, fmt.Sprintf("invalid request body: %s", err.Error())
	}

	if err := validate.Struct(dst); err != nil {
		var errs validator.ValidationErrors
		if ok := strings.Contains(err.Error(), "validation"); ok {
			errs = err.(validator.ValidationErrors)
			return false, formatValidationErrors(errs)
		}
		return false, err.Error()
	}

	return true, ""
}

func formatValidationErrors(errs validator.ValidationErrors) string {
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		field := strings.ToLower(e.Field())
		switch e.Tag() {
		case "required":
			msgs = append(msgs, fmt.Sprintf("%s is required", field))
		case "min":
			msgs = append(msgs, fmt.Sprintf("%s must be at least %s characters", field, e.Param()))
		case "max":
			msgs = append(msgs, fmt.Sprintf("%s must be at most %s characters", field, e.Param()))
		case "email":
			msgs = append(msgs, fmt.Sprintf("%s must be a valid email address", field))
		case "oneof":
			msgs = append(msgs, fmt.Sprintf("%s must be one of: %s", field, e.Param()))
		case "e164":
			msgs = append(msgs, fmt.Sprintf("%s must be a valid phone number in E.164 format", field))
		default:
			msgs = append(msgs, fmt.Sprintf("%s is invalid (%s)", field, e.Tag()))
		}
	}
	return strings.Join(msgs, "; ")
}
