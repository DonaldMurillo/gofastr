# Access control

GoFastr's access control is permission-based with role-based grants.
The framework gives you the building blocks; wiring permissions to
users is your responsibility (typically in an auth middleware).

## Quickstart

```go
policy := framework.NewRolePolicy()
policy.Grant("admin",  "posts:read", "posts:write", "posts:delete")
policy.Grant("editor", "posts:read", "posts:write")
policy.Grant("reader", "posts:read")

app.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx := framework.WithPolicy(r.Context(), policy)
        ctx  = framework.WithRoles(ctx, currentUserRoles(r))
        next.ServeHTTP(w, r.WithContext(ctx))
    })
})

app.Router.With(framework.RequirePermission("posts:write")).
    Post("/posts", postsHandler)
```

The policy + roles wiring above is common enough that there's a one-liner
for it — `framework.AccessMiddleware`:

```go
app.Use(framework.AccessMiddleware(policy, currentUserRoles))
// currentUserRoles has signature func(ctx context.Context) []string
```

## Gating auto-CRUD (`EntityConfig.Access`)

`RequirePermission` gates routes you mount yourself. To gate the
**auto-generated CRUD** for an entity, declare the permission for each
operation on the entity config:

```go
app.Entity("posts", framework.EntityConfig{
    Access: framework.AccessControl{
        Read:   "posts:read",   // List + Get
        Create: "posts:write",
        Update: "posts:write",
        Delete: "posts:delete",
    },
})
```

Each blank field leaves that operation un-gated by RBAC (owner and tenant
scoping still apply). When a field is set, auto-CRUD refuses a request
whose context lacks the permission with **403** — on List, Get, Create,
Update, Delete, the batch/stream variants, and the `_events` SSE feed. The
roles + policy must be in the request context first; mount
`framework.AccessMiddleware` (above) ahead of the CRUD routes.

> **Scope: HTTP only.** `EntityConfig.Access` gates the HTTP CRUD surface.
> The **in-process** APIs — `CrudHandler.CreateOne/UpdateOne/DeleteOne/
> GetOne/ListAll/UpsertOne` and the generated typed repo (`Repo.Query()…`)
> — are trusted Go code you call yourself; they enforce **owner and tenant
> scope** (tenant fail-closed) but **not** per-op permissions. Apply your
> own authorization before calling them from a handler. (Tenant isolation
> is a hard boundary and is enforced everywhere; per-op RBAC is an
> HTTP-request concept.)

> Before this existed, exposing an entity granted **every authenticated
> user full CRUD** unless you hand-composed route-group middleware.
> `EntityConfig.Access` makes the requirement visible at the declaration
> and enforced by default.

## Concepts

- **Permission** — opaque string. By convention `"<resource>:<verb>"`
  (`"posts:read"`, `"users:delete"`). The framework does not enforce
  the format.
- **Role** — string key that holds a list of permissions.
- **Policy** — maps role → permissions. `RolePolicy` is the shipped
  implementation; the `Policy` interface lets you swap in your own.

The framework never asks who the user *is* — only what permissions
their context carries. Get the roles into context however you want:
JWT claims, session cookie, API key lookup.

## API

### Building a policy

```go
p := framework.NewRolePolicy()
p.Grant("admin", "users:read", "users:write")
p.Revoke("admin", "users:write")
```

`Grant` is additive; `Revoke` removes specific permissions. Roles
with no grants are valid but match no permission.

### Attaching to a request

```go
ctx = framework.WithPolicy(ctx, policy)
ctx = framework.WithRoles(ctx, []string{"editor", "reader"})
```

Both calls are required. Without them, every permission check
denies — fail-closed.

### Checking from a handler

```go
perms := framework.GetPermissions(ctx)
// [posts:read posts:write …]
```

Or via middleware on a specific route:

```go
app.Router.With(framework.RequirePermission("posts:delete")).
    Delete("/posts/{id}", postsHandler)
```

`RequirePermission` returns `403 access denied: missing permission X`
when the user does not hold the named permission. The error format
is JSON via `core/handler.WriteError`.

## The `Policy` interface

```go
type Policy interface {
    Can(ctx context.Context, permission Permission, resource any) bool
}
```

Implement this to plug in:

- Database-backed permission lookups.
- Resource-level checks (`"can user X update post Y?"`).
- External authorisation services (OPA, etc.).

The `resource` argument is passed through unchanged so resource-aware
policies can inspect it. `RolePolicy` ignores it.

## Where to apply checks

Two patterns, both supported:

1. **Per-route middleware** — `RequirePermission` is one line per
   route, easy to audit, but disconnects the permission name from
   the entity declaration.
2. **In a `BeforeCreate` / `BeforeUpdate` hook** — closer to the
   data, can inspect the patch, can deny per-record. More code; use
   when row-level checks matter.

The framework does **not** auto-generate permission strings from
entity declarations. Pick a convention (`posts:read`, `posts:write`,
…) and apply it consistently.

## Common mistakes

- **Forgetting `WithPolicy`.** Every check fails closed. If
  `RequirePermission` denies everyone, this is usually why.
- **Granting permissions on the wrong policy instance.** `RolePolicy`
  is mutable; if you grant on one instance and put a different
  instance into context, checks pass for the in-context one and
  ignore the granted one.
- **Encoding business logic in permission strings.** Keep them
  resource:verb. Express logic in `Policy` implementations or
  hooks — strings should be data, not code.
- **Trusting client-supplied roles.** Roles come from your auth
  layer; never from a request header or body the user controls.
