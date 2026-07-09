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

app.Router().Post("/posts",
    framework.RequirePermission("posts:write")(http.HandlerFunc(postsHandler)))
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

The generated OpenAPI spec (`/openapi.json`) advertises **401** (authentication
required) and **403** (authenticated but forbidden) on every operation of an
RBAC-gated entity — including the `_batch` and `_events` endpoints. This means
generated SDKs and agents see the correct error contract instead of treating
RBAC-gated routes as public.

The spec also declares **how** callers authenticate. When any entity is
auth-gated (owner-scoped, multi-tenant, or RBAC), the spec includes
`components.securitySchemes` with two schemes a gated operation accepts:
`bearerAuth` (HTTP bearer, JWT) and `cookieAuth` (the auth battery's session
cookie). Each gated operation then carries a per-operation `security` block
listing both — meaning either scheme authorises the call. Ungated entities
are left unmarked, so clients and codegen treat them as publicly reachable.
Auth is per-operation, not global: the spec never sets a top-level `security`
requirement.

The `cookieAuth` name is the auth battery's production default
(`__Host-session`, set in `battery/auth` `AuthConfig.defaults()`); `DevMode`
flips it to `session_id`. If your deployment overrides
`AuthConfig.SessionCookie`, overwrite the scheme after building the spec —
`Spec.SetSecurityScheme("cookieAuth", …)` replaces it by name.

> **Scope: HTTP only.** `EntityConfig.Access` gates the HTTP CRUD surface.
> The **in-process** APIs — `CrudHandler.CreateOne/UpdateOne/DeleteOne/
> GetOne/ListAll/UpsertOne` and the generated typed repo (`Repo.Query()…`)
> — are trusted Go code you call yourself; they enforce **owner and tenant
> scope** (tenant fail-closed) but **not** per-op permissions. Apply your
> own authorization before calling them from a handler. (Tenant isolation
> is a hard boundary and is enforced everywhere; per-op RBAC is an
> HTTP-request concept.) For a deliberate cross-owner read (an aggregate
> or admin lookup that spans every owner's rows), wrap the context in
> `owner.AllowCrossOwner(ctx)` — server-side Go only; the HTTP CRUD
> endpoints have no path to it. See [entity-declarations](entity-declarations.md)
> → "Reading across owners".

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
JWT claims, session cookie, API key lookup. **Service accounts** (see
[Authentication → Service accounts & API tokens](auth.md#service-accounts--scoped-api-tokens))
hold roles exactly like users and flow through the same `Policy.Can`
path; an API token's **scopes** (`posts:read`, `*:*`) are an additional
token-level restriction layered on top, enforced by `auth.HasScope` /
`auth.RequireScope` — independent of the role/permission model here.

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

To branch UI (or any logic) on the caller's roles rather than their
resolved permissions, read the roles back with `GetRoles`:

```go
roles := framework.GetRoles(ctx)
// [editor reader] — the same slice installed by WithRoles
if slices.Contains(roles, "admin") {
    // render the admin-only nav
}
```

`GetRoles` is the reader half of the role-context seam: `WithRoles`
puts roles in, `GetRoles` reads them back. It returns `nil` for a nil
context or one carrying no roles — never panics, so it is safe to call
on an un-wired (anonymous) request. Permission checks should still go
through `GetPermissions` / `Can`; `GetRoles` is for role-shaped
branching where the permission grant map isn't the right granularity.

Or via middleware on a specific route:

```go
app.Router().Delete("/posts/{id}",
    framework.RequirePermission("posts:delete")(http.HandlerFunc(postsHandler)))
```

`RequirePermission` returns `403 access denied: missing permission X`
when the user does not hold the named permission. The error format
is JSON via `core/handler.WriteError`.

## The `Policy` interface

```go
type Policy interface {
    Can(ctx context.Context, permission Permission) bool
}
```

The check takes only the `ctx` and the permission string — there is
**no** resource argument. Everything a policy needs (subject, roles,
tenant, request metadata) travels in the context. Implement this to
plug in:

- Database-backed permission lookups.
- External authorisation services (OPA, etc.).

`RolePolicy` is the shipped implementation; it resolves the roles
installed via `WithRoles` against its grant map.

### Row-level ("can user X update post Y?") checks

The `Policy` interface is **coarse-grained** — it answers "does this
context hold permission P?", not "may this context act on record R?".
There is no resource argument, so per-record decisions are made
elsewhere:

- **Owner scoping** — set `EntityConfig.OwnerField` so auto-CRUD only
  ever reads/writes rows owned by the caller. See
  [entity-declarations.md](entity-declarations.md) → "Per-user scoping".
- **`BeforeCreate` / `BeforeUpdate` / `BeforeDelete` hooks** — these run
  with the candidate record (and, for updates, the patch) in hand, so
  they can deny per-row. Return an error from the hook to reject. This is
  the supported seam for "can user X update post Y?".

Keep coarse permission checks in the `Policy` and put record-aware logic
in a hook or owner scoping — don't try to smuggle the resource through
`Can`.

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
