package i18n

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// HTTPError is http.Error with a translated message: it writes status
// and p's text in the request's locale as a text/plain body. A zero p,
// or status's own [StatusPhrase] that the locale does not translate,
// writes http.StatusText ("Internal Server Error"), as http.Error does.
// Framework code reaches every plain-text refusal through here, so an
// app's catalog translates them all; the repo's rawtext analyzer
// refuses a bare http.Error.
func HTTPError(w http.ResponseWriter, r *http.Request, status int, p Phrase, params ...map[string]any) {
	msg := http.StatusText(status)
	if !p.IsZero() {
		ctx := context.Background()
		if r != nil {
			ctx = r.Context()
		}
		if s := p.Text(ctx, params...); p.Key() != StatusPhrase(status).Key() || s != p.English() {
			msg = s
		}
	}
	h := w.Header()
	h.Del("Content-Length")
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	fmt.Fprintln(w, msg)
}

// Status phrases: the generic refusal for each HTTP error status, under
// "error.<code>". A refusal with nothing more specific to say uses
// [StatusPhrase].
var statusPhrases = map[int]Phrase{
	http.StatusBadRequest:                   Define("error.bad_request", "bad request"),
	http.StatusUnauthorized:                 Define("error.unauthorized", "unauthorized"),
	http.StatusForbidden:                    Define("error.forbidden", "forbidden"),
	http.StatusNotFound:                     Define("error.not_found", "not found"),
	http.StatusMethodNotAllowed:             Define("error.method_not_allowed", "method not allowed"),
	http.StatusNotAcceptable:                Define("error.not_acceptable", "not acceptable"),
	http.StatusConflict:                     Define("error.conflict", "conflict"),
	http.StatusGone:                         Define("error.gone", "gone"),
	http.StatusLengthRequired:               Define("error.length_required", "length required"),
	http.StatusPreconditionFailed:           Define("error.precondition_failed", "precondition failed"),
	http.StatusRequestEntityTooLarge:        Define("error.body_too_large", "request body too large"),
	http.StatusRequestURITooLong:            Define("error.uri_too_long", "request URI too long"),
	http.StatusUnsupportedMediaType:         Define("error.unsupported_media_type", "unsupported media type"),
	http.StatusRequestedRangeNotSatisfiable: Define("error.range_not_satisfiable", "range not satisfiable"),
	http.StatusUnprocessableEntity:          Define("error.unprocessable", "unprocessable request"),
	http.StatusPreconditionRequired:         Define("error.precondition_required", "precondition required"),
	http.StatusTooManyRequests:              Define("error.too_many_requests", "too many requests"),
	http.StatusRequestHeaderFieldsTooLarge:  Define("error.headers_too_large", "request headers too large"),
	http.StatusInternalServerError:          Define("error.internal", "internal server error"),
	http.StatusNotImplemented:               Define("error.not_implemented", "not implemented"),
	http.StatusBadGateway:                   Define("error.bad_gateway", "bad gateway"),
	http.StatusServiceUnavailable:           Define("error.unavailable", "service unavailable"),
	http.StatusGatewayTimeout:               Define("error.gateway_timeout", "gateway timeout"),
}

// StatusPhrase returns the generic phrase for an HTTP error status, or
// the zero Phrase for a status without one.
func StatusPhrase(status int) Phrase { return statusPhrases[status] }

// HTTPErrorFor is [HTTPError] for an error: a [Phrased] error in err's
// chain translates with its params; any other error writes status's
// generic phrase, so an unphrased error's own text (often a driver's or
// a library's) never reaches the body.
func HTTPErrorFor(w http.ResponseWriter, r *http.Request, status int, err error) {
	if e, ok := errors.AsType[Phrased](err); ok {
		if p, params := e.Phrased(); !p.IsZero() {
			HTTPError(w, r, status, p, params)
			return
		}
	}
	HTTPError(w, r, status, StatusPhrase(status))
}

// Request refusals more than one package writes.
var (
	ReasonBodyUnreadable    = Define("error.body_unreadable", "failed to read request body")
	ReasonInvalidJSONBody   = Define("error.invalid_json_body", "invalid JSON body")
	ReasonInvalidForm       = Define("error.invalid_form", "invalid form data")
	ReasonJSONMediaType     = Define("error.json_media_type", "unsupported media type: expected application/json")
	ReasonTooManyStreams    = Define("error.too_many_streams", "too many concurrent streams for this caller")
	ReasonWebSocketRequired = Define("error.websocket_required", "websocket upgrade required")
	ReasonCrossOriginHost   = Define("error.cross_origin_host", "forbidden: cross-origin or unexpected Host")
	ReasonOriginRefused     = Define("error.origin_refused", "forbidden: request origin not allowed")
)
