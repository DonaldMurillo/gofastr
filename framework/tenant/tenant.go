package tenant

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// crossTenantKey marks a context as deliberately cross-tenant. It must ONLY
// ever be set server-side (admin routes, background jobs), never derived from
// a client-supplied header, or it becomes a tenant-isolation bypass.
type crossTenantKey struct{}

// AllowCrossTenant marks ctx as permitted to operate without a tenant id.
// Multi-tenant CRUD reads then span every tenant (the scope helpers no-op on
// an empty tenant id) and the secure-by-default RequireTenant gate stops
// refusing the request.
//
// SECURITY: use ONLY on deliberately-admin routes, and gate those routes with
// your own permission check, there is no built-in role check. Never set this
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

	// Header is a legacy field retained for API compatibility. It is NOT
	// read anywhere, the tenant ID is resolved exclusively from the
	// server-side authenticated context (never a client-sent header), so
	// setting this changes no behavior. See TenantMiddleware.
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
//
// The entity has already been through entity.Define, which adds the tenant
// column to the fields of an entity declared multi-tenant, so this adds it
// the same way: a hidden, read-only String. Without it AutoMigrate created
// no tenant column and the first create failed on the column CRUD writes.
// The tenant_id column Define injected for a MultiTenant entity is renamed
// to a custom Field rather than kept beside it.
func WithMultiTenant(ent *entity.Entity, config TenantConfig) *entity.Entity {
	if ent.Config.Scope == nil {
		ent.Config.Scope = &entity.ScopeConfig{}
	}
	ent.Config.Scope.MultiTenant = true
	if config.Field != "" && config.Field != "tenant_id" {
		ent.Config.Scope.TenantField = config.Field
	}
	col := ent.Config.TenantColumn()
	if _, err := query.SafeIdent(col); err != nil {
		panic(fmt.Sprintf("entity %q: TenantField %q is not a valid SQL identifier: %v", ent.Config.Name, col, err))
	}
	injected := -1
	for i, f := range ent.Config.Fields {
		if f.Name == col {
			return ent
		}
		if f.Name == "tenant_id" && isInjectedTenantField(f) {
			injected = i
		}
	}
	if injected >= 0 {
		fields := slices.Clone(ent.Config.Fields)
		fields[injected].Name = col
		ent.Config.Fields = fields
		return ent
	}
	ent.Config.Fields = append(slices.Clone(ent.Config.Fields), schema.Field{
		Name:         col,
		Type:         schema.String,
		AutoGenerate: schema.AutoNone,
		ReadOnly:     true,
		Hidden:       true,
	})
	return ent
}

// isInjectedTenantField reports whether f is the tenant column entity.Define
// adds, and nothing an app declared itself.
func isInjectedTenantField(f schema.Field) bool {
	return reflect.DeepEqual(f, schema.Field{
		Name:         f.Name,
		Type:         schema.String,
		AutoGenerate: schema.AutoNone,
		ReadOnly:     true,
		Hidden:       true,
	})
}

// ApplyTenantFilter adds a WHERE tenant_id = ? clause to the query
// builder. The tenantID is parameterized to prevent SQL injection. An
// empty tenantID is FAIL-CLOSED, the query is scoped to a guaranteed-
// empty result set ("WHERE 1 = 0") rather than being left unscoped. Apps
// that genuinely want cross-tenant queries (admin tooling) must construct
// them with the tenant filter disabled deliberately, not by handing in
// an empty string here.
//
// This standalone helper always uses the default "tenant_id" column. For an
// entity with a custom EntityConfig.TenantField, the CRUD auto-scope
// (CrudHandler.ApplyTenantScope*) already filters by the right column, use
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
// SECURITY: the middleware does NOT trust any client-sent header. Doing
// so would let any caller impersonate any tenant by setting an HTTP
// header. The tenant comes exclusively from the server-resolved
// authenticated context (handler.GetTenant, seeded by the auth layer).
// Hosts that need a different resolution strategy (subdomain, JWT
// claim, etc.) should compose their own middleware and call SetTenantID
// directly.
//
// The `header` parameter is IGNORED, it is retained only so existing
// call sites keep compiling from the era when the header was consulted.
// Passing a header name here does not (and must not) make the
// middleware read it.
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

// ResolveMiddleware resolves the request's tenant with resolve, which reads
// server-side state for the authenticated principal (a membership lookup, a
// session field) and returns "" when the caller belongs to no tenant. Mount
// it after the auth middleware.
//
// A request that stays open, the CRUD event stream in particular, runs
// resolve again for the refreshed principal on every handler.RecheckPrincipal,
// so a member removed from a tenant, or moved to another, stops receiving
// the old tenant's data at the next check. A middleware that calls SetTenantID
// itself installs no such re-check: the stream keeps the tenant it read at
// connect.
func ResolveMiddleware(resolve func(ctx context.Context) string) func(http.Handler) http.Handler {
	if resolve == nil {
		panic("tenant.ResolveMiddleware: nil resolve")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := SetTenantID(r.Context(), resolve(r.Context()))
			ctx = handler.AddPrincipalCheck(ctx, func(c context.Context) (context.Context, bool) {
				return SetTenantID(c, resolve(c)), true
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SetTenantID stores the tenant ID string in the context. The slot is
// core/handler's (handler.SetTenantID), so packages below this one, such as
// access.CachedResolver, can read the same tenant.
func SetTenantID(ctx context.Context, id string) context.Context {
	return handler.SetTenantID(ctx, id)
}

// GetTenantID retrieves the tenant ID string from the context.
// Returns an empty string if no tenant ID is set.
func GetTenantID(ctx context.Context) string {
	return handler.GetTenantID(ctx)
}

// InjectTenantID automatically injects the tenant_id field into a data map
// before insert/update operations. This ensures records are associated with
// the current tenant from the context. Like ApplyTenantFilter, it uses the
// default "tenant_id" column; entities with a custom EntityConfig.TenantField
// are handled by the CRUD layer (CrudHandler.InjectTenant), not this helper.
//
// The resolved tenant always wins over a body-supplied value, and with
// no tenant in ctx the body's value is removed rather than kept: a
// caller must never be able to name the row's tenant when resolution
// failed.
func InjectTenantID(data map[string]any, ctx context.Context) {
	tenantID := GetTenantID(ctx)
	if tenantID == "" {
		delete(data, "tenant_id")
		return
	}
	data["tenant_id"] = tenantID
}
