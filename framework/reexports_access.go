package framework

import "github.com/DonaldMurillo/gofastr/framework/access"

// Re-exports of framework/access so callers using framework.X (benchmarks,
// example apps) keep compiling after the access package extraction.

type (
	Permission = access.Permission
	Policy     = access.Policy
	RolePolicy = access.RolePolicy
)

var (
	NewRolePolicy     = access.NewRolePolicy
	RequirePermission = access.RequirePermission
	GetPermissions    = access.GetPermissions
	WithPolicy        = access.WithPolicy
	WithRoles         = access.WithRoles
	// Can reports whether the request context carries a permission.
	Can = access.Can
	// AccessMiddleware installs the RBAC policy + roles into request context
	// so RequirePermission and EntityConfig.Access gates can resolve.
	AccessMiddleware = access.Middleware
)
