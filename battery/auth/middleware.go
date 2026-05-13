package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// RequireAuth returns middleware that validates a Bearer JWT token
// and stores the authenticated user in the request context.
func RequireAuth(jwt *JWTAuth) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractBearerToken(r)
			if tokenStr == "" {
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
		for _, role := range userRoles {
			if role == target {
				return true
			}
		}
	}
	return false
}
