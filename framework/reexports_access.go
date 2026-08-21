package framework

import (
	"context"
	"database/sql"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"net/http"
	"time"
)

// Re-exports of framework/access so callers using framework.X (benchmarks,
// example apps) keep compiling after the access package extraction.

type (
	Permission = access.Permission
	Policy     = access.Policy
	RolePolicy = access.RolePolicy
	GrantStore = access.GrantStore
	// UnknownCapabilityError is a strict-mode Grant rejection; errors.As on
	// it to answer 400 (caller typo) instead of 500 (store failure).
	UnknownCapabilityError = access.UnknownCapabilityError
	// CachedResolver wraps a roles resolver with per-user TTL caching.
	CachedResolver = access.CachedResolver
	// RoleWithOrigin labels an effective role with where it came from
	// ("direct" vs "resolved") for the admin users screen.
	RoleWithOrigin = access.RoleWithOrigin
	// Ref, Decision and Decider form the resource-scoped decision seam:
	// "member can edit project 42", consulted before the role policy.
	Ref      = access.Ref
	Decision = access.Decision
	Decider  = access.Decider
)

const (
	DecisionAbstain = access.DecisionAbstain
	DecisionAllow   = access.DecisionAllow
	DecisionDeny    = access.DecisionDeny
)

// NewRolePolicy wraps access.NewRolePolicy.
func NewRolePolicy() *access.RolePolicy { return access.NewRolePolicy() }

// NewGrantStore wraps access.NewGrantStore.
func NewGrantStore(db *sql.DB, policy *access.RolePolicy, opts ...access.GrantStoreOption) *access.GrantStore {
	return access.NewGrantStore(db, policy, opts...)
}

// RequirePermission wraps access.RequirePermission.
func RequirePermission(permission access.Permission) func(http.Handler) http.Handler {
	return access.RequirePermission(permission)
}

// GetPermissions wraps access.GetPermissions.
func GetPermissions(ctx context.Context) []access.Permission { return access.GetPermissions(ctx) }

// WithPolicy wraps access.WithPolicy.
func WithPolicy(ctx context.Context, policy *access.RolePolicy) context.Context {
	return access.WithPolicy(ctx, policy)
}

// WithRoles wraps access.WithRoles.
func WithRoles(ctx context.Context, roles []string) context.Context {
	return access.WithRoles(ctx, roles)
}

// GetRoles reads the roles installed via WithRoles back out of the
// request context, the reader half of the role-context seam, for
// role-based UI branching.
func GetRoles(ctx context.Context) []string { return access.GetRoles(ctx) }

// Can reports whether the request context carries a permission.
func Can(ctx context.Context, permission access.Permission) bool { return access.Can(ctx, permission) }

// AccessMiddleware installs the RBAC policy + roles into request context
// so RequirePermission and EntityConfig.Access gates can resolve.
func AccessMiddleware(policy *access.RolePolicy, roles func(ctx context.Context) []string) func(http.Handler) http.Handler {
	return access.Middleware(policy, roles)
}

// NewCachedResolver wraps a func(ctx) []string roles resolver with
// per-user TTL caching + Invalidate; pair with AccessMiddleware.
func NewCachedResolver(resolve func(context.Context) []string, opts ...access.CachedResolverOption) *access.CachedResolver {
	return access.NewCachedResolver(resolve, opts...)
}

// WithResolverTTL configures NewCachedResolver's cache TTL.
func WithResolverTTL(ttl time.Duration) access.CachedResolverOption { return access.WithTTL(ttl) }

// CanResource is Can with a resource in hand: it consults the Decider
// installed via DeciderMiddleware/WithDecider, falling back to Can.
func CanResource(ctx context.Context, capability access.Permission, resource access.Ref) bool {
	return access.CanResource(ctx, capability, resource)
}

// WithDecider / GetDecider install and read the resource-decision seam
// on a context; DeciderMiddleware does it per-request after
// AccessMiddleware.
func WithDecider(ctx context.Context, d access.Decider) context.Context {
	return access.WithDecider(ctx, d)
}

// GetDecider wraps access.GetDecider.
func GetDecider(ctx context.Context) access.Decider { return access.GetDecider(ctx) }

// DeciderMiddleware wraps access.DeciderMiddleware.
func DeciderMiddleware(d access.Decider) func(http.Handler) http.Handler {
	return access.DeciderMiddleware(d)
}
