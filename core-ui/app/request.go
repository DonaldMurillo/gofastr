package app

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

type requestContextKey struct{}

type overlayContextKey struct{}

func withOverlay(ctx context.Context, as ScreenType) context.Context {
	return context.WithValue(ctx, overlayContextKey{}, as)
}

// OverlayFromContext reports whether the screen is rendering as an intercepted overlay (see
// InterceptFrom) and which presentation it wears: ScreenDrawer or ScreenSheet.
func OverlayFromContext(ctx context.Context) (ScreenType, bool) {
	as, ok := ctx.Value(overlayContextKey{}).(ScreenType)
	return as, ok
}

type overlayOriginContextKey struct{}

type overlayOriginQueryContextKey struct{}

func withOverlayOrigin(ctx context.Context, origin string) context.Context {
	if i := strings.IndexByte(origin, '#'); i >= 0 {
		origin = origin[:i]
	}
	var rawQuery string
	if i := strings.IndexByte(origin, '?'); i >= 0 {
		origin, rawQuery = origin[:i], origin[i+1:]
	}
	if !strings.HasPrefix(origin, "/") || strings.HasPrefix(origin, "//") || strings.Contains(origin, `\`) {
		return ctx
	}
	ctx = context.WithValue(ctx, overlayOriginContextKey{}, origin)
	if q, err := url.ParseQuery(rawQuery); err == nil && len(q) > 0 {
		ctx = context.WithValue(ctx, overlayOriginQueryContextKey{}, q)
	}
	return ctx
}

// OverlayOriginFromContext returns the path of the page an intercepted overlay opened over (the
// list, or the record whose Related tab added to it), or "" on any other render.
func OverlayOriginFromContext(ctx context.Context) string {
	s, _ := ctx.Value(overlayOriginContextKey{}).(string)
	return s
}

// OverlayOriginQueryFromContext returns the query of the page an intercepted overlay opened over,
// or nil on any other render or when the origin had none.
func OverlayOriginQueryFromContext(ctx context.Context) url.Values {
	q, _ := ctx.Value(overlayOriginQueryContextKey{}).(url.Values)
	if q == nil {
		return nil
	}
	out := make(url.Values, len(q))
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// WithRequest returns a new context that carries r.
func WithRequest(ctx context.Context, r *http.Request) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(render.WithIDScope(ctx), requestContextKey{}, r)
}

// RequestFromContext returns the *http.Request associated with ctx, or nil if none was set.
func RequestFromContext(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestContextKey{}).(*http.Request)
	return r
}

// QueryFromContext returns the query of the request in ctx, or empty Values when none is attached.
func QueryFromContext(ctx context.Context) url.Values {
	r := RequestFromContext(ctx)
	if r == nil || r.URL == nil {
		return url.Values{}
	}
	return r.URL.Query()
}
