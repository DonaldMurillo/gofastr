package framework

import "github.com/gofastr/gofastr/framework/access"

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
)
