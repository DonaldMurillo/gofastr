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

The spec also declares **how** callers authenticate. Auto-CRUD is
secure-by-default (see [security](security.md) → "Default CRUD
authentication"), so every entity is auth-gated in the spec — owner-scoped,
multi-tenant, RBAC-gated, or just the plain default session requirement —
UNLESS it declares `Public: true`. Every gated operation carries
`components.securitySchemes` with two schemes it accepts: `bearerAuth`
(HTTP bearer, JWT) and `cookieAuth` (the auth battery's session cookie),
listed in a per-operation `security` block — either scheme authorises the
call. Only `Public: true` entities are left unmarked, so clients and
codegen correctly treat exactly those (and nothing else) as publicly
reachable. Auth is per-operation, not global: the spec never sets a
top-level `security` requirement.

The `cookieAuth` name is the auth battery's production default
(`__Host-session`, set in `battery/auth` `AuthConfig.defaults()`); `DevMode`
flips it to `session_id`. If your deployment overrides
`AuthConfig.SessionCookie`, overwrite the scheme after building the spec —
`Spec.SetSecurityScheme("cookieAuth", …)` replaces it by name.

> **Important — `EntityConfig.Access` is HTTP-only.** It gates the HTTP
> CRUD routes, not in-process repository or `CrudHandler` calls. This is
> intentional: in-process Go code is trusted, while owner and tenant
> isolation still apply at the data layer. SSR screens do not inherit
> `EntityConfig.Access` checks automatically. Enforce per-row rules for SSR
> in lifecycle hooks or explicit screen/handler checks before calling
> `CrudHandler.CreateOne/UpdateOne/DeleteOne/GetOne/ListAll/UpsertOne` or a
> generated typed repo. For deliberate cross-owner reads, use
> `owner.AllowCrossOwner(ctx)` only from trusted server-side Go; HTTP CRUD
> has no path to it. See [entity-declarations](entity-declarations.md) →
> "Reading across owners".
>
> **Declarative cross-owner read.** `EntityConfig.CrossOwnerRead` names a
> permission that, when held by the request context, lifts owner scoping
> for READ operations only on that entity — letting a staff or admin role
> see every owner's rows on List/Get/Count while writes stay scoped. It
> is fail-closed (no policy ⇒ no widening) and requires `OwnerField`. See
> [entity-declarations](entity-declarations.md) → "Letting a role read
> every owner's rows".

> Before this existed, exposing an entity granted **every authenticated
> user full CRUD** unless you hand-composed route-group middleware.
> `EntityConfig.Access` makes the requirement visible at the declaration
> and enforced by default.

## Concepts

- **Permission** — string capability. By convention `"<resource>:<verb>"`
  (`"posts:read"`, `"users:delete"`). A capability registry can validate
  grants, but the registry is optional and the string format remains yours.
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

Register the capabilities the application checks, then grant them to roles:

```go
p := framework.NewRolePolicy()
p.Register("users:read", "users:write", "teams:read", "teams:write")

if err := p.Grant("admin", framework.Wildcard); err != nil {
    return err
}
if err := p.Grant("editor", "users:read", "users:write"); err != nil {
    return err
}
p.Revoke("editor", "users:write")

caps := p.Capabilities() // sorted defensive copy
```

`Register` is idempotent and thread-safe. `Grant` ignores duplicate entries.
With a non-empty registry, an unknown grant is accepted for backward
compatibility but emits a `slog` warning naming the grant, its nearest
registered capability, and that it will never match a registered gate.

Opt into rejection when configuration mistakes must stop startup:

```go
p := framework.NewRolePolicy().StrictCapabilities()
p.Register("users:read", "users:write")
if err := p.Grant("editor", "usres:write"); err != nil {
    return err // rejected; nothing was granted
}
```

Strict rejections are typed: `errors.As(err, &e)` with
`*access.UnknownCapabilityError` distinguishes a caller's typo (`e.Grant`,
`e.Nearest`) from a real store failure, so handlers can answer 400 instead of
500 — the admin grant screen does exactly this.

The global `framework.Wildcard` (`"*"`) remains the superuser grant. A
resource wildcard such as `"teams:*"` is different: with a non-empty
registry, `Grant` expands it immediately to every registered capability with
the `"teams:"` prefix, and `Can` continues to perform exact matching. The
wildcard itself is not retained. `GrantStore.Grant` persists those expanded
rows, and `LoadInto` expands old wildcard rows while loading them.

With an empty registry, ordinary grants keep the previous behavior and emit
no warning. A non-global grant containing `*` cannot expand, so it emits the
loud warning and remains stored for compatibility; strict mode rejects it.
Register capabilities before using resource wildcards.

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

### Caching role resolution

`access.NewCachedResolver` wraps the role lookup function passed to
`access.Middleware`:

```go
roles := access.NewCachedResolver(
    func(ctx context.Context) []string {
        user := auth.GetCurrentUser(ctx)
        if user == nil {
            return nil
        }
        return loadEffectiveRoles(ctx, user.GetID())
    },
    access.WithTTL(30*time.Second),
)

app.Use(access.Middleware(policy, roles.Resolve))
```

The default TTL is 30 seconds. Cache keys come from the authenticated value in
`core/handler` context when it implements `GetID() string`, which is the same
user seam populated by `battery/auth`. Missing or empty user IDs resolve
without caching, so anonymous requests never share role state. Concurrent
misses for one user share one lookup. `Resolve` returns defensive copies;
call `Invalidate(userID)` after changing one user's role inputs or
`InvalidateAll()` after a global role-policy change. A zero or negative TTL
keeps same-key single-flight behavior but does not retain results.

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


## Persistent grants (GrantStore)

`RolePolicy` grants are code-defined at boot: `policy.Grant("admin", ...)`.
For apps that need **runtime-editable** RBAC (an admin UI that grants and
revokes without a redeploy), `access.GrantStore` persists grants to a
database table and keeps the live `*RolePolicy` in sync.

```go
policy := framework.NewRolePolicy()
policy.Grant("admin", framework.Wildcard) // code-defined baseline

store := framework.NewGrantStore(db, policy)
store.EnsureSchema(ctx)                    // CREATE TABLE IF NOT EXISTS access_grants
store.LoadInto(ctx, policy)               // hydrate from DB → live policy

// Later: runtime grant (admin screen, CLI, etc.)
store.Grant(ctx, "editor", "posts:write")  // DB INSERT + policy.Grant
store.Revoke(ctx, "editor", "posts:write") // DB DELETE + policy.Revoke
```

### Shape

The store **holds a reference** to the live `*RolePolicy` (store-holds-policy).
`NewGrantStore(db, policy)` binds the policy; `LoadInto(ctx, policy)` loads
persisted rows into it (call once at boot). Subsequent `Grant`/`Revoke` calls
mutate both the DB and the policy in one call — the policy's RWMutex covers
concurrent `Can` checks, so a grant/revoke is "atomic enough": a reader sees
the state before or after, never a torn map.

### Cross-replica grant propagation

`GrantStore.Grant`/`Revoke` mutate the LOCAL `*RolePolicy` only. With N
replicas behind a load balancer sharing one database, the other replicas'
in-memory policies stay stale until restart — an editor granted on replica
A still fails `Can("posts:write")` on replica B until B reboots.

Attaching a fanout closes that window. Register the store with the app
(`framework.WithGrantStore`) so the framework auto-wires it to the same
fanout as `WithFanout`:

```go
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithGrantStore(store),
    framework.WithFanout(fanout.NewPostgres(dsn, db)),
)
```

On every `Grant`/`Revoke`, the store publishes a refresh-signal on the
`gofastr.access` lane naming the role whose grants changed. Each
subscriber re-reads that role's rows from `access_grants` and atomically
swaps them into its local policy via `RolePolicy.ReplaceRole`. The
message body is never trusted — a crafted payload can only trigger a
re-read, never pollute the policy directly.

**Consistency window.** Fanout is lossy best-effort. A publish that
doesn't reach a peer (the peer's queue overflowed, the bus was briefly
down) leaves that peer stale until the NEXT grant/revoke on the same
role, or until restart (when `LoadInto` re-hydrates authoritative
state). The store itself remains correct — it always reads from and
writes to the DB; only the in-memory cache lags. A reconnecting
replica's `LoadInto` reloads authoritative state on boot.

Capability validation happens before `GrantStore` writes. A strict rejection
therefore leaves both the database and live policy unchanged. In warning mode,
unknown concrete grants remain persisted for compatibility. The admin roles
screen uses `Policy.Capabilities()` as a datalist when the registry is
non-empty and marks existing non-global grants outside the registry as
`unknown/dead`.

### Security

- Role and permission strings are **bound as `$n` parameters** — never
  interpolated into SQL. The table name is validated via `query.SafeIdent`.
- `Grant`/`Revoke` do **not** check authorization — they are trusted
  server-side calls. The admin battery gates them behind its default-deny
  `b.gate` (see [Admin UI](admin.md)).
- There is no unauthenticated or self-service grant path.

### Enumeration API

`RolePolicy` exposes read-only getters for admin UIs:

```go
roles := policy.Roles()                    // sorted []string
perms := policy.PermissionsOf("editor")    // []Permission (copy)
caps  := policy.Capabilities()             // sorted []Permission (copy)
```

All three return defensive copies — callers iterate without holding the lock.

### Effective roles in the admin

`admin.Config.EffectiveRoles` can add resolved role origins to the user-role
screen without changing the direct roles stored by `battery/auth`:

```go
admin.New(admin.Config{
    Auth: authManager,
    EffectiveRoles: func(ctx context.Context, userID string) []access.RoleWithOrigin {
        return []access.RoleWithOrigin{
            {Role: organizationRole(ctx, userID), Origin: "resolved"},
        }
    },
})
```

The screen unions these entries with `auth_users.roles`, labels stored roles
as `direct`, and preserves only the direct roles in its assignment form.
Duplicate role/origin pairs are shown once. When the hook is nil, the screen
keeps its direct-roles-only output.


## Resource-scoped decisions

The `Policy.Can` check is coarse-grained by design — "does this context hold
permission P?", with no resource argument (see [Row-level checks](#row-level-can-user-x-update-post-y-checks)).
Owner scoping and lifecycle hooks cover most per-row needs. When you need a
per-resource authority that those don't express — "a team **maintainer** may
edit **their** team's projects, but not other teams'" — without standing up a
ReBAC/tuple store, install a **Decider**. The decider is consulted *before* the
role policy on resource-aware checks, so it can tighten *or* loosen the coarse
`Can` answer per record.

### The seam

```go
// access.Ref identifies the resource a check is about.
type Ref struct {
    Type string // entity name: "projects"
    ID   string // record id; "" for collection-level checks (List/Create/batch/feed)
}

type Decision int
const (
    DecisionAbstain Decision = iota // fall through to the role policy (Can)
    DecisionAllow                   // permit; role policy not consulted
    DecisionDeny                    // refuse, even when the role policy would allow
)

type Decider func(ctx context.Context, roles []string, capability Permission, resource Ref) Decision
```

`access.CanResource(ctx, capability, resource)` is the resource-aware entrypoint:

1. If a `Decider` is in ctx → call it with the caller's roles, the capability,
   and the `Ref`. `DecisionAllow` → true; `DecisionDeny` → false;
   `DecisionAbstain` → fall through.
2. Otherwise (or after Abstain) → exactly `access.Can(ctx, capability)`.

`access.Can` itself is untouched — there is no wildcard or resource-segment
logic in the hot path. The resource-aware path is a separate entrypoint you opt
into; with no decider installed, `CanResource` answers byte-identically to `Can`.

### Wiring it: DeciderMiddleware

`access.DeciderMiddleware(d)` installs a decider into request context. Mount it
alongside `access.Middleware` — the two compose; the policy+roles middleware
feeds the decider its `roles` argument:

```go
roles := access.NewCachedResolver(
    func(ctx context.Context) []string {
        user := auth.GetCurrentUser(ctx)
        if user == nil { return nil }
        return loadEffectiveRoles(ctx, user.GetID())
    },
    access.WithTTL(30*time.Second),
)

app.Use(access.Middleware(policy, roles.Resolve))
app.Use(access.DeciderMiddleware(decideProjectAccess))
```

### Worked example: team maintainers edit their projects

A team-maintainer rule that the role policy can't express: any caller holding
`projects:update` may edit *some* projects, but a maintainer may edit every
project their team owns even without the global grant. The decider consults a
memberships table and returns Allow/Deny/Abstain:

```go
func decideProjectAccess(ctx context.Context, roles []string, cap access.Permission, res access.Ref) access.Decision {
    // Only opine on project writes for a specific record.
    if res.Type != "projects" || res.ID == "" {
        return access.DecisionAbstain
    }
    user := auth.GetCurrentUser(ctx)
    if user == nil {
        return access.DecisionAbstain // let the role policy fail-closed
    }
    // Maintainer of this project's team → allow the update regardless of role.
    if cap == "projects:update" && isMaintainerOf(ctx, user.GetID(), res.ID) {
        return access.DecisionAllow
    }
    // Otherwise defer to the role policy (which may grant projects:update globally).
    return access.DecisionAbstain
}
```

Auto-CRUD consults this automatically: `EntityConfig.Access` gates route through
`CanResource`, passing `Ref{Type: <entity name>, ID: <path id>}` for item-scoped
ops (read-one/update/delete) and `Ref{Type: <entity name>, ID: ""}` for
collection-level ops (list/create/batch/the `_events` feed). No handler change
is needed — declaring the `Access` block and mounting `DeciderMiddleware` is the
whole wiring.

### When to Deny vs Abstain

- **Deny** when the decider has *positive knowledge* the caller must not act on
  this resource — e.g. "this project belongs to a team the caller is not on".
  Deny short-circuits to false; the role policy never runs, so even a wildcard
  grant cannot override it. Use Deny to tighten below the role policy.
- **Abstain** when the decider has *no opinion* — the resource type isn't one it
  governs, the caller's relationship is unknown, or you want the role policy to
  decide. Abstain is the zero value, so a decider that forgets to return is
  safe (falls through to `Can`). Use Abstain to delegate.
- **Allow** when the decider grants access the role policy would not — e.g. the
  team-maintainer case above. Allow short-circuits to true; use it to loosen
  beyond the role policy for a specific resource.

A decider that always returns Abstain is a no-op: behaviour is exactly the
role-policy-only world. That is the safe default while you roll the decider out.

### Alternative: the `resource:id:capability` string convention

An app-side pattern (the framework does **not** interpret this) encodes the
resource into the permission string itself: `"projects:42:update"`. Combined
with `GrantStore`, each such string becomes a row in `access_grants`, so the
grant matrix is visible in the admin UI and editable at runtime — every
per-resource grant is a real, enumerable row.

The tradeoff is **row explosion**: one row per (role, resource, capability),
which is fine for dozens of resources but does not scale to thousands. The
framework's `Can` performs exact-string matching, so it never parses the
`resource:id:capability` segments — that decomposition is a convention your
code (or a wrapper policy) owns. If you need per-resource authority at scale,
or with inheritance ("a maintainer of team T may edit all of T's projects"),
use the **Decider seam** above: one membership check replaces unbounded grant
rows, and the rule lives in your code where it can consult any table or
service. The two compose — a Decider can fall back to `Abstain` and let a
`resource:id:capability` grant row in the policy decide.

## ScopeMatch (module/token scope algebra)

`Can` is the RBAC hot path and stays deliberately blunt: it matches an
**exact** permission string, or the global `Wildcard` (`"*"`). It does **not**
understand `posts:*` or `*:read` — widening it would silently change live
RBAC for every caller, so it is untouched.

`access.ScopeMatch(granted []Permission, required Permission) bool` is the
separate, pure matcher for the richer `resource:verb` wildcard grammar used
by **token scopes** and **module capability grants**:

```go
// exact | resource wildcard | verb wildcard | grant-all
access.ScopeMatch([]access.Permission{"posts:*"}, "posts:read")  // true
access.ScopeMatch([]access.Permission{"*:read"}, "users:read")   // true
access.ScopeMatch([]access.Permission{"*:*"}, "anything:here")   // true
access.ScopeMatch(nil, "posts:read")                             // false (deny by default)
```

It is a function of its two arguments only — it **does not consult the
capability registry and does not expand resource wildcards** the way
`RolePolicy.Grant` does at grant time (`teams:*` matches literally here; it
is not fanned out to registered `teams:` capabilities). Matching and
grant-time expansion are deliberately not entangled.

`battery/auth`'s token-scope matcher (`HasScope` / `auth.ScopeMatch`)
delegates to this function, so the `resource:verb` algebra has exactly one
home. Use `access.ValidScope(s)` to reject malformed scope strings at
mint/install time under the same closed vocabulary. New capability gates
that hold `[]Permission` should call `access.ScopeMatch` directly rather
than reimplementing a weaker string matcher.