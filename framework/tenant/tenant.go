// Package tenant scopes rows to the request's tenant: the tenant on the context, its middleware and WithMultiTenant.
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

type crossTenantKey struct{}

// AllowCrossTenant marks ctx as reading every tenant; set it only server-side on admin routes you gate yourself.
func AllowCrossTenant(ctx context.Context) context.Context {
	return context.WithValue(ctx, crossTenantKey{}, true)
}

// IsCrossTenant reports whether ctx was marked by AllowCrossTenant.
func IsCrossTenant(ctx context.Context) bool {
	v, _ := ctx.Value(crossTenantKey{}).(bool)
	return v
}

// TenantConfig configures multi-tenancy behavior for an entity.
type TenantConfig struct {
	// Field is the tenant column, "tenant_id" by default.
	Field string
}

// DefaultTenantConfig returns the tenant_id column configuration.
func DefaultTenantConfig() TenantConfig {
	return TenantConfig{
		Field: "tenant_id",
	}
}

// WithMultiTenant makes ent multi-tenant on config.Field, declaring that column hidden and read-only if absent; it panics on an unsafe name.
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
	ent.Config.Fields = append(slices.Clone(ent.Config.Fields), entity.TenantColumnField(col))
	return ent
}

func isInjectedTenantField(f schema.Field) bool {
	return reflect.DeepEqual(f, entity.TenantColumnField(f.Name))
}

// ApplyTenantFilter scopes builder to tenantID on tenant_id, and to no rows when tenantID is empty.
func ApplyTenantFilter(builder *query.QueryBuilder, tenantID string) {
	if tenantID == "" {
		builder.Where("tenant_id IS NULL AND 1=0")
		return
	}
	builder.Where("tenant_id = $1", tenantID)
}

// TenantMiddleware sets the tenant from handler.GetTenant, never from a client header; header is ignored.
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

// ResolveMiddleware sets the tenant resolve returns, and again on every handler.RecheckPrincipal; mount it after auth.
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

// SetTenantID stores the tenant ID in core/handler's context slot.
func SetTenantID(ctx context.Context, id string) context.Context {
	return handler.SetTenantID(ctx, id)
}

// GetTenantID returns the context's tenant ID, or "".
func GetTenantID(ctx context.Context) string {
	return handler.GetTenantID(ctx)
}

// InjectTenantID sets data's tenant_id to the context's tenant, or deletes it when there is none.
func InjectTenantID(data map[string]any, ctx context.Context) {
	tenantID := GetTenantID(ctx)
	if tenantID == "" {
		delete(data, "tenant_id")
		return
	}
	data["tenant_id"] = tenantID
}
