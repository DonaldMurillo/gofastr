package embed

import (
	"context"
	"fmt"
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
			g, err := h.VerifyGrant(r.Context(), token)
			if err != nil {
				i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
				return
			}

			surface, known := h.Lookup(g.Surface)
			if !known || surface == nil {
				i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
				return
			}
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

			w.Header().Add("Vary", GrantHeader)
			w.Header().Set("Cache-Control", "private, no-store")

			ctx := WithGrant(r.Context(), g)
			ctx = handler.SetUser(ctx, nil)
			ctx = handler.SetTenant(ctx, nil)
			ctx = tenant.SetTenantID(ctx, "")

			if !g.Expires.IsZero() {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, g.Expires)
				defer cancel()
			}

			ctx, hasUser, err := h.installSubject(ctx, g.Subject)
			if err != nil {
				i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
				return
			}
			if g.Subject != "" && (h.resolve != nil || h.resolveTenant != nil) {
				subject := g.Subject
				ctx = handler.WithPrincipalCheck(ctx, func(c context.Context) (context.Context, bool) {
					fresh, stillUser, err := h.installSubject(c, subject)
					return fresh, err == nil && stillUser == hasUser
				})
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

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

func (h *Host) installSubject(ctx context.Context, subject string) (context.Context, bool, error) {
	ctx = handler.SetUser(ctx, nil)
	ctx = handler.SetTenant(ctx, nil)
	ctx = tenant.SetTenantID(ctx, "")
	if subject == "" {
		return ctx, false, nil
	}
	if h.resolveTenant != nil {
		tid, err := h.resolveTenant(ctx, subject)
		if err != nil {
			return ctx, false, err
		}
		if tid != "" {
			ctx = handler.SetTenant(ctx, tid)
			ctx = tenant.SetTenantID(ctx, tid)
		}
	}
	if h.resolve == nil {
		return ctx, false, nil
	}
	user, err := h.resolve(ctx, subject)
	if err != nil {
		return ctx, false, err
	}
	if IsNilValue(user) {
		return ctx, false, nil
	}
	return handler.SetUser(ctx, user), true, nil
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
