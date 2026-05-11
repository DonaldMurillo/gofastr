package access

import (
	"context"
	"net/http"

	"github.com/gofastr/gofastr/core/handler"
)

// Permission represents an action permission string (e.g. "posts:read", "posts:write").
type Permission string

// Policy determines whether a subject can perform an action on a resource.
type Policy interface {
	Can(ctx context.Context, permission Permission, resource any) bool
}

// RolePolicy implements Policy using role-based permission grants.
type RolePolicy struct {
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
	rp.rolePermissions[role] = append(rp.rolePermissions[role], permissions...)
}

// Revoke removes specific permissions from a role.
func (rp *RolePolicy) Revoke(role string, permissions ...Permission) {
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

// Can checks if the user from ctx has the given permission via any of their roles.
func (rp *RolePolicy) Can(ctx context.Context, permission Permission, resource any) bool {
	perms := GetPermissions(ctx)
	for _, p := range perms {
		if p == permission {
			return true
		}
	}
	return false
}

// GetPermissions extracts the user's permissions from context by looking up
// the user's roles against the RolePolicy.
func GetPermissions(ctx context.Context) []Permission {
	// We store the policy and roles in context; extract them here
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
		for _, p := range policy.rolePermissions[role] {
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
			if policy == nil || !policy.Can(ctx, permission, nil) {
				herr := handler.Errorf(http.StatusForbidden, "access denied: missing permission %s", permission)
				handler.WriteError(w, herr)
				return
			}
			next.ServeHTTP(w, r)
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
