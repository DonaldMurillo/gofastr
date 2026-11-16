package framework

import (
	"context"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"net/http"
)

// Root spellings of framework/tenant.

type TenantConfig = tenant.TenantConfig

func DefaultTenantConfig() tenant.TenantConfig { return tenant.DefaultTenantConfig() }

func WithMultiTenant(ent *entity.Entity, config tenant.TenantConfig) *entity.Entity {
	return tenant.WithMultiTenant(ent, config)
}

func ApplyTenantFilter(builder *query.QueryBuilder, tenantID string) {
	tenant.ApplyTenantFilter(builder, tenantID)
}

func TenantMiddleware(header string) func(http.Handler) http.Handler {
	return tenant.TenantMiddleware(header)
}

func SetTenantID(ctx context.Context, id string) context.Context { return tenant.SetTenantID(ctx, id) }

func GetTenantID(ctx context.Context) string { return tenant.GetTenantID(ctx) }

func InjectTenantID(data map[string]any, ctx context.Context) { tenant.InjectTenantID(data, ctx) }
