package framework

import (
	"context"
	"net/http"

	"github.com/gofastr/gofastr/core/handler"
	"github.com/gofastr/gofastr/core/query"
	"github.com/gofastr/gofastr/framework/entity"
)

// tenantIDKey is the context key for the string tenant identifier
// used by the framework-level tenant helpers.
type tenantIDKey struct{}

// TenantConfig configures multi-tenancy behavior for an entity.
type TenantConfig struct {
	// Field is the database column name used for tenant scoping.
	// Defaults to "tenant_id".
	Field string

	// Header is the HTTP header name from which the tenant ID is extracted.
	// Defaults to "X-Tenant-ID".
	Header string

	// AutoScope, when true, automatically adds tenant filtering to all queries.
	AutoScope bool
}

// DefaultTenantConfig returns a TenantConfig with sensible defaults.
func DefaultTenantConfig() TenantConfig {
	return TenantConfig{
		Field:     "tenant_id",
		Header:    "X-Tenant-ID",
		AutoScope: true,
	}
}

// WithMultiTenant configures an entity for multi-tenancy.
// Sets the MultiTenant flag on the entity config and stores the tenant config
// in the entity metadata.
func WithMultiTenant(ent *entity.Entity, config TenantConfig) *entity.Entity {
	ent.Config.MultiTenant = true
	return ent
}

// ApplyTenantFilter adds a WHERE tenant_id = ? clause to the query builder.
// The tenantID is parameterized to prevent SQL injection.
// If tenantID is empty, no filter is applied (admin/cross-tenant access).
func ApplyTenantFilter(builder *query.QueryBuilder, tenantID string) {
	if tenantID != "" {
		builder.Where("tenant_id = $1", tenantID)
	}
}

// TenantMiddleware returns an HTTP middleware that extracts the tenant ID from
// the specified header and stores it in the request context.
// If the header is present and non-empty, the tenant ID is available via
// GetTenantID.
func TenantMiddleware(header string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := r.Header.Get(header)
			if tenantID != "" {
				ctx := SetTenantID(r.Context(), tenantID)
				// Also set via handler package for cross-package compatibility
				ctx = handler.SetTenant(ctx, tenantID)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SetTenantID stores the tenant ID string in the context.
func SetTenantID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, tenantIDKey{}, id)
}

// GetTenantID retrieves the tenant ID string from the context.
// Returns an empty string if no tenant ID is set.
func GetTenantID(ctx context.Context) string {
	id, _ := ctx.Value(tenantIDKey{}).(string)
	return id
}

// InjectTenantID automatically injects the tenant_id field into a data map
// before insert/update operations. This ensures records are associated with
// the current tenant from the context.
func InjectTenantID(data map[string]any, ctx context.Context) {
	tenantID := GetTenantID(ctx)
	if tenantID != "" {
		data["tenant_id"] = tenantID
	}
}
