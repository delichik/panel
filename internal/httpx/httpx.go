package httpx

import (
	"encoding/json"
	"errors"
	"net/http"

	"panel/internal/panelerr"
)

type Envelope struct {
	Data  any       `json:"data"`
	Error *APIError `json:"error"`
}

type APIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{Data: data, Error: nil})
}

func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func Error(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "Internal server error"
	var domain *panelerr.Error
	if errors.As(err, &domain) {
		status = domain.HTTPStatus
		code = domain.Code
		message = domain.Message
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{Data: nil, Error: &APIError{Code: code, Message: message}})
}

func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		Error(w, panelerr.BadRequest("bad_request", "Invalid JSON request body"))
		return false
	}
	return true
}
