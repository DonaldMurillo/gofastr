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

## Custom tenant column

The tenant column defaults to `tenant_id`. To use a different name, set
`TenantField` on the entity — it's the single source of the column name
across injection, auto-migrate, and the CRUD insert/scope/filter paths:

```go
app.Entity("docs", framework.EntityConfig{
    MultiTenant: true,
    TenantField: "org_id",   // injected, created, written, and scoped by this column
    Fields:      []schema.Field{{Name: "title", Type: schema.String}},
})
```

A `TenantField` that isn't a valid SQL identifier fails loud at
definition time. If you configure tenancy via `tenant.WithMultiTenant(ent,
TenantConfig{Field: "org_id"})`, the `Field` flows into `TenantField`
automatically.

> The standalone helpers `framework.ApplyTenantFilter` / `InjectTenantID`
> always use the default `tenant_id` column (they have no entity context).
> A custom `TenantField` is honored by the automatic CRUD scoping, which is
> the path you normally use — reach for the standalone helpers only with the
> default column.

## Configuration

`TenantConfig` / `DefaultTenantConfig()` carry the header name and
`AutoScope`. The defaults are:

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

**Secure by default (since the tenant-gate change).** A `MultiTenant`
entity served over HTTP with **no tenant id in the request context is
refused with `401`** — on every operation (list, get, create, update,
delete, batch, stream, SSE) and through the in-process CRUD API. A
missing tenant context can no longer silently return or mutate every
tenant's rows.

> **BREAKING:** earlier versions failed *open* — an empty tenant id
> disabled filtering and returned all tenants' rows. If you relied on
> "admin routes just omit the header to see across tenants", you must
> now opt in explicitly (below). Auto-scope by tenant id is unchanged
> for the normal request path.

To read or write across tenants deliberately (admin tooling), mark the
context with `tenant.AllowCrossTenant` — **server-side only**, never
from a client-controlled header:

```go
// inside an admin-gated handler / middleware, AFTER your own role check:
ctx := tenant.AllowCrossTenant(r.Context())
r = r.WithContext(ctx)
// CRUD on this request now spans every tenant (scope helpers no-op on
// the empty tenant id) instead of being refused with 401.
```

There is no built-in role check linking permissions to tenant scope.
`AllowCrossTenant` only lifts the tenant *requirement*; you must still
gate the route with `RequirePermission` (or equivalent) so only the
right role can reach it. Creating a row while cross-tenant with no
tenant id is still refused — there's no tenant to stamp it with.

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
  context has a tenant — without the middleware every request now gets a
  `401` (secure by default), not silent cross-tenant access. Mount the
  middleware, or set `tenant.AllowCrossTenant` deliberately on admin
  routes.
- **Cross-tenant joins via `?include=`.** If both parent and child
  are multi-tenant, includes scope on the parent's tenant only.
  Non-multi-tenant child entities are returned unfiltered — model
  carefully.
