# Multi-tenant scoping

Marking an entity multi-tenant adds a `tenant_id` column, auto-injects
it on writes, filters reads by it, and scopes lifecycle events to it.
The tenant ID is extracted from a request header by middleware.

## Quickstart

```go
app.Entity("posts", framework.EntityConfig{
    MultiTenant: true,
    Fields: []schema.Field{
        {Name: "title", Type: schema.String, Required: true},
    },
})

// Default header is X-Tenant-ID. Add the middleware once on the app:
app.Use(framework.TenantMiddleware("X-Tenant-ID"))
```

Once both are wired:

- `INSERT` auto-populates `tenant_id` from the request context.
- `SELECT` adds `WHERE tenant_id = $tenant`.
- `UPDATE` / `DELETE` cannot touch rows owned by other tenants.
- The SSE `_events` stream filters to the requester's tenant.

## How tenants get into the request

```go
func TenantMiddleware(header string) func(http.Handler) http.Handler
```

The middleware reads the named header on every request and, if
non-empty, attaches the value to the request context. Downstream
handlers and hooks call `framework.GetTenantID(ctx)` to read it.

If your tenants come from JWT claims, a subdomain, or anywhere else,
write your own middleware that calls `framework.SetTenantID(ctx, id)`
— the framework only cares that the context value is set.

## Configuration

`TenantConfig` and `DefaultTenantConfig()` exist for the case where
you need to customise the column or header name. The defaults are:

| Field        | Default       |
|--------------|---------------|
| `Field`      | `tenant_id`   |
| `Header`     | `X-Tenant-ID` |
| `AutoScope`  | `true`        |

`AutoScope=false` lets you read across tenants from admin routes
while still writing scoped — handy for support/admin tooling.

## Helpers

- `framework.SetTenantID(ctx, id)` — stash a tenant on context.
- `framework.GetTenantID(ctx)` — read it back; returns `""` when not
  set.
- `framework.InjectTenantID(data, ctx)` — set `data["tenant_id"]`
  from context. Used internally on writes; expose-yourself helper for
  custom endpoints that bypass the auto path.
- `framework.ApplyTenantFilter(qb, tenantID)` — add
  `WHERE tenant_id = $1` to a query builder.

## Cross-tenant access

An empty tenant ID disables filtering. Two patterns:

1. **Admin user with no tenant context** — `TenantMiddleware` only
   sets the value when the header is present. Admin routes call
   without the header to see across tenants.
2. **Explicit `WithoutTenant`** — write your own middleware that
   inspects user roles and clears the tenant context for cross-tenant
   reads.

There is no built-in role check linking permissions to tenant scope.
Compose with `RequirePermission` to gate cross-tenant access on the
right role.

## Schema implications

When you declare `MultiTenant: true`, `framework.AutoMigrate` adds the
column. If you migrated the table manually, add:

```sql
ALTER TABLE posts
ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
CREATE INDEX posts_tenant_idx ON posts (tenant_id);
```

The index is recommended on any table that filters by tenant_id on
every read.

## Common mistakes

- **Adding `MultiTenant: true` to an existing entity without
  backfilling `tenant_id`.** Existing rows have empty string and
  match every tenant. Backfill before flipping the flag.
- **Setting the tenant from a request body field.** Trivially
  spoofable. Use a signed header, JWT claim, or session lookup.
- **Forgetting `TenantMiddleware`.** Auto-scope only fires when the
  context has a tenant — without the middleware, every request looks
  like cross-tenant access.
- **Cross-tenant joins via `?include=`.** If both parent and child
  are multi-tenant, includes scope on the parent's tenant only.
  Non-multi-tenant child entities are returned unfiltered — model
  carefully.
