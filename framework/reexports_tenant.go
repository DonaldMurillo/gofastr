package framework

import (
	"context"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"net/http"
)

// Re-exports of framework/tenant so callers using framework.X (benchmarks,
// example apps) keep compiling after the tenant package extraction.

type TenantConfig = tenant.TenantConfig

// DefaultTenantConfig wraps tenant.DefaultTenantConfig.
func DefaultTenantConfig() tenant.TenantConfig { return tenant.DefaultTenantConfig() }

// WithMultiTenant wraps tenant.WithMultiTenant.
func WithMultiTenant(ent *entity.Entity, config tenant.TenantConfig) *entity.Entity {
	return tenant.WithMultiTenant(ent, config)
}

// ApplyTenantFilter wraps tenant.ApplyTenantFilter.
func ApplyTenantFilter(builder *query.QueryBuilder, tenantID string) {
	tenant.ApplyTenantFilter(builder, tenantID)
}

// TenantMiddleware wraps tenant.TenantMiddleware.
func TenantMiddleware(header string) func(http.Handler) http.Handler {
	return tenant.TenantMiddleware(header)
}

// SetTenantID wraps tenant.SetTenantID.
func SetTenantID(ctx context.Context, id string) context.Context { return tenant.SetTenantID(ctx, id) }

// GetTenantID wraps tenant.GetTenantID.
func GetTenantID(ctx context.Context) string { return tenant.GetTenantID(ctx) }

// InjectTenantID wraps tenant.InjectTenantID.
func InjectTenantID(data map[string]any, ctx context.Context) { tenant.InjectTenantID(data, ctx) }
