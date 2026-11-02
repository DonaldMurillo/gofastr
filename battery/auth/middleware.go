package auth

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// RequireAuth returns middleware that validates a Bearer JWT token
// and stores the authenticated user in the request context.
//
// When the JWTAuth carries a user store (every manager-built one does;
// AuthManager.Init wires it, see JWTAuth.SetUserStore), the token's
// subject is re-resolved on EVERY request and identity AND roles come
// from the fresh row — deletion, erasure, and role downgrades take
// effect on the next request, not at token TTL. A missing owner row
// answers 401. A bare NewJWTAuth with no store keeps the stateless
// claims-derived principal (service-to-server deployments with no user
// table).
//
// RequireAuth panics on a nil jwt: mgr.JWT() is nil only when the manager has
// no JWTSecret, and the middleware would otherwise panic on the first bearer
// request instead of at wiring time.
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
				// An embed request has no bearer token and cannot have one:
				// embeds.Middleware() deletes Authorization precisely so no
				// second credential can compete with the grant it just verified.
				// Demanding one here 401'd every embed request on any route
				// using JWT auth, under the middleware order the embed docs
				// prescribe. A grant already carries a verified subject, so the
				// requirement this middleware exists to enforce is met.
				// Only when the grant actually resolved to a user. A surface
				// with no Resolve, the documented "public pricing table",
				// handed to every anonymous visitor, produces a grant whose
				// subject is nobody, and passing that through would hand every
				// handler behind RequireAuth a request with no user while
				// implying one was verified.
				if u, ok := handler.GetUser(r.Context()); ok && u != nil {
					if _, embedded := embed.GrantFromContext(r.Context()); embedded {
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

			// A valid signature is necessary, not sufficient: the
			// principal comes from the user store's fresh row (resolveOwner),
			// so a deleted owner or a downgraded role set fails closed on
			// the very next request instead of outliving the token TTL.
			// Same 401 body as an invalid token: which of the two failed
			// is not an observable distinction worth an oracle for.
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

// jwtRecheck re-validates the token (signature and expiry) and re-resolves
// its owner for a request that stays open (an _events stream), so expiry or
// deleting the owner ends the stream. See handler.WithPrincipalCheck.
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

// RequireRole returns middleware that checks if the authenticated user
// has at least one of the required roles. A Decider installed in the
// request context (access.WithDecider / DeciderMiddleware) binds this
// gate too: DecisionDeny refuses the caller before the role check,
// DecisionAbstain falls through to it, the same precedence CanResource
// gives resource-scoped gates.
func RequireRole(roles ...string) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetCurrentUser(r.Context())
			if user == nil {
				handler.WriteError(w, r, handler.Fail(http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized)))
				return
			}

			// A role is something the SUBJECT holds; a grant is delegated,
			// scoped authority handed to a third party's page. Letting a grant
			// inherit the subject's roles means a credential minted for a
			// read-only reporting surface satisfies RequireRole("admin")
			// whenever its subject happens to be one, which is the whole
			// reason the surface declared scopes in the first place.
			//
			// Use embeds.RequireScope for what an embed may reach; roles gate
			// interactive callers.
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
// Returns nil if no user is present.
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

// roleGateDenied consults the Decider installed in ctx (access.WithDecider
// / DeciderMiddleware) with the caller's roles and the zero Ref — a role
// gate holds no resource. DecisionDeny refuses (fail closed before the
// role check, the precedence CanResource gives resource gates);
// DecisionAbstain and a missing decider fall through to the role policy.
func roleGateDenied(ctx context.Context, user User) bool {
	d := access.GetDecider(ctx)
	if d == nil {
		return false
	}
	return d(ctx, user.GetRoles(), access.RoleGateCapability, access.Ref{}) == access.DecisionDeny
}

// extractBearerToken extracts the token from the Authorization header.
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

// hasAnyRole checks if the user has at least one of the target roles. A
// scope-restricted caller (an API token) carries its owner's roles, so a role
// counts for it only when it also holds access.RoleScope of that role
// ("role:admin", "role:*" or "*:*"), the rule framework.MCPRequireRole
// follows. Without it a token an admin minted for "customers:read" would
// pass every gate that asks for "admin". Sessions and JWTs carry no scopes.
func hasAnyRole(ctx context.Context, user User, targetRoles []string) bool {
	userRoles := user.GetRoles()
	for _, target := range targetRoles {
		if slices.Contains(userRoles, target) && access.ScopeAllows(ctx, access.RoleScope(target)) {
			return true
		}
	}
	return false
}
