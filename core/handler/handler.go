package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Handler is a typed function that processes an input of type I and returns
// an output of type O. It receives a context (which carries the *http.Request
// via RequestFromContext) and a fully-bound input struct.
type Handler[I, O any] func(ctx context.Context, in I) (O, error)

// HandlerAdapter bridges a typed Handler into a standard http.HandlerFunc.
// It handles input binding, panic recovery, and output serialization.
func HandlerAdapter[I, O any](h Handler[I, O]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// started turns true once a status has gone out. A panic after
		// that (a ResponseType's WriteBody) cannot become a 500: the
		// client already has a 200 and part of a body, and an error
		// document written now would be spliced onto it as if it were
		// that body. Abort the connection instead, so the client sees a
		// broken response rather than a successful one.
		var started bool
		defer func() {
			if rec := recover(); rec != nil {
				if started {
					slog.Default().Error("panic recovered in handler after the response started; aborting it",
						"error", textsafe.Recovered(rec),
						"stack", textsafe.Truncate(string(debug.Stack()), maxStackLogLen),
					)
					panic(http.ErrAbortHandler)
				}
				// The raw panic value (driver strings, internal paths,
				// wrapped DB errors) must stay out of the client body.
				// Log it server-side and return a fixed generic 500,
				// mirroring middleware.RecoveryFn. textsafe.Recovered
				// scrubs C0/DEL/C1/bidi and truncates, so a giant or
				// control-laden panic can neither forge lines in the
				// operator's tail nor drive a multi-MB log.
				slog.Default().Error("panic recovered in handler",
					"error", textsafe.Recovered(rec),
					"stack", textsafe.Truncate(string(debug.Stack()), maxStackLogLen),
				)
				WriteError(w, r, Fail(http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError)))
			}
		}()

		ctx := r.Context()
		ctx = context.WithValue(ctx, requestKey{}, r)

		var in I
		if err := Bind(r, &in); err != nil {
			WriteError(w, r, err)
			return
		}

		out, err := h(ctx, in)
		if err != nil {
			WriteError(w, r, err)
			return
		}

		respond(w, r, out, &started)
	}
}

// Caps on the panic value / stack pieces logged from the recover path,
// so a handler that panics with a huge value can't drive a multi-MB log
// line through slog.Default.
const (
	maxPanicLogLen = 4 << 10  // 4 KiB
	maxStackLogLen = 64 << 10 // 64 KiB
)

// Error is a structured HTTP error with optional field-level validation errors.
//
// Reason, when set, is what the client reads: the response message is
// Reason translated into the request's locale with Params, and the
// body's "reason" is Reason.Code(), the stable code a client branches
// on. Message is the text an error with no Reason sends as written (an
// app's own wording); framework code always sets a Reason.
type Error struct {
	Code       int                           // HTTP status code
	Reason     i18n.Phrase                   // translated message + stable code (optional)
	Params     map[string]any                // Reason's {{placeholders}}
	Message    string                        // message sent as written when Reason is zero
	Err        error                         // wrapped cause (optional)
	Violations map[string][]schema.Violation // field-level validation errors (optional)
}

// Error implements the error interface with the English message.
func (e *Error) Error() string {
	msg := e.Message
	if !e.Reason.IsZero() {
		msg = e.Reason.Format(e.Params)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", msg, e.Err)
	}
	return msg
}

// Unwrap returns the wrapped cause for errors.Is/As.
func (e *Error) Unwrap() error {
	return e.Err
}

// Phrased implements i18n.Phrased; an Error with no Reason returns the
// zero Phrase, which i18n.ErrorText passes over.
func (e *Error) Phrased() (i18n.Phrase, map[string]any) { return e.Reason, e.Params }

// Errorf creates a new Error with the given HTTP status code and a
// formatted message sent as written. Framework code uses [Fail] so the
// message translates; Errorf is for an app's own wording.
func Errorf(code int, format string, args ...any) *Error {
	return &Error{
		Code: code,
		//gofastr:allow(rawtext) Errorf is the app author's own-wording escape hatch; framework code reaches for Fail
		Message: fmt.Sprintf(format, args...),
	}
}

// Fail creates an Error whose message is reason translated with params.
func Fail(code int, reason i18n.Phrase, params ...map[string]any) *Error {
	e := &Error{Code: code, Reason: reason}
	if len(params) > 0 {
		e.Params = params[0]
	}
	return e
}

// WrapError wraps an existing error with an HTTP status code and the
// phrase the client reads, keeping the cause for errors.Is/As without
// sending it.
func WrapError(code int, reason i18n.Phrase, err error) *Error {
	return &Error{
		Code:   code,
		Reason: reason,
		Err:    err,
	}
}

// ReasonValidationFailed is the message of a 400 that carries
// field-level violations.
var ReasonValidationFailed = i18n.Define("error.validation_failed", "validation failed")

// ValidationError creates a 400 error with field-level violations.
func ValidationError(fields map[string][]schema.Violation) *Error {
	return &Error{
		Code:       http.StatusBadRequest,
		Reason:     ReasonValidationFailed,
		Violations: fields,
	}
}

// errorResponse is the JSON shape sent to clients.
type errorResponse struct {
	Error   errorResponseDetail `json:"error"`
	Success bool                `json:"success"`
}

type errorResponseDetail struct {
	Code         int                 `json:"code"`
	Reason       string              `json:"reason,omitempty"`
	Message      string              `json:"message"`
	Fields       map[string][]string `json:"fields,omitempty"`
	FieldReasons map[string][]string `json:"field_reasons,omitempty"`
}

// ViolationBodies renders field violations for a response body: the
// messages in ctx's locale, and the stable codes in the same order.
func ViolationBodies(ctx context.Context, fields map[string][]schema.Violation) (messages, codes map[string][]string) {
	if len(fields) == 0 {
		return nil, nil
	}
	messages = make(map[string][]string, len(fields))
	codes = make(map[string][]string, len(fields))
	for name, vs := range fields {
		for _, v := range vs {
			messages[name] = append(messages[name], v.Text(ctx))
			codes[name] = append(codes[name], v.Code())
		}
	}
	return messages, codes
}

// WriteError writes a structured error response to w, translated into
// r's locale.
//
// An *Error is rendered as the developer built it: its Code, its
// Reason (translated, with the code in "reason") or its Message, and
// its Violations. Any other error type is treated as an internal
// failure: the response message is a generic "internal server error"
// with status 500, and the original error stays out of the response
// body. Callers that *do* want a message to reach the client must wrap
// explicitly via [Fail], [WrapError] or [Errorf] (WrapError keeps the
// cause for `errors.Is/As` without leaking it).
//
// This prevents accidental leaks of database errors, driver-specific
// strings ("pq: password authentication failed for user \"admin\""),
// or wrapped stack traces. r may be nil; the message is then English.
//
// A nil *Error is the generic 500, and a Code outside 400-599 (a
// forgotten zero, a 1xx or 2xx) is sent as 500: an error never goes out
// under a status that reads as success.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	herr, ok := err.(*Error)
	if !ok || herr == nil {
		herr = &Error{
			Code:   http.StatusInternalServerError,
			Reason: i18n.StatusPhrase(http.StatusInternalServerError),
			Err:    err,
		}
	}
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	code := herr.Code
	if code < 400 || code > 599 {
		code = http.StatusInternalServerError
	}
	w.WriteHeader(code)

	detail := errorResponseDetail{Code: code, Message: herr.Message}
	if !herr.Reason.IsZero() {
		detail.Reason = herr.Reason.Code()
		detail.Message = herr.Reason.Text(ctx, herr.Params)
	}
	detail.Fields, detail.FieldReasons = ViolationBodies(ctx, herr.Violations)
	json.NewEncoder(w).Encode(errorResponse{Success: false, Error: detail})
}

// RequestFromContext retrieves the *http.Request stored by HandlerAdapter.
func RequestFromContext(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(requestKey{}).(*http.Request)
	return r, ok
}

// context keys
type requestKey struct{}
