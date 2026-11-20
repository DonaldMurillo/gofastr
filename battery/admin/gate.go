package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

func (b *Battery) askPolicy(name string, ask func() bool) (ok bool) {
	defer func() {
		if rec := recover(); rec != nil {
			b.logger().Error("admin: policy callback panicked", "callback", name, "panic", fmt.Sprintf("%T", rec))
			ok = false
		}
	}()
	return ask()
}

// AdminScope is the API-token scope a token-authenticated request needs to
// pass the admin gate.
const AdminScope = "admin:access"

func (b *Battery) authorized(ctx context.Context) bool {
	if _, embedded := embed.GrantFromContext(ctx); embedded {
		return false
	}
	if held, tokenAuthed := auth.TokenScopes(ctx); tokenAuthed && !auth.ScopeMatch(held, AdminScope) {
		return false
	}
	if d := access.GetDecider(ctx); d != nil {
		if d(ctx, access.GetRoles(ctx), access.Wildcard, access.Ref{}) == access.DecisionDeny {
			return false
		}
	}
	if b.cfg.Authorize != nil {
		return b.askPolicy("Authorize", func() bool { return b.cfg.Authorize(ctx) })
	}
	u, ok := handler.GetUser(ctx)
	if !ok || u == nil {
		return false
	}
	rh, ok := u.(interface{ GetRoles() []string })
	if !ok {
		return false
	}
	return slices.Contains(rh.GetRoles(), b.adminRole())
}

func (b *Battery) admitJob(ctx context.Context) context.Context {
	if b.cfg.Policy != nil && access.PolicyFromContext(ctx) == nil {
		ctx = access.WithPolicy(ctx, b.cfg.Policy)
	}
	if b.authorized(ctx) {
		return b.elevate(ctx)
	}
	return ctx
}

func (b *Battery) elevate(ctx context.Context) context.Context {
	return crud.WithElevation(ctx, b.names...)
}

func (b *Battery) adminRole() string {
	if b.cfg.AdminRole != "" {
		return b.cfg.AdminRole
	}
	return "admin"
}

func (b *Battery) authzStatus(ctx context.Context) int {
	if u, ok := handler.GetUser(ctx); ok && u != nil {
		return http.StatusForbidden
	}
	return http.StatusUnauthorized
}

func (b *Battery) routes() *router.Router {
	return b.router.Group("", func(next http.Handler) http.Handler { return b.gate(next) })
}

func (b *Battery) headers(next http.Handler) http.Handler {
	secure := middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !b.underPrefix(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		secure.ServeHTTP(w, r)
	})
}

func (b *Battery) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isSafeAdminMethod(r.Method) && rejectCrossSiteForm(w, r) {
			return
		}
		if !b.authorized(r.Context()) {
			status := b.authzStatus(r.Context())
			if status == http.StatusUnauthorized && b.cfg.LoginPath != "" && r.Method == http.MethodGet {
				handler.NoteSessionRefusal(r.Context())
				http.Redirect(w, r, b.cfg.LoginPath+"?next="+url.QueryEscape(loginNext(r)), http.StatusSeeOther)
				return
			}
			i18n.HTTPError(w, r, status, i18n.StatusPhrase(status))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loginNext(r *http.Request) string {
	next := r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		next += "?" + r.URL.RawQuery
	}
	return next
}

func isSafeAdminMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}
