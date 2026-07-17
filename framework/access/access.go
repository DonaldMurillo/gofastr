package access

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Permission represents an action permission string (e.g. "posts:read", "posts:write").
type Permission string

// RoleWithOrigin identifies an effective role and where it came from, such as
// a direct user assignment or a resolved organization membership.
type RoleWithOrigin struct {
	Role   string
	Origin string
}

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
	mu                 sync.RWMutex
	rolePermissions    map[string][]Permission
	capabilities       map[Permission]struct{}
	strictCapabilities bool
}

// NewRolePolicy creates a new empty RolePolicy.
func NewRolePolicy() *RolePolicy {
	return &RolePolicy{
		rolePermissions: make(map[string][]Permission),
		capabilities:    make(map[Permission]struct{}),
	}
}

// Register adds capabilities to the policy's known capability registry.
// Registration is idempotent and safe to call concurrently with grants and
// permission checks.
func (rp *RolePolicy) Register(capabilities ...Permission) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	for _, capability := range capabilities {
		rp.capabilities[capability] = struct{}{}
	}
}

// Capabilities returns the policy's registered capabilities in sorted order.
// The returned slice is a defensive copy.
func (rp *RolePolicy) Capabilities() []Permission {
	rp.mu.RLock()
	capabilities := make([]Permission, 0, len(rp.capabilities))
	for capability := range rp.capabilities {
		capabilities = append(capabilities, capability)
	}
	rp.mu.RUnlock()
	sort.Slice(capabilities, func(i, j int) bool {
		return capabilities[i] < capabilities[j]
	})
	return capabilities
}

// StrictCapabilities makes unknown capability grants fail instead of warning.
// It returns the policy so callers can opt in while constructing it.
func (rp *RolePolicy) StrictCapabilities() *RolePolicy {
	rp.mu.Lock()
	rp.strictCapabilities = true
	rp.mu.Unlock()
	return rp
}

// Grant adds permissions to a role. Duplicate grants are ignored.
//
// With a non-empty capability registry, resource wildcards such as "teams:*"
// expand to all registered capabilities with that prefix. Unknown grants warn
// by default and remain accepted for backward compatibility; a strict policy
// rejects them and returns an error.
func (rp *RolePolicy) Grant(role string, permissions ...Permission) error {
	prepared, err := rp.prepareGrants(permissions)
	if err != nil {
		return err
	}
	rp.grantPrepared(role, prepared)
	return nil
}

func (rp *RolePolicy) prepareGrants(permissions []Permission) ([]Permission, error) {
	rp.mu.RLock()
	registered := make([]Permission, 0, len(rp.capabilities))
	for capability := range rp.capabilities {
		registered = append(registered, capability)
	}
	strict := rp.strictCapabilities
	rp.mu.RUnlock()
	sort.Slice(registered, func(i, j int) bool {
		return registered[i] < registered[j]
	})

	registeredSet := make(map[Permission]struct{}, len(registered))
	for _, capability := range registered {
		registeredSet[capability] = struct{}{}
	}

	prepared := make([]Permission, 0, len(permissions))
	seen := make(map[Permission]struct{}, len(permissions))
	appendPrepared := func(permission Permission) {
		if _, ok := seen[permission]; ok {
			return
		}
		seen[permission] = struct{}{}
		prepared = append(prepared, permission)
	}

	for _, permission := range permissions {
		if permission == Wildcard {
			appendPrepared(permission)
			continue
		}

		raw := string(permission)
		if len(registered) == 0 {
			if strings.Contains(raw, "*") {
				if err := handleUnknownCapability(permission, "", strict); err != nil {
					return nil, err
				}
			}
			appendPrepared(permission)
			continue
		}

		if strings.HasSuffix(raw, ":*") {
			prefix := strings.TrimSuffix(raw, "*")
			matched := false
			for _, capability := range registered {
				if strings.HasPrefix(string(capability), prefix) {
					appendPrepared(capability)
					matched = true
				}
			}
			if matched {
				continue
			}
		}

		if _, ok := registeredSet[permission]; !ok {
			nearest := nearestCapability(permission, registered)
			if err := handleUnknownCapability(permission, nearest, strict); err != nil {
				return nil, err
			}
		}
		appendPrepared(permission)
	}
	return prepared, nil
}

func (rp *RolePolicy) grantPrepared(role string, permissions []Permission) {
	if len(permissions) == 0 {
		return
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()

	existing := rp.rolePermissions[role]
	seen := make(map[Permission]struct{}, len(existing)+len(permissions))
	for _, permission := range existing {
		seen[permission] = struct{}{}
	}
	for _, permission := range permissions {
		if _, ok := seen[permission]; ok {
			continue
		}
		seen[permission] = struct{}{}
		existing = append(existing, permission)
	}
	rp.rolePermissions[role] = existing
}

// UnknownCapabilityError reports a strict-mode grant of a capability that is
// not in the policy's registry. Handlers can errors.As on it to present the
// message as a caller mistake (e.g. HTTP 400) rather than a server fault.
type UnknownCapabilityError struct {
	Grant   Permission
	Nearest Permission
}

func (e *UnknownCapabilityError) Error() string {
	if e.Nearest == "" {
		return fmt.Sprintf("access: capability grant %q will never match; register concrete capabilities before granting a resource wildcard", e.Grant)
	}
	return fmt.Sprintf("access: capability grant %q will never match; nearest registered capability is %q", e.Grant, e.Nearest)
}

func handleUnknownCapability(grant, nearest Permission, strict bool) error {
	if strict {
		return &UnknownCapabilityError{Grant: grant, Nearest: nearest}
	}
	attrs := []any{slog.String("grant", string(grant))}
	if nearest != "" {
		attrs = append(attrs, slog.String("nearest", string(nearest)))
	}
	slog.Warn("access: capability grant will never match a registered capability", attrs...)
	return nil
}

func nearestCapability(grant Permission, registered []Permission) Permission {
	if len(registered) == 0 {
		return ""
	}
	nearest := registered[0]
	best := stringDistance(string(grant), string(nearest))
	for _, candidate := range registered[1:] {
		distance := stringDistance(string(grant), string(candidate))
		if distance < best {
			nearest = candidate
			best = distance
		}
	}
	return nearest
}

func stringDistance(a, b string) int {
	if len(a) < len(b) {
		a, b = b, a
	}
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for i := range previous {
		previous[i] = i
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			current[j] = min(
				current[j-1]+1,
				previous[j]+1,
				previous[j-1]+cost,
			)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
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

// Roles returns the sorted list of all roles that currently have at least
// one granted permission. The slice is a defensive copy — callers can
// iterate it without holding the lock. Intended for admin UIs that need
// to enumerate the grant matrix; not used on the hot Can path.
func (rp *RolePolicy) Roles() []string {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	roles := make([]string, 0, len(rp.rolePermissions))
	for r := range rp.rolePermissions {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	return roles
}

// PermissionsOf returns a defensive copy of the permissions granted to
// the given role. Returns nil when the role has no grants. Callers
// iterate the returned slice without holding the lock so a concurrent
// Grant/Revoke can't mutate it under them.
func (rp *RolePolicy) PermissionsOf(role string) []Permission {
	return rp.permissionsFor(role)
}

// Wildcard is the superuser permission: a role granted "*" passes every
// permission check. Grant it deliberately and only to fully-trusted,
// separately-gated surfaces (e.g. the admin back-office, which has its own
// Authorize gate) — never to an end-user role.
const Wildcard Permission = "*"

// Can checks if the user from ctx has the given permission via any of their
// roles. A role holding the Wildcard permission ("*") passes any check.
func (rp *RolePolicy) Can(ctx context.Context, permission Permission) bool {
	perms := GetPermissions(ctx)
	for _, p := range perms {
		if p == permission || p == Wildcard {
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

// GetRoles reads back the roles installed via WithRoles (by
// access.Middleware or battery/auth). It is the reader half of the
// role-context seam — without it, role context is one-way (you can put
// roles in but not read them out), which blocks role-based UI branching
// (e.g. "show the admin nav only when the caller holds 'admin'").
//
// Returns nil when ctx is nil or carries no roles — never panics. A nil
// context is treated as an anonymous request.
func GetRoles(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	roles, _ := ctx.Value(rolesKey{}).([]string)
	return roles
}
