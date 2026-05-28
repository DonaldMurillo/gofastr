package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Handler is a typed function that processes an input of type I and returns
// an output of type O. It receives a context (which carries the *http.Request
// via RequestFromContext) and a fully-bound input struct.
type Handler[I, O any] func(ctx context.Context, in I) (O, error)

// HandlerAdapter bridges a typed Handler into a standard http.HandlerFunc.
// It handles input binding, panic recovery, and output serialization.
func HandlerAdapter[I, O any](h Handler[I, O]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				err := Errorf(http.StatusInternalServerError, "internal server error: %v", rec)
				WriteError(w, err)
			}
		}()

		ctx := r.Context()
		ctx = context.WithValue(ctx, requestKey{}, r)

		var in I
		if err := Bind(r, &in); err != nil {
			WriteError(w, err)
			return
		}

		out, err := h(ctx, in)
		if err != nil {
			WriteError(w, err)
			return
		}

		Respond(w, r, out)
	}
}

// Error is a structured HTTP error with optional field-level validation errors.
type Error struct {
	Code    int                 // HTTP status code
	Message string              // human-readable message
	Err     error               // wrapped cause (optional)
	Fields  map[string][]string // field-level validation errors (optional)
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap returns the wrapped cause for errors.Is/As.
func (e *Error) Unwrap() error {
	return e.Err
}

// Errorf creates a new Error with the given HTTP status code and formatted message.
func Errorf(code int, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// WrapError wraps an existing error with an HTTP status code and message.
func WrapError(code int, message string, err error) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// ValidationError creates a 400 error with field-level validation messages.
func ValidationError(fields map[string][]string) *Error {
	return &Error{
		Code:    http.StatusBadRequest,
		Message: "validation failed",
		Fields:  fields,
	}
}

// errorResponse is the JSON shape sent to clients.
type errorResponse struct {
	Error   errorResponseDetail `json:"error"`
	Success bool                `json:"success"`
}

type errorResponseDetail struct {
	Code    int                 `json:"code"`
	Message string              `json:"message"`
	Fields  map[string][]string `json:"fields,omitempty"`
}

// WriteError writes a structured error response to w.
//
// An *Error is rendered verbatim (its Code, Message, and Fields are
// what the developer chose to expose). Any other error type is treated
// as an internal failure: the response message is a generic
// "internal server error" with status 500, and the original error
// stays out of the response body. Callers that *do* want the inner
// message to reach the client must wrap explicitly via [Errorf] or
// [WrapError] (the latter keeps the cause for `errors.Is/As` without
// leaking it).
//
// This prevents accidental leaks of database errors, driver-specific
// strings ("pq: password authentication failed for user \"admin\""),
// or wrapped stack traces.
func WriteError(w http.ResponseWriter, err error) {
	herr, ok := err.(*Error)
	if !ok {
		herr = &Error{
			Code:    http.StatusInternalServerError,
			Message: "internal server error",
			Err:     err,
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(herr.Code)

	resp := errorResponse{
		Success: false,
		Error: errorResponseDetail{
			Code:    herr.Code,
			Message: herr.Message,
			Fields:  herr.Fields,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

// RequestFromContext retrieves the *http.Request stored by HandlerAdapter.
func RequestFromContext(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(requestKey{}).(*http.Request)
	return r, ok
}

// context keys
type requestKey struct{}
