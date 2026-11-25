package api

import (
	"fmt"
	"net/http"
)

type Code string

const (
	CodeInvalidRequest Code = "invalid_request"
	CodeUnauthorized   Code = "unauthorized"
	CodeNotFound       Code = "not_found"
	CodeConflict       Code = "conflict"
	CodeUpstream       Code = "upstream_unavailable"
	CodeRateLimited    Code = "rate_limited"
	CodeNotReady       Code = "not_ready"
	CodeInternal       Code = "internal"
)

type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewError(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func (e *Error) Status() int {
	switch e.Code {
	case CodeInvalidRequest:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeUpstream, CodeRateLimited, CodeNotReady:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func (e *Error) Retryable() bool {
	return e.Status() == http.StatusServiceUnavailable
}
