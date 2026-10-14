package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"

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
			message, details = translateApplicationValidationError(domain.Message, details)
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

// MaxRequestBodyBytes caps JSON request bodies decoded through Decode. Uploads
// handled via multipart parsing are not affected.
const MaxRequestBodyBytes = 10 << 20 // 10 MiB

func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			Error(w, panelerr.BadRequest("request_body_too_large", "Request body exceeds 10 MiB limit"))
			return false
		}
		Error(w, panelerr.BadRequest("bad_request", "Invalid JSON request body"))
		return false
	}
	return true
}

func translateApplicationValidationError(message string, details map[string]any) (string, map[string]any) {
	translatedDetails, firstField, firstMessage := translateApplicationValidationDetails(details)
	if firstMessage != "" {
		if firstField != "" {
			return firstField + ": " + firstMessage, translatedDetails
		}
		return firstMessage, translatedDetails
	}
	return translateValidationMessage(message), translatedDetails
}

func translateApplicationValidationDetails(details map[string]any) (map[string]any, string, string) {
	if details == nil {
		return nil, "", ""
	}
	rawIssues, ok := details["issues"]
	if !ok {
		return details, "", ""
	}
	value := reflect.ValueOf(rawIssues)
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return details, "", ""
	}
	issues := make([]map[string]string, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		field, message, ok := validationIssueFromValue(value.Index(i))
		if !ok {
			continue
		}
		issues = append(issues, map[string]string{
			"field":   field,
			"message": translateValidationMessage(message),
		})
	}
	if len(issues) == 0 {
		return details, "", ""
	}
	translated := make(map[string]any, len(details))
	for key, value := range details {
		translated[key] = value
	}
	translated["issues"] = issues
	return translated, issues[0]["field"], issues[0]["message"]
}

func validationIssueFromValue(value reflect.Value) (string, string, bool) {
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "", "", false
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		field := stringField(value.FieldByName("Field"))
		message := stringField(value.FieldByName("Message"))
		return field, message, message != ""
	case reflect.Map:
		field := stringMapValue(value, "field")
		message := stringMapValue(value, "message")
		return field, message, message != ""
	default:
		return "", "", false
	}
}

func stringField(value reflect.Value) string {
	if !value.IsValid() || value.Kind() != reflect.String {
		return ""
	}
	return value.String()
}

func stringMapValue(value reflect.Value, key string) string {
	if value.Type().Key().Kind() != reflect.String {
		return ""
	}
	item := value.MapIndex(reflect.ValueOf(key))
	for item.IsValid() && (item.Kind() == reflect.Interface || item.Kind() == reflect.Pointer) {
		if item.IsNil() {
			return ""
		}
		item = item.Elem()
	}
	if !item.IsValid() || item.Kind() != reflect.String {
		return ""
	}
	return item.String()
}

func translateValidationMessage(message string) string {
	field, detail, ok := strings.Cut(message, ": ")
	if ok {
		translated := i18n.Translate("", detail)
		if translated != detail {
			return field + ": " + translated
		}
	}
	return i18n.Translate("", message)
}
