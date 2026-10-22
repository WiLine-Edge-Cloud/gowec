package wec

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// FieldError is one entry of a validation error's "fields" list.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// APIError is the public API's one error shape:
// {"error": {"code", "message", "request_id", "fields"?, ...extras}}.
type APIError struct {
	Status    int
	Code      string
	Message   string
	RequestID string
	Fields    []FieldError
	// Extra holds the other keys the error carried, e.g. "available" names
	// for an unknown_zone, "quoted_total" for launch_price_changed. It is nil
	// when the answer wasn't in the API's error shape (a proxy's page).
	Extra map[string]any
}

func (e *APIError) Error() string {
	msg := e.Code + ": " + e.Message
	if e.RequestID != "" {
		msg += " (request " + e.RequestID + ")"
	}
	return msg
}

// IsCode reports whether err is an APIError with the given code.
func IsCode(err error, code string) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.Code == code
}

func parseError(resp *http.Response, data []byte) *APIError {
	apiErr := &APIError{Status: resp.StatusCode, RequestID: resp.Header.Get("X-Request-Id")}

	var envelope struct {
		Error map[string]json.RawMessage `json:"error"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Error == nil {
		// Not ours (a proxy page, a gateway timeout): keep what we know.
		apiErr.Code = strings.ReplaceAll(strings.ToLower(http.StatusText(resp.StatusCode)), " ", "_")
		apiErr.Message = fmt.Sprintf("the server answered %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		return apiErr
	}

	apiErr.Extra = map[string]any{}
	for key, raw := range envelope.Error {
		switch key {
		case "code":
			_ = json.Unmarshal(raw, &apiErr.Code)
		case "message":
			_ = json.Unmarshal(raw, &apiErr.Message)
		case "request_id":
			_ = json.Unmarshal(raw, &apiErr.RequestID)
		case "fields":
			_ = json.Unmarshal(raw, &apiErr.Fields)
		default:
			var v any
			if json.Unmarshal(raw, &v) == nil {
				apiErr.Extra[key] = v
			}
		}
	}
	return apiErr
}
