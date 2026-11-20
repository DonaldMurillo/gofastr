package auth

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// RequireAuth validates the Bearer JWT and puts its owner, re-read from the user store, in the context; it panics on a nil jwt.
func RequireAuth(jwt *JWTAuth) middleware.Middleware {
	if jwt == nil {
		panic("auth: RequireAuth needs a *JWTAuth; mgr.JWT() is nil when AuthConfig.JWTSecret is empty")
	}
	if jwt.onResolve != nil {
		jwt.onResolve()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractBearerToken(r)
			if tokenStr == "" {
				if u, ok := handler.GetUser(r.Context()); ok && u != nil {
					if _, embedded := embed.GrantFromContext(r.Context()); embedded || framework.IsInProcessRequest(r) {
						next.ServeHTTP(w, r)
						return
					}
				}
				handler.WriteError(w, r, handler.Fail(http.StatusUnauthorized, reasonBadAuthorization))
				return
			}

			claims, err := jwt.ValidateToken(tokenStr)
			if err != nil {
				handler.WriteError(w, r, handler.Fail(http.StatusUnauthorized, reasonInvalidToken))
				return
			}

			user, ok := jwt.resolveOwner(r.Context(), claims)
			if !ok {
				handler.WriteError(w, r, handler.Fail(http.StatusUnauthorized, reasonInvalidToken))
				return
			}
			ctx := handler.SetUser(r.Context(), user)
			ctx = handler.WithPrincipalCheck(ctx, jwtRecheck(jwt, tokenStr))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func jwtRecheck(jwt *JWTAuth, tokenStr string) handler.PrincipalCheck {
	return func(ctx context.Context) (context.Context, bool) {
		claims, err := jwt.ValidateToken(tokenStr)
		if err != nil {
			return ctx, false
		}
		user, ok := jwt.resolveOwner(ctx, claims)
		if !ok {
			return ctx, false
		}
		return handler.SetUser(ctx, user), true
	}
}

// RequireRole refuses a caller holding none of roles, an embed grant, or one the context Decider denies.
func RequireRole(roles ...string) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetCurrentUser(r.Context())
			if user == nil {
				handler.WriteError(w, r, handler.Fail(http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized)))
				return
			}

			if _, embedded := embed.GrantFromContext(r.Context()); embedded {
				handler.WriteError(w, r, handler.Fail(http.StatusForbidden, reasonEmbedRoleGated))
				return
			}

			if roleGateDenied(r.Context(), user) {
				handler.WriteError(w, r, handler.Fail(http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden)))
				return
			}

			if !hasAnyRole(r.Context(), user, roles) {
				handler.WriteError(w, r, handler.Fail(http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden)))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// GetCurrentUser extracts the authenticated User from the context.
func GetCurrentUser(ctx context.Context) User {
	raw, ok := handler.GetUser(ctx)
	if !ok || raw == nil {
		return nil
	}
	u, ok := raw.(User)
	if !ok {
		return nil
	}
	return u
}

func roleGateDenied(ctx context.Context, user User) bool {
	d := access.GetDecider(ctx)
	if d == nil {
		return false
	}
	return d(ctx, user.GetRoles(), access.RoleGateCapability, access.Ref{}) == access.DecisionDeny
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func hasAnyRole(ctx context.Context, user User, targetRoles []string) bool {
	userRoles := user.GetRoles()
	for _, target := range targetRoles {
		if slices.Contains(userRoles, target) && access.ScopeAllows(ctx, access.RoleScope(target)) {
			return true
		}
	}
	return false
}
