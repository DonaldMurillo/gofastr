package framework

import "github.com/DonaldMurillo/gofastr/framework/tenant"

// Re-exports of framework/tenant so callers using framework.X (benchmarks,
// example apps) keep compiling after the tenant package extraction.

type TenantConfig = tenant.TenantConfig

var (
	DefaultTenantConfig = tenant.DefaultTenantConfig
	WithMultiTenant     = tenant.WithMultiTenant
	ApplyTenantFilter   = tenant.ApplyTenantFilter
	TenantMiddleware    = tenant.TenantMiddleware
	SetTenantID         = tenant.SetTenantID
	GetTenantID         = tenant.GetTenantID
	InjectTenantID      = tenant.InjectTenantID
)
