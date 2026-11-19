package i18n

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// HTTPError writes status and p's text in the request's locale as text/plain,
// http.StatusText for a zero or untranslated status phrase, and 500 for a
// status outside 400-599.
func HTTPError(w http.ResponseWriter, r *http.Request, status int, p Phrase, params ...map[string]any) {
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
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

// Status phrases: the generic refusal for each HTTP error status.
var statusPhrases = map[int]Phrase{
	http.StatusBadRequest:                    Define("error.bad_request", "bad request"),
	http.StatusUnauthorized:                  Define("error.unauthorized", "unauthorized"),
	http.StatusForbidden:                     Define("error.forbidden", "forbidden"),
	http.StatusNotFound:                      Define("error.not_found", "not found"),
	http.StatusMethodNotAllowed:              Define("error.method_not_allowed", "method not allowed"),
	http.StatusNotAcceptable:                 Define("error.not_acceptable", "not acceptable"),
	http.StatusPaymentRequired:               Define("error.payment_required", "payment required"),
	http.StatusProxyAuthRequired:             Define("error.proxy_auth_required", "proxy authentication required"),
	http.StatusRequestTimeout:                Define("error.request_timeout", "request timeout"),
	http.StatusConflict:                      Define("error.conflict", "conflict"),
	http.StatusGone:                          Define("error.gone", "gone"),
	http.StatusLengthRequired:                Define("error.length_required", "length required"),
	http.StatusPreconditionFailed:            Define("error.precondition_failed", "precondition failed"),
	http.StatusRequestEntityTooLarge:         Define("error.body_too_large", "request body too large"),
	http.StatusRequestURITooLong:             Define("error.uri_too_long", "request URI too long"),
	http.StatusUnsupportedMediaType:          Define("error.unsupported_media_type", "unsupported media type"),
	http.StatusRequestedRangeNotSatisfiable:  Define("error.range_not_satisfiable", "range not satisfiable"),
	http.StatusExpectationFailed:             Define("error.expectation_failed", "expectation failed"),
	http.StatusTeapot:                        Define("error.teapot", "I'm a teapot"),
	http.StatusMisdirectedRequest:            Define("error.misdirected_request", "misdirected request"),
	http.StatusUnprocessableEntity:           Define("error.unprocessable", "unprocessable request"),
	http.StatusLocked:                        Define("error.locked", "locked"),
	http.StatusFailedDependency:              Define("error.failed_dependency", "failed dependency"),
	http.StatusTooEarly:                      Define("error.too_early", "too early"),
	http.StatusUpgradeRequired:               Define("error.upgrade_required", "upgrade required"),
	http.StatusPreconditionRequired:          Define("error.precondition_required", "precondition required"),
	http.StatusTooManyRequests:               Define("error.too_many_requests", "too many requests"),
	http.StatusRequestHeaderFieldsTooLarge:   Define("error.headers_too_large", "request headers too large"),
	http.StatusUnavailableForLegalReasons:    Define("error.legal_reasons", "unavailable for legal reasons"),
	http.StatusInternalServerError:           Define("error.internal", "internal server error"),
	http.StatusNotImplemented:                Define("error.not_implemented", "not implemented"),
	http.StatusBadGateway:                    Define("error.bad_gateway", "bad gateway"),
	http.StatusServiceUnavailable:            Define("error.unavailable", "service unavailable"),
	http.StatusGatewayTimeout:                Define("error.gateway_timeout", "gateway timeout"),
	http.StatusHTTPVersionNotSupported:       Define("error.http_version_not_supported", "HTTP version not supported"),
	http.StatusVariantAlsoNegotiates:         Define("error.variant_also_negotiates", "variant also negotiates"),
	http.StatusInsufficientStorage:           Define("error.insufficient_storage", "insufficient storage"),
	http.StatusLoopDetected:                  Define("error.loop_detected", "loop detected"),
	http.StatusNotExtended:                   Define("error.not_extended", "not extended"),
	http.StatusNetworkAuthenticationRequired: Define("error.network_auth_required", "network authentication required"),
}

// StatusPhrase returns the generic phrase for an HTTP error status, or zero.
func StatusPhrase(status int) Phrase { return statusPhrases[status] }

// HTTPErrorFor is [HTTPError] for err: its [Phrased] error, else status's
// generic phrase, never err's own text.
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
