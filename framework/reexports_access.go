package framework

import "github.com/DonaldMurillo/gofastr/framework/access"

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
	// Ref, Decision and Decider form the resource-scoped decision seam —
	// "member can edit project 42" — consulted before the role policy.
	Ref      = access.Ref
	Decision = access.Decision
	Decider  = access.Decider
)

const (
	DecisionAbstain = access.DecisionAbstain
	DecisionAllow   = access.DecisionAllow
	DecisionDeny    = access.DecisionDeny
)

var (
	NewRolePolicy     = access.NewRolePolicy
	NewGrantStore     = access.NewGrantStore
	RequirePermission = access.RequirePermission
	GetPermissions    = access.GetPermissions
	WithPolicy        = access.WithPolicy
	WithRoles         = access.WithRoles
	// GetRoles reads the roles installed via WithRoles back out of the
	// request context — the reader half of the role-context seam, for
	// role-based UI branching.
	GetRoles = access.GetRoles
	// Can reports whether the request context carries a permission.
	Can = access.Can
	// AccessMiddleware installs the RBAC policy + roles into request context
	// so RequirePermission and EntityConfig.Access gates can resolve.
	AccessMiddleware = access.Middleware
	// NewCachedResolver wraps a func(ctx) []string roles resolver with
	// per-user TTL caching + Invalidate; pair with AccessMiddleware.
	NewCachedResolver = access.NewCachedResolver
	// WithResolverTTL configures NewCachedResolver's cache TTL.
	WithResolverTTL = access.WithTTL
	// CanResource is Can with a resource in hand: it consults the Decider
	// installed via DeciderMiddleware/WithDecider, falling back to Can.
	CanResource = access.CanResource
	// WithDecider / GetDecider install and read the resource-decision seam
	// on a context; DeciderMiddleware does it per-request after
	// AccessMiddleware.
	WithDecider       = access.WithDecider
	GetDecider        = access.GetDecider
	DeciderMiddleware = access.DeciderMiddleware
)
