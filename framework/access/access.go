package access

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/fuzzy"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Permission is an action permission string, e.g. "posts:read".
type Permission string

// RoleWithOrigin is an effective role and where it came from (a direct
// assignment, an organization membership).
type RoleWithOrigin struct {
	Role   string
	Origin string
}

// Policy reports whether the subject in ctx holds a permission.
type Policy interface {
	Can(ctx context.Context, permission Permission) bool
}

// RolePolicy is a Policy over role grants, safe for concurrent use.
type RolePolicy struct {
	mu                 sync.RWMutex
	rolePermissions    map[string][]Permission
	capabilities       map[Permission]struct{}
	strictCapabilities bool
}

// NewRolePolicy returns an empty RolePolicy.
func NewRolePolicy() *RolePolicy {
	return &RolePolicy{
		rolePermissions: make(map[string][]Permission),
		capabilities:    make(map[Permission]struct{}),
	}
}

// Register adds capabilities to the known-capability registry. Idempotent.
func (rp *RolePolicy) Register(capabilities ...Permission) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	for _, capability := range capabilities {
		rp.capabilities[capability] = struct{}{}
	}
}

// Capabilities returns a sorted copy of the registered capabilities.
func (rp *RolePolicy) Capabilities() []Permission {
	rp.mu.RLock()
	capabilities := make([]Permission, 0, len(rp.capabilities))
	for capability := range rp.capabilities {
		capabilities = append(capabilities, capability)
	}
	rp.mu.RUnlock()
	slices.Sort(capabilities)
	return capabilities
}

// Snapshot returns a deep copy of the role→permissions map.
func (rp *RolePolicy) Snapshot() map[string][]Permission {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	out := make(map[string][]Permission, len(rp.rolePermissions))
	for role, perms := range rp.rolePermissions {
		cp := make([]Permission, len(perms))
		copy(cp, perms)
		out[role] = cp
	}
	return out
}

// StrictCapabilities makes a grant of an unknown capability an error instead
// of a warning, and returns rp.
func (rp *RolePolicy) StrictCapabilities() *RolePolicy {
	rp.mu.Lock()
	rp.strictCapabilities = true
	rp.mu.Unlock()
	return rp
}

// Grant adds permissions to role. With registered capabilities, "res:*"
// expands to every registered "res:" capability. An unknown capability warns,
// or errors with *UnknownCapabilityError on a strict policy.
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
	slices.Sort(registered)

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

// prepareRevokes expands like prepareGrants but keeps each raw permission and
// passes unknown ones through.
func (rp *RolePolicy) prepareRevokes(permissions []Permission) []Permission {
	rp.mu.RLock()
	registered := make([]Permission, 0, len(rp.capabilities))
	for capability := range rp.capabilities {
		registered = append(registered, capability)
	}
	rp.mu.RUnlock()
	slices.Sort(registered)

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
		appendPrepared(permission)
		if permission == Wildcard {
			continue
		}
		if raw := string(permission); strings.HasSuffix(raw, ":*") {
			prefix := strings.TrimSuffix(raw, "*")
			for _, capability := range registered {
				if strings.HasPrefix(string(capability), prefix) {
					appendPrepared(capability)
				}
			}
		}
	}
	return prepared
}

// UnknownCapabilityError is a strict-mode grant of an unregistered capability;
// Nearest is the closest registered one, or empty.
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
	best := fuzzy.Levenshtein(string(grant), string(nearest))
	for _, candidate := range registered[1:] {
		distance := fuzzy.Levenshtein(string(grant), string(candidate))
		if distance < best {
			nearest = candidate
			best = distance
		}
	}
	return nearest
}

// Revoke removes permissions from role, expanding "res:*" like Grant and also
// removing the raw literal. Revoking an unheld permission is a no-op.
func (rp *RolePolicy) Revoke(role string, permissions ...Permission) {
	prepared := rp.prepareRevokes(permissions)
	rp.mu.Lock()
	defer rp.mu.Unlock()
	existing, ok := rp.rolePermissions[role]
	if !ok {
		return
	}
	revokeSet := make(map[Permission]bool, len(prepared))
	for _, p := range prepared {
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

// ReplaceRole sets role's permissions to exactly perms, prepared as Grant
// prepares them. On a strict-mode error the role is unchanged.
func (rp *RolePolicy) ReplaceRole(role string, perms ...Permission) error {
	prepared, err := rp.prepareGrants(perms)
	if err != nil {
		return err
	}
	rp.replacePrepared(role, prepared)
	return nil
}

func (rp *RolePolicy) replacePrepared(role string, prepared []Permission) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	out := make([]Permission, len(prepared))
	copy(out, prepared)
	rp.rolePermissions[role] = out
}

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

// Roles returns the sorted roles that hold at least one permission.
func (rp *RolePolicy) Roles() []string {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	return slices.Sorted(maps.Keys(rp.rolePermissions))
}

// PermissionsOf returns a copy of role's permissions, or nil.
func (rp *RolePolicy) PermissionsOf(role string) []Permission {
	return rp.permissionsFor(role)
}

// Wildcard is the superuser permission: a role granted "*" passes every check.
// Grant it only to separately gated surfaces, never to an end-user role.
const Wildcard Permission = "*"

// Can reports whether any of the ctx's roles holds permission or Wildcard.
func (rp *RolePolicy) Can(ctx context.Context, permission Permission) bool {
	perms := GetPermissions(ctx)
	for _, p := range perms {
		if p == permission || p == Wildcard {
			return true
		}
	}
	return false
}

// Can reports whether ctx's policy and roles grant permission and, for a
// scope-restricted caller, a held scope allows it. No policy or a nil ctx is false.
func Can(ctx context.Context, permission Permission) bool {
	policy := PolicyFromContext(ctx)
	if policy == nil {
		return observe(ctx, permission, false, "can")
	}
	return observe(ctx, permission, ScopeAllows(ctx, permission) && policy.Can(ctx, permission), "can")
}

// GetPermissions returns the permissions ctx's roles hold under its policy, or
// nil when ctx, the policy or the roles are missing.
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

// RequirePermission returns middleware that answers 403 unless the caller's
// scopes allow permission and the Decider, or on abstain the role policy, grants it.
func RequirePermission(permission Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			granted := ScopeAllows(ctx, permission) && requireResource(ctx, permission, Ref{})
			if !observe(ctx, permission, granted, "require-permission") {
				handler.WriteError(w, r, handler.Fail(http.StatusForbidden, handler.ReasonMissingPermission, map[string]any{"permission": string(permission)}))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Middleware installs policy and the roles resolved by roles (nil installs only
// the policy) into the request context, re-resolving them on a principal recheck.
func Middleware(policy *RolePolicy, roles func(ctx context.Context) []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithPolicy(r.Context(), policy)
			if roles != nil {
				ctx = WithRoles(ctx, roles(ctx))
				ctx = handler.AddPrincipalCheck(ctx, func(c context.Context) (context.Context, bool) {
					return WithRoles(c, roles(c)), true
				})
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

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

// GetRoles returns the roles WithRoles installed, or nil.
func GetRoles(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	roles, _ := ctx.Value(rolesKey{}).([]string)
	return roles
}

// PolicyFromContext returns the policy WithPolicy installed, or nil.
func PolicyFromContext(ctx context.Context) *RolePolicy {
	if ctx == nil {
		return nil
	}
	policy, _ := ctx.Value(policyKey{}).(*RolePolicy)
	return policy
}
