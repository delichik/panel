package panelerr

import "net/http"

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string { return e.Message }

func New(status int, code, message string) *Error {
	return &Error{HTTPStatus: status, Code: code, Message: message}
}

func BadRequest(code, message string) *Error { return New(http.StatusBadRequest, code, message) }
func Unauthorized(message string) *Error {
	return New(http.StatusUnauthorized, "unauthorized", message)
}
func NotFound(resource string) *Error {
	return New(http.StatusNotFound, "not_found", resource+" not found")
}
func Conflict(code, message string) *Error { return New(http.StatusConflict, code, message) }
func Validation(code, message string) *Error {
	return New(http.StatusUnprocessableEntity, code, message)
}
func BadGateway(code, message string) *Error { return New(http.StatusBadGateway, code, message) }
func Timeout(message string) *Error          { return New(http.StatusGatewayTimeout, "remote_timeout", message) }
