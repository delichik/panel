package httpx

import (
	"encoding/json"
	"errors"
	"net/http"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/i18n"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
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
	var details map[string]any
	var domain *panelerr.Error
	if errors.As(err, &domain) {
		status = domain.HTTPStatus
		code = domain.Code
		if len(domain.Details) > 0 {
			details = domain.Details
		}
		if code == "application_invalid" && details != nil {
			message = domain.Message
		} else {
			message = i18n.Translate(domain.Code, domain.Message)
		}
	} else {
		message = i18n.Translate(code, message)
	}
	logFields := []zap.Field{zap.Int("status", status), zap.String("code", code), zap.Error(err)}
	if status >= http.StatusInternalServerError {
		logging.L().Error("api error response", logFields...)
	} else {
		logging.L().Debug("api error response", logFields...)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{Data: nil, Error: &APIError{Code: code, Message: message, Details: details}})
}

func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		Error(w, panelerr.BadRequest("bad_request", "Invalid JSON request body"))
		return false
	}
	return true
}
