// Package errors provides domain error types with structured error codes.
package errors

import (
	"fmt"
	"net/http"
)

// Code represents a machine-readable error code.
type Code string

const (
	CodeNotFound        Code = "NOT_FOUND"
	CodeAlreadyExists   Code = "ALREADY_EXISTS"
	CodeInvalidInput    Code = "INVALID_INPUT"
	CodeInternal        Code = "INTERNAL"
	CodeValidation      Code = "VALIDATION_ERROR"
	CodeConflict        Code = "CONFLICT"
	CodePayloadTooLarge Code = "PAYLOAD_TOO_LARGE"
	CodeRateLimited     Code = "MONTHLY_LIMIT_EXCEEDED"
)

// Error is the domain error type used throughout the application.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
	// RateLimitLimit/Used populate monthly ingest cap responses.
	RateLimitLimit *int64 `json:"-"`
	RateLimitUsed  *int64 `json:"-"`
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Err
}

// HTTPStatus maps error codes to HTTP status codes.
func (e *Error) HTTPStatus() int {
	switch e.Code {
	case CodeNotFound:
		return http.StatusNotFound
	case CodeAlreadyExists, CodeConflict:
		return http.StatusConflict
	case CodeInvalidInput, CodeValidation:
		return http.StatusBadRequest
	case CodePayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case CodeRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// Constructors

// NotFound creates a not found error.
func NotFound(entity, id string) *Error {
	return &Error{Code: CodeNotFound, Message: fmt.Sprintf("%s %q not found", entity, id)}
}

// AlreadyExists creates an already exists error.
func AlreadyExists(entity, id string) *Error {
	return &Error{Code: CodeAlreadyExists, Message: fmt.Sprintf("%s %q already exists", entity, id)}
}

// InvalidInput creates a validation error.
func InvalidInput(msg string) *Error {
	return &Error{Code: CodeInvalidInput, Message: msg}
}

// Validation creates a validation error with a field.
func Validation(field, reason string) *Error {
	return &Error{Code: CodeValidation, Message: fmt.Sprintf("field %q: %s", field, reason)}
}

// Internal creates an internal error, wrapping the cause.
func Internal(msg string, err error) *Error {
	return &Error{Code: CodeInternal, Message: msg, Err: err}
}

// MonthlyLimitExceeded is returned when a tenant exceeds its plan cap.
func MonthlyLimitExceeded(limit, used int64) *Error {
	return &Error{
		Code:           CodeRateLimited,
		Message:        fmt.Sprintf("monthly audit event limit exceeded (%d/%d)", used, limit),
		RateLimitLimit: &limit,
		RateLimitUsed:  &used,
	}
}
