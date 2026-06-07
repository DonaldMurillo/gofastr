package access

import (
	"context"
	"net/http"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Permission represents an action permission string (e.g. "posts:read", "posts:write").
type Permission string

// Policy determines whether the subject in ctx holds a permission.
type Policy interface {
	Can(ctx context.Context, permission Permission) bool
}

// RolePolicy implements Policy using role-based permission grants.
//
// Grant and Revoke may be called concurrently with Can / GetPermissions:
// the underlying role→permissions map is guarded by an RWMutex so reads
// don't block each other and writes won't trigger Go's concurrent-map
// fatal.
type RolePolicy struct {
	mu              sync.RWMutex
	rolePermissions map[string][]Permission
}

// NewRolePolicy creates a new empty RolePolicy.
func NewRolePolicy() *RolePolicy {
	return &RolePolicy{
		rolePermissions: make(map[string][]Permission),
	}
}

// Grant adds permissions to a role.
func (rp *RolePolicy) Grant(role string, permissions ...Permission) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.rolePermissions[role] = append(rp.rolePermissions[role], permissions...)
}

// Revoke removes specific permissions from a role.
func (rp *RolePolicy) Revoke(role string, permissions ...Permission) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	existing, ok := rp.rolePermissions[role]
	if !ok {
		return
	}
	revokeSet := make(map[Permission]bool, len(permissions))
	for _, p := range permissions {
		revokeSet[p] = true
	}
	filtered := existing[:0]
	for _, p := range existing {
		if !revokeSet[p] {
			filtered = append(filtered, p)
		}
	}
	rp.rolePermissions[role] = filtered
}

// permissionsFor returns a defensive snapshot of permissions for the
// given role. Callers iterate the returned slice without holding the
// lock so a concurrent Grant/Revoke can't mutate it under them.
func (rp *RolePolicy) permissionsFor(role string) []Permission {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	src := rp.rolePermissions[role]
	if len(src) == 0 {
		return nil
	}
	out := make([]Permission, len(src))
	copy(out, src)
	return out
}

// Can checks if the user from ctx has the given permission via any of their roles.
func (rp *RolePolicy) Can(ctx context.Context, permission Permission) bool {
	perms := GetPermissions(ctx)
	for _, p := range perms {
		if p == permission {
			return true
		}
	}
	return false
}

// Can reports whether the request context carries the given permission. It
// reads the RolePolicy and roles installed via WithPolicy / WithRoles (by
// access.Middleware or battery/auth). Returns false when no policy is present
// — the secure-by-default answer for an un-wired request. This is the seam the
// CRUD layer uses to enforce EntityConfig.Access.
func Can(ctx context.Context, permission Permission) bool {
	policy, _ := ctx.Value(policyKey{}).(*RolePolicy)
	if policy == nil {
		return false
	}
	return policy.Can(ctx, permission)
}

// GetPermissions extracts the user's permissions from context by looking up
// the user's roles against the RolePolicy.
//
// Returns nil if ctx is nil, missing a policy, or missing roles — never
// panics. A nil context is treated as an anonymous request rather than
// allowed to crash the handler.
func GetPermissions(ctx context.Context) []Permission {
	if ctx == nil {
		return nil
	}
	policy, _ := ctx.Value(policyKey{}).(*RolePolicy)
	if policy == nil {
		return nil
	}
	roles, _ := ctx.Value(rolesKey{}).([]string)
	if len(roles) == 0 {
		return nil
	}
	seen := make(map[Permission]bool)
	var result []Permission
	for _, role := range roles {
		for _, p := range policy.permissionsFor(role) {
			if !seen[p] {
				seen[p] = true
				result = append(result, p)
			}
		}
	}
	return result
}

// RequirePermission returns HTTP middleware that checks if the current user
// has the specified permission. Returns 403 if denied.
func RequirePermission(permission Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			policy, _ := ctx.Value(policyKey{}).(*RolePolicy)
			if policy == nil || !policy.Can(ctx, permission) {
				herr := handler.Errorf(http.StatusForbidden, "access denied: missing permission %s", permission)
				handler.WriteError(w, herr)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Middleware installs the RBAC policy and the request's roles into the context
// so downstream RequirePermission middleware and auto-CRUD permission gates
// (EntityConfig.Access) can resolve permissions. roles maps a request context
// to the caller's roles — typically by reading the authenticated user; pass
// nil to install only the policy (roles resolved elsewhere). Mount this once,
// app-wide or on a route group, ahead of any permission-gated routes.
func Middleware(policy *RolePolicy, roles func(ctx context.Context) []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithPolicy(r.Context(), policy)
			if roles != nil {
				ctx = WithRoles(ctx, roles(ctx))
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// --- Context helpers for roles and policy ---

type policyKey struct{}
type rolesKey struct{}

// WithPolicy stores a RolePolicy in the context.
func WithPolicy(ctx context.Context, policy *RolePolicy) context.Context {
	return context.WithValue(ctx, policyKey{}, policy)
}

// WithRoles stores user roles in the context.
func WithRoles(ctx context.Context, roles []string) context.Context {
	return context.WithValue(ctx, rolesKey{}, roles)
}
