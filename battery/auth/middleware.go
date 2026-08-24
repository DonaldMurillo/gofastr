package auth

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// RequireAuth returns middleware that validates a Bearer JWT token
// and stores the authenticated user in the request context.
func RequireAuth(jwt *JWTAuth) middleware.Middleware {
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
				http.Error(w, `{"error":{"code":401,"message":"missing or invalid authorization header"}}`, http.StatusUnauthorized)
				return
			}

			claims, err := jwt.ValidateToken(tokenStr)
			if err != nil {
				http.Error(w, `{"error":{"code":401,"message":"invalid or expired token"}}`, http.StatusUnauthorized)
				return
			}

			user := claimsToUser(claims)
			ctx := handler.SetUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole returns middleware that checks if the authenticated user
// has at least one of the required roles.
func RequireRole(roles ...string) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetCurrentUser(r.Context())
			if user == nil {
				http.Error(w, `{"error":{"code":401,"message":"unauthorized"}}`, http.StatusUnauthorized)
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
				http.Error(w, `{"error":{"code":403,"message":"role-gated routes are not reachable from an embedded surface"}}`, http.StatusForbidden)
				return
			}

			if !hasAnyRole(user, roles) {
				http.Error(w, `{"error":{"code":403,"message":"forbidden"}}`, http.StatusForbidden)
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

// hasAnyRole checks if the user has at least one of the target roles.
func hasAnyRole(user User, targetRoles []string) bool {
	userRoles := user.GetRoles()
	for _, target := range targetRoles {
		if slices.Contains(userRoles, target) {
			return true
		}
	}
	return false
}
