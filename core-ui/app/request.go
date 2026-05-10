package app

import (
	"context"
	"net/http"
	"net/url"
)

// requestContextKey is the unexported type used to store the active
// *http.Request on a context.Context. The host (uihost / framework
// router) wraps the per-request context with WithRequest before
// calling Screen.Load, so screens can read URL query params, headers,
// or any other request data via RequestFromContext.
type requestContextKey struct{}

// WithRequest returns a new context that carries r. The host should
// call this exactly once per page render — typically inside the HTTP
// handler that drives the screen.
func WithRequest(ctx context.Context, r *http.Request) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, requestContextKey{}, r)
}

// RequestFromContext returns the *http.Request associated with ctx, or
// nil if none was set. Always nil-check the result — Load may run in
// build-time SSG too, where there is no live request.
func RequestFromContext(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestContextKey{}).(*http.Request)
	return r
}

// QueryFromContext is a convenience that returns the URL query Values
// of the request in ctx, or an empty Values when no request is
// attached (e.g. SSG builds).
func QueryFromContext(ctx context.Context) url.Values {
	r := RequestFromContext(ctx)
	if r == nil || r.URL == nil {
		return url.Values{}
	}
	return r.URL.Query()
}
