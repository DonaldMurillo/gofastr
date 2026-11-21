package embed

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"reflect"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

type grantCtxKey struct{}

// GrantFromContext returns the embed grant that authenticated this request.
func GrantFromContext(ctx context.Context) (Grant, bool) {
	g, ok := ctx.Value(grantCtxKey{}).(Grant)
	return g, ok
}

// WithGrant installs a verified grant on ctx, and its scopes as the held scopes.
func WithGrant(ctx context.Context, g Grant) context.Context {
	ctx = access.WithHeldScopes(ctx, g.Scopes)
	return context.WithValue(ctx, grantCtxKey{}, g)
}

// Middleware authenticates requests carrying a GrantHeader and passes others
// through untouched. Install it outermost, before any authentication
// middleware; it does not enforce scopes (see [Host.RequireScope]).
func (h *Host) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get(GrantHeader)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !h.Ready() {
				i18n.HTTPError(w, r, http.StatusServiceUnavailable, i18n.StatusPhrase(http.StatusServiceUnavailable))
				return
			}
			g, surface, err := h.verifyConfiguredGrant(r.Context(), token)
			if err != nil {
				i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
				return
			}

			grantWriter := &grantResponseWriter{ResponseWriter: w}
			grantWriter.setCachePolicy()
			defer grantWriter.setCachePolicy()
			w = grantWriter

			routed, routable := RoutedPath(r.URL)
			if !routable {
				i18n.HTTPError(w, r, http.StatusBadRequest, reasonPathNotCanonical)
				return
			}
			if res, reserved := reservedFor(h.reserved, path.Clean(routed)); reserved {
				i18n.HTTPError(w, r, http.StatusForbidden, reasonReservedPath, map[string]any{
					"surface": strconv.Quote(g.Surface), "path": routed, "reserved": res,
				})
				return
			}
			if !surface.MayReach(routed) && !h.widgetReach(surface, routed) {
				i18n.HTTPError(w, r, http.StatusForbidden, reasonOutsideReach, map[string]any{
					"surface": strconv.Quote(g.Surface), "path": routed,
					"suggestion": fmt.Sprintf("[]string{%q}", reachSuggestion(routed)),
				})
				return
			}

			r.Header.Del("Cookie")
			r.Header.Del("Authorization")
			r.Header.Del("X-API-Key")

			if u, ok := handler.GetUser(r.Context()); ok && u != nil {
				misordered(w, r, "a user")
				return
			}
			if t, ok := handler.GetTenant(r.Context()); ok && t != nil {
				misordered(w, r, "a tenant")
				return
			}
			if tenant.GetTenantID(r.Context()) != "" {
				misordered(w, r, "a tenant id")
				return
			}

			ctx := WithGrant(r.Context(), g)

			if !g.Expires.IsZero() {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, g.Expires)
				defer cancel()
			}

			ctx, hasUser, _, err := h.InstallSubject(ctx, g.Subject)
			if err != nil {
				i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
				return
			}
			if g.Subject != "" && (h.resolve != nil || h.resolveTenant != nil) {
				subject := g.Subject
				ctx = handler.WithPrincipalCheck(ctx, func(c context.Context) (context.Context, bool) {
					fresh, stillUser, _, err := h.InstallSubject(c, subject)
					return fresh, err == nil && stillUser == hasUser
				})
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type grantResponseWriter struct {
	http.ResponseWriter
	committed bool
}

func (w *grantResponseWriter) setCachePolicy() {
	if w.committed {
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "private, no-store")
	for _, value := range h.Values("Vary") {
		for field := range strings.SplitSeq(value, ",") {
			field = strings.TrimSpace(field)
			if strings.EqualFold(field, GrantHeader) || field == "*" {
				return
			}
		}
	}
	h.Add("Vary", GrantHeader)
}

func (w *grantResponseWriter) WriteHeader(status int) {
	w.setCachePolicy()
	w.ResponseWriter.WriteHeader(status)
	if status >= 200 || status == http.StatusSwitchingProtocols {
		w.committed = true
	}
}

func (w *grantResponseWriter) Write(p []byte) (int, error) {
	w.setCachePolicy()
	w.committed = true
	return w.ResponseWriter.Write(p)
}

func (w *grantResponseWriter) ReadFrom(src io.Reader) (int64, error) {
	w.setCachePolicy()
	n, err := io.Copy(w.ResponseWriter, src)
	if n > 0 {
		w.committed = true
	}
	return n, err
}

func (w *grantResponseWriter) FlushError() error {
	w.setCachePolicy()
	err := http.NewResponseController(w.ResponseWriter).Flush()
	if err == nil {
		w.committed = true
	}
	return err
}

func (w *grantResponseWriter) Flush() { _ = w.FlushError() }

func (w *grantResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *grantResponseWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (w *grantResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// RequireScope refuses an embed request whose grant lacks scope, and a grant
// header Middleware did not verify. Requests without a grant pass through.
func (h *Host) RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			g, ok := GrantFromContext(r.Context())
			if !ok {
				if r.Header.Get(GrantHeader) != "" {
					i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if !g.HasScope(scope) {
				i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// InstallSubject replaces the request identity with the grant subject's tenant and user.
// It reports whether a user was installed and names the failed lookup on error.
func (h *Host) InstallSubject(ctx context.Context, subject string) (resolved context.Context, hasUser bool, reason string, err error) {
	ctx = handler.SetUser(ctx, nil)
	ctx = handler.SetTenant(ctx, nil)
	ctx = tenant.SetTenantID(ctx, "")
	if subject == "" {
		return ctx, false, "", nil
	}
	if h.resolveTenant != nil {
		tid, err := h.resolveTenant(ctx, subject)
		if err != nil {
			return ctx, false, "tenant did not resolve", err
		}
		if tid != "" {
			ctx = handler.SetTenant(ctx, tid)
			ctx = tenant.SetTenantID(ctx, tid)
		}
	}
	if h.resolve == nil {
		return ctx, false, "", nil
	}
	user, err := h.resolve(ctx, subject)
	if err != nil {
		return ctx, false, "subject did not resolve", err
	}
	if IsNilValue(user) {
		return ctx, false, "", nil
	}
	return handler.SetUser(ctx, user), true, "", nil
}

func misordered(w http.ResponseWriter, r *http.Request, what string) {
	i18n.HTTPError(w, r, http.StatusInternalServerError, reasonMisordered, map[string]any{"what": what})
}

// IsNilValue reports whether v is nil, including a typed nil inside an interface.
func IsNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return rv.IsNil()
	}
	return false
}

func reachSuggestion(p string) string {
	cleaned := path.Clean(p)
	parts := strings.SplitN(strings.TrimPrefix(cleaned, "/"), "/", 3)
	if len(parts) >= 2 && parts[1] != "" {
		return "/" + parts[0] + "/" + parts[1]
	}
	if len(parts) >= 1 && parts[0] != "" {
		return "/" + parts[0]
	}
	return cleaned
}
