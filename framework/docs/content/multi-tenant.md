# Multi-tenancy

GoFastr supports shared-database, column-scoped multi-tenancy: every query, create, update, delete, include, and event stream on a multi-tenant entity is automatically scoped to the active tenant on the request context.

## Enabling multi-tenancy

1. **Resolve the tenant per request** with `tenant.ResolveMiddleware` (recommended — derives and verifies the tenant from the authenticated session or membership) or `framework.TenantMiddleware("X-Tenant-ID")` (only behind a trusted proxy that strips and sets the header):

```go
app.Use(tenant.ResolveMiddleware(func(ctx context.Context) string {
	return verifiedTenantForUser(ctx)
}))
```

2. **Mark each tenant-scoped entity** with `Scope.MultiTenant: true`:

```go
app.Entity("projects", framework.EntityConfig{
	Fields: []schema.Field{
		{Name: "name", Type: schema.String, Required: true},
	},
	Scope: &framework.ScopeConfig{
		MultiTenant: true,
	},
})
```

You can combine `MultiTenant: true` with `Scope.OwnerField` (per-user rows inside a tenant) and `Scope.SoftDelete: true`.

## What multi-tenancy enforces

- **Schema:** GoFastr adds the tenant ID column (`tenant_id` by default, override via `Scope.TenantField`). Add composite indexes starting with `tenant_id` yourself for query performance.
- **Create:** stamps `tenant_id` from the request context, ignoring any client-supplied value.
- **Read / List / Update / Delete / Batch / Transitions:** appends `WHERE tenant_id = <active_tenant>` so cross-tenant IDs return `404`.
- **Includes & relation filters:** `?include=` and `?rel.field=` enforce the target entity's tenant scope too.
- **Fail-closed:** a request without a resolved tenant in context is rejected (`401`/`403`) rather than falling back to an unscoped query.
- **EntityUI & Admin forms:** tenant columns are server-owned and never rendered as editable form inputs.

## Background jobs and cross-tenant admin tasks

Background workers and CLI jobs do not have an HTTP request. Pass the target tenant explicitly in the job payload and attach it with `framework.SetTenantID(ctx, tenantID)` before querying multi-tenant entities. For trusted server-side operational reports that intentionally span tenants, wrap the context with `tenant.AllowCrossTenant(ctx)`.

## Common mistakes

- **Trusting a client-supplied `X-Tenant-ID` header without checking membership:** always verify the authenticated user actually belongs to the requested tenant before putting it in context.
- **Forgetting to index `(tenant_id, ...)` on high-traffic entities:** include `tenant_id` as the leading column in composite indexes and unique constraints (for example unique `(tenant_id, slug)` rather than global `slug`).
- **Running background jobs without attaching a tenant to `ctx`:** queries against multi-tenant entities fail closed when no tenant is present in context.
- **Confusing `MultiTenant` with `OwnerField`:** `MultiTenant` isolates organizations/workspaces; `OwnerField` isolates individual users. Use both when a workspace has private per-user records.
