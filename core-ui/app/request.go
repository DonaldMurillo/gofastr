package app

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// requestContextKey is the unexported type used to store the active
// *http.Request on a context.Context. The host (uihost / framework
// router) wraps the per-request context with WithRequest before
// calling Screen.Load, so screens can read URL query params, headers,
// or any other request data via RequestFromContext.
type requestContextKey struct{}

// overlayContextKey marks a render as an intercepted overlay.
type overlayContextKey struct{}

func withOverlay(ctx context.Context, as ScreenType) context.Context {
	return context.WithValue(ctx, overlayContextKey{}, as)
}

// OverlayFromContext reports whether the screen is rendering as an
// intercepted overlay (see InterceptFrom) and which presentation it
// wears: ScreenDrawer or ScreenSheet. A screen draws the chrome a
// layer needs (a close control, the page's path) only then; the
// canonical full page is the same render without it.
func OverlayFromContext(ctx context.Context) (ScreenType, bool) {
	as, ok := ctx.Value(overlayContextKey{}).(ScreenType)
	return as, ok
}

// overlayOriginContextKey carries the path an overlay opened over.
type overlayOriginContextKey struct{}

// withOverlayOrigin records the origin's path, query and fragment cut
// off. Anything but a rooted local path (a protocol-relative
// "//host", a backslash a browser reads as one) records nothing.
func withOverlayOrigin(ctx context.Context, origin string) context.Context {
	if i := strings.IndexAny(origin, "?#"); i >= 0 {
		origin = origin[:i]
	}
	if !strings.HasPrefix(origin, "/") || strings.HasPrefix(origin, "//") || strings.Contains(origin, `\`) {
		return ctx
	}
	return context.WithValue(ctx, overlayOriginContextKey{}, origin)
}

// OverlayOriginFromContext returns the path of the page an intercepted
// overlay opened over (the list, or the record whose Related tab added
// to it), or "" on any other render. A form in the overlay navigates
// there on success, and the runtime answers that by closing the overlay
// and refreshing the page under it instead of leaving it.
func OverlayOriginFromContext(ctx context.Context) string {
	s, _ := ctx.Value(overlayOriginContextKey{}).(string)
	return s
}

// WithRequest returns a new context that carries r. The host should
// call this exactly once per page render, typically inside the HTTP
// handler that drives the screen.
func WithRequest(ctx context.Context, r *http.Request) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, requestContextKey{}, r)
}

// RequestFromContext returns the *http.Request associated with ctx, or
// nil if none was set. Always nil-check the result, Load may run in
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
