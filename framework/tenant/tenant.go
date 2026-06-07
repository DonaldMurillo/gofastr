package tenant

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// tenantIDKey is the context key for the string tenant identifier
// used by the framework-level tenant helpers.
type tenantIDKey struct{}

// crossTenantKey marks a context as deliberately cross-tenant. It must ONLY
// ever be set server-side (admin routes, background jobs) — never derived from
// a client-supplied header — or it becomes a tenant-isolation bypass.
type crossTenantKey struct{}

// AllowCrossTenant marks ctx as permitted to operate without a tenant id.
// Multi-tenant CRUD reads then span every tenant (the scope helpers no-op on
// an empty tenant id) and the secure-by-default RequireTenant gate stops
// refusing the request.
//
// SECURITY: use ONLY on deliberately-admin routes, and gate those routes with
// your own permission check — there is no built-in role check. Never set this
// from a request header or any other client-controlled value.
func AllowCrossTenant(ctx context.Context) context.Context {
	return context.WithValue(ctx, crossTenantKey{}, true)
}

// IsCrossTenant reports whether ctx was explicitly marked for cross-tenant
// access via AllowCrossTenant.
func IsCrossTenant(ctx context.Context) bool {
	v, _ := ctx.Value(crossTenantKey{}).(bool)
	return v
}

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

// WithMultiTenant configures an entity for multi-tenancy. It honors a custom
// tenant column: TenantConfig.Field flows into EntityConfig.TenantField, the
// single source of the column name used by entity injection, auto-migrate, and
// the CRUD insert/scope/filter paths. A blank or default "tenant_id" leaves
// TenantField unset (the default applies).
func WithMultiTenant(ent *entity.Entity, config TenantConfig) *entity.Entity {
	ent.Config.MultiTenant = true
	if config.Field != "" && config.Field != "tenant_id" {
		ent.Config.TenantField = config.Field
	}
	return ent
}

// ApplyTenantFilter adds a WHERE tenant_id = ? clause to the query
// builder. The tenantID is parameterized to prevent SQL injection. An
// empty tenantID is FAIL-CLOSED — the query is scoped to a guaranteed-
// empty result set ("WHERE 1 = 0") rather than being left unscoped. Apps
// that genuinely want cross-tenant queries (admin tooling) must construct
// them with the tenant filter disabled deliberately, not by handing in
// an empty string here.
//
// This standalone helper always uses the default "tenant_id" column. For an
// entity with a custom EntityConfig.TenantField, the CRUD auto-scope
// (CrudHandler.ApplyTenantScope*) already filters by the right column — use
// that rather than this helper, which has no entity context.
func ApplyTenantFilter(builder *query.QueryBuilder, tenantID string) {
	if tenantID == "" {
		// Fail-closed: a missing tenant scopes the query to a tenant
		// that can never match a real row. Mentions tenant_id explicitly
		// so a casual reader sees the scope even though the comparison
		// can never be true.
		builder.Where("tenant_id IS NULL AND 1=0")
		return
	}
	builder.Where("tenant_id = $1", tenantID)
}

// TenantMiddleware returns an HTTP middleware that resolves the tenant
// ID for the request from server-side state.
//
// SECURITY: the middleware does NOT trust the raw `header` value sent by
// the client. Doing so would let any caller impersonate any tenant by
// setting an HTTP header. Instead, the header is treated as a *hint* and
// the middleware looks up a server-resolved tenant for the
// authenticated user (via handler.GetTenant). Hosts that need a different
// resolution strategy (subdomain, JWT claim, etc.) should compose their
// own middleware and call SetTenantID directly.
//
// The legacy `header` parameter is retained for API compatibility but
// only consulted when the resolved tenant matches it — preventing
// header-only privilege escalation.
func TenantMiddleware(header string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if t, ok := handler.GetTenant(r.Context()); ok {
				if tenantID, ok := t.(string); ok && tenantID != "" {
					r = r.WithContext(SetTenantID(r.Context(), tenantID))
				}
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
// the current tenant from the context. Like ApplyTenantFilter, it uses the
// default "tenant_id" column; entities with a custom EntityConfig.TenantField
// are handled by the CRUD layer (CrudHandler.InjectTenant), not this helper.
func InjectTenantID(data map[string]any, ctx context.Context) {
	tenantID := GetTenantID(ctx)
	if tenantID != "" {
		data["tenant_id"] = tenantID
	}
}
