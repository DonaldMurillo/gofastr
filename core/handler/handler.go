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

// Handler is a typed handler; RequestFromContext gives it the request.
type Handler[I, O any] func(ctx context.Context, in I) (O, error)

// HandlerAdapter serves h over HTTP: Bind, h, then Respond or WriteError,
// with a panic answered as a generic 500.
func HandlerAdapter[I, O any](h Handler[I, O]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

const maxStackLogLen = 64 << 10

// Error is an HTTP error. The client reads Reason translated with Params
// (and Reason.Code() as "reason"), or Message when Reason is zero.
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

// Errorf returns an Error whose message is sent as written.
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

// WrapError returns an Error sending reason and keeping err as its cause.
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

type errorResponse struct {
	Error        string              `json:"error"`
	Code         int                 `json:"code"`
	Reason       string              `json:"reason,omitempty"`
	Fields       map[string][]string `json:"fields,omitempty"`
	FieldReasons map[string][]string `json:"field_reasons,omitempty"`
	Success      bool                `json:"success"`
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

// WriteError writes the error envelope ({"error","code","reason","fields",
// "field_reasons","success":false}) in r's locale. Only a *Error's own
// text is sent; any other error and a nil *Error are a generic 500, and
// a *Error whose code is outside 400-599 is sent as 500 with its own
// message, reason and violations. r may be nil.
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

	body := errorResponse{Code: code, Error: herr.Message}
	if !herr.Reason.IsZero() {
		body.Reason = herr.Reason.Code()
		body.Error = herr.Reason.Text(ctx, herr.Params)
	}
	body.Fields, body.FieldReasons = ViolationBodies(ctx, herr.Violations)
	json.NewEncoder(w).Encode(body)
}

// RequestFromContext retrieves the *http.Request stored by HandlerAdapter.
func RequestFromContext(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(requestKey{}).(*http.Request)
	return r, ok
}

type requestKey struct{}
