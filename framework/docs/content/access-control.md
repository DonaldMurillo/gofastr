# Access control

GoFastr's access control is permission-based with role grants. Your auth layer puts roles on the request context; the policy checks whether those roles hold a required capability.

## Quickstart

```go
policy := framework.NewRolePolicy()
policy.Grant("admin",  "posts:read", "posts:write", "posts:delete")
policy.Grant("editor", "posts:read", "posts:write")
policy.Grant("reader", "posts:read")

// Attach policy + current user's roles to every request:
app.Use(framework.AccessMiddleware(policy, currentUserRoles))
// currentUserRoles has signature func(ctx context.Context) []string

// Gate individual routes:
app.Router().Post("/posts",
    framework.RequirePermission("posts:write")(http.HandlerFunc(postsHandler)))
```

`AccessMiddleware` is shorthand for calling `framework.WithPolicy(ctx, policy)` and `framework.WithRoles(ctx, roles)` in middleware. Both are required—missing either fails closed (`403`).

## Gating auto-CRUD (`Exposure.Access`)

Declare operation permissions directly on an entity to gate its REST routes, batch/stream endpoints, `_events` SSE feed, and generated MCP tools:

```go
app.Entity("posts", framework.EntityConfig{
    Exposure: &framework.ExposureConfig{
        Access: framework.AccessControl{
            Read:   "posts:read",   // List + Get + _events
            Create: "posts:write",
            Update: "posts:write",
            Delete: "posts:delete",
        },
    },
})
```

Once any field in `Access` is set:
- **Blank `Read`:** reads are open to everyone ("public reads, gated writes").
- **Blank `Create`, `Update`, or `Delete`:** denied to every caller (`403`), including wildcard roles and MCP tools (only trusted server code with `crud.WithElevation` can write).
- **`framework.Anyone`** (`"anyone"` in YAML): explicitly opens that operation to anonymous callers (e.g., `Create: framework.Anyone` for a public contact form). Owner and tenant scoping still apply if configured.

### Row-level filtering, OpenAPI & in-process reads

- **Row-level read scoping:** `Exposure.Access` controls *whether* a caller can access the entity; `Exposure.ReadScope` controls *which rows* they see (e.g., `status=published` for anonymous callers, all rows for `Unrestricted` or signed-in users). See [entity-declarations.md](entity-declarations.md).
- **OpenAPI filtering:** `/openapi.json` advertises `401`/`403` and `bearerAuth`/`cookieAuth` on every gated entity (everything except `Public: true`), and omits entities the caller cannot read so schemas aren't disclosed. Pass `WithPublicOpenAPI` to serve the full unfiltered spec. If you customize `AuthConfig.SessionCookie`, update the scheme via `Spec.SetSecurityScheme("cookieAuth", ...)`.
- **In-process Go calls:** `Exposure.Access` is HTTP-only. Trusted server-side Go (`CrudHandler.ListAll`, typed repos, SSR screens) still enforces owner and tenant scoping, but does not run `Exposure.Access` automatically—use `CanReadScoped` / `RequireRead` below or lifecycle hooks for SSR and custom handlers.
- **Cross-owner reads:** Set `Scope.CrossOwnerRead` to a permission name so staff/admin roles can read across owners while writes remain owner-scoped. For trusted background code, use `owner.AllowCrossOwner(ctx)`.

## Policies, capabilities & wildcards

A **Permission** is a string (conventionally `"<resource>:<verb>"`), a **Role** holds permissions, and a **Policy** implements:

```go
type Policy interface {
    Can(ctx context.Context, permission Permission) bool
}
```

Implement `Policy` yourself for DB-backed or external (e.g., OPA) authorization. The built-in `RolePolicy` supports an optional capability registry to catch typos at startup and during `gofastr verify` (rule `GOFASTR1904`):

```go
p := framework.NewRolePolicy().StrictCapabilities() // omit StrictCapabilities() to warn instead of error
p.Register("users:read", "users:write", "teams:read", "teams:write")

if err := p.Grant("admin", access.Wildcard); err != nil { // "*" superuser; imported from framework/access
    return err
}
if err := p.Grant("editor", "teams:*"); err != nil { // expands immediately to teams:read, teams:write
    return err
}
p.Revoke("editor", "teams:write")
```

- `access.Wildcard` (`"*"`) is the global superuser grant (import `github.com/DonaldMurillo/gofastr/framework/access`).
- Resource wildcards like `"teams:*"` expand against registered capabilities at `Grant`/`Revoke` time; `Can` always does exact matching (or `"*"`). Always call `Register` before granting resource wildcards.
- Strict rejections return `*access.UnknownCapabilityError` (struct fields `e.Grant` and `e.Nearest`) so admin UIs can return `400`.
- Read-only inspection helpers (defensive copies): `p.Roles()`, `p.PermissionsOf(role)`, `p.Capabilities()`.

### Checking in handlers & caching role lookups

Read permissions or roles from context:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
import "context"
var ctx = context.Background()
import "slices"
-->
```go
roles := framework.GetRoles(ctx)
if slices.Contains(roles, "admin") {
    // role-shaped UI branching; prefer framework.GetPermissions(ctx) or access.Can for authz
}
```

To avoid hitting the DB on every request, wrap your role loader with `access.NewCachedResolver` (30s default TTL, single-flight per `(userID, tenantID)`, bounded to 100,000 keys, never caches anonymous or canceled lookups):

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

Call `roles.Invalidate(userID)` or `roles.InvalidateAll()` after changing roles. Note that cache invalidation is local to the replica—on multi-replica deployments, shorten `WithTTL` or store runtime grants in a `GrantStore` with fanout.

### Scoped tokens & roles

Service accounts and personal API tokens ([auth.md](auth.md#service-accounts--scoped-api-tokens)) carry their owner's roles, **narrowed** by their token scopes. `RequirePermission`, `access.Can`, `CanResource`, and `CanResourceExact` refuse any scoped caller whose scopes don't cover the permission (even if a `Decider` returns `DecisionAllow`). Role gates (`auth.RequireRole`, `auth.MCPRole`, `framework.MCPRequireRole`, `/.debug`) require `"role:<name>"` (`access.RoleScope(role)`, `"role:*"`, or `"*:*"`).

## Non-CRUD surfaces & relation checks

SSR screens, reports, exports, and custom handlers bypass CRUD route middleware. Use the `CrudHandler` read-posture helpers so every surface enforces the same baseline session, owner, tenant, and RBAC rules as `GET /api/<entity>`:

- **`crudHandler.CanReadScoped(ctx)`:** Returns `bool` for SSR/UI checks without writing HTTP responses.
- **`crudHandler.RequireRead(w, r)`:** Writes the exact same `401`/`403` error response as the CRUD list route and returns `false` on denial.
- **`crudHandler.CanReadRecordScoped(ctx, id)`:** Checks collection posture plus a per-record `Decider` (use on detail/edit screens).
- **`crudHandler.CanReadRelatedScoped(ctx)`:** Same as `CanReadScoped`, plus checks `Exposure.CRUD` and scoped-token `<table>:read` (use for pickers, facets, and relation labels).

Prefer `CanReadScoped` over `CanRead`—`CanRead` only checks RBAC and returns `true` for anonymous callers on default-posture entities that declare no `Access` block. `framework/entityui` calls these helpers automatically (rendering muted `—` instead of raw IDs for unreadable relations). On CRUD routes, `?include=rel` and `?rel.field=` automatically enforce the target entity's posture and owner/tenant scopes.

## Persistent grants (`GrantStore`)

When roles need runtime edits (for example from `battery/admin`) without redeploying, `GrantStore` persists changes in `access_grants` and keeps your live `*RolePolicy` synchronized:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/access"
import "database/sql"
var db *sql.DB
import "context"
var ctx = context.Background()
-->
```go
policy := framework.NewRolePolicy()
policy.Grant("admin", access.Wildcard) // code-defined baseline

store := framework.NewGrantStore(db, policy)
store.EnsureSchema(ctx)
store.LoadInto(ctx, policy)

store.Grant(ctx, "editor", "posts:write")  // DB + live policy
store.Revoke(ctx, "editor", "posts:write") // DB delete + tombstone + live policy
```

### Cross-replica propagation & revocation tombstones

Wire the store with `framework.WithGrantStore(store)` alongside `framework.WithFanout(pg)` so `Grant`/`Revoke` publishes refresh signals on the `gofastr.access` lane. Peers re-read that role from the DB and swap it atomically via `RolePolicy.ReplaceRole`. A background reconciliation timer reloads every role every 5 minutes (`access.DefaultReconcileInterval`, tunable via `access.WithReconcileInterval`) in case a fanout frame is dropped.

`App.Shutdown` waits for the grant store's fanout to stop only until the shutdown deadline. If the deadline expires first, the store finishes stopping in the background once its in-flight work returns. Shutdown reports an error wrapping `context.DeadlineExceeded` unless an earlier teardown error takes precedence.

`Revoke` writes a tombstone in `access_grants_revoked` so revoking a code-seeded grant survives restarts and propagates across replicas. Effective grants are always `(code baseline ∪ DB grants) − tombstones`:
- Calling `Grant` is the only way to lift a tombstone.
- After `Revoke("teams:*")`, granting a single child like `Grant("teams:read")` returns `access.ErrRevokedByWildcard` (re-grant `"teams:*"`, then revoke individual permissions). Use `store.CheckGrant(ctx, role, perms...)` to preflight a batch without writing.
- `EnsureSchema` registers both tables with `App.ExportData`; restart after `App.ImportData` so `LoadInto` reloads them.
- `Grant`/`Revoke` are trusted server calls with no built-in authz check (`battery/admin` gates them behind its admin gate).

To display resolved (non-direct) roles in `battery/admin`'s user screen, pass `admin.Config.EffectiveRoles`:

```go
admin.New(admin.Config{
    Auth: authManager,
    EffectiveRoles: func(ctx context.Context, userID string) []access.RoleWithOrigin {
        return []access.RoleWithOrigin{{Role: organizationRole(ctx, userID), Origin: "resolved"}}
    },
})
```

## Resource-scoped decisions (`Decider`)

`Policy.Can` is intentionally coarse ("does this caller hold `projects:update`?"). For per-row rules, start with `Scope.OwnerField`, `Exposure.ReadScope`, or `BeforeCreate`/`BeforeUpdate`/`BeforeDelete` hooks.

When you need relationship-based authority ("a team maintainer can edit their team's projects") across CRUD, batch operations, and route gates, install a `Decider`:

```go
type Ref struct {
    Type string // entity name, e.g. "projects"
    ID   string // record id, or "" for collection-level checks
}

type Decider func(ctx context.Context, roles []string, capability access.Permission, resource access.Ref) access.Decision
```

A `Decider` returns one of three values:
- **`access.DecisionAllow`:** Permits immediately (loosens beyond role policy; still narrowed by token scopes).
- **`access.DecisionDeny`:** Refuses immediately (tightens below role policy; even `access.Wildcard` cannot override it).
- **`access.DecisionAbstain`** (zero value): Falls through to `RolePolicy`.

### Wiring it: DeciderMiddleware

Mount `access.DeciderMiddleware` after `access.Middleware`:

```go
app.Use(access.Middleware(policy, roles.Resolve))
app.Use(access.DeciderMiddleware(decideProjectAccess))
```

```go
func decideProjectAccess(ctx context.Context, roles []string, cap access.Permission, res access.Ref) access.Decision {
    if res.Type != "projects" || res.ID == "" {
        return access.DecisionAbstain
    }
    user := auth.GetCurrentUser(ctx)
    if user == nil {
        return access.DecisionAbstain
    }
    if cap == "projects:update" && isMaintainerOf(ctx, user.GetID(), res.ID) {
        return access.DecisionAllow
    }
    return access.DecisionAbstain
}
```

Auto-CRUD calls `access.CanResource(ctx, cap, ref)` automatically (including per-item inside `PATCH`/`DELETE /_batch` transactions). State-transition permissions use `access.CanResourceExact`, where `access.Wildcard` does not match on abstain. Route and role gates (`RequirePermission`, `auth.RequireRole`, `MCPRequireRole`, `/.debug`) also consult the decider with a zero `Ref` (role gates pass `access.RoleGateCapability`), so resource deciders should abstain when `res.ID == ""`.

## ScopeMatch (token & module scopes)

`RolePolicy.Can` only matches exact strings or `"*"`. For the `resource:verb` wildcard algebra used by API tokens and module grants, call `access.ScopeMatch` (and validate inputs with `access.ValidScope`):

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/access"
-->
```go
access.ScopeMatch([]access.Permission{"posts:*"}, "posts:read") // true
access.ScopeMatch([]access.Permission{"*:read"}, "users:read")  // true
access.ScopeMatch([]access.Permission{"*:*"}, "anything:here")  // true
access.ScopeMatch(nil, "posts:read")                            // false (deny by default)
```

`ScopeMatch` is purely syntactic—it does not consult a capability registry. `auth.HasScope` and `auth.ScopeMatch` delegate directly to it.

## Common mistakes

- **Forgetting `WithPolicy` or `AccessMiddleware`.** Every check fails closed; if `RequirePermission` denies everyone, check your middleware order.
- **Mutating a different `*RolePolicy` instance.** Grant on the same instance passed to `AccessMiddleware`.
- **Gating an SSR screen on `CanRead` instead of `CanReadScoped`.** `CanRead` only checks RBAC and passes anonymous users on default-posture entities.
- **Trusting client-supplied roles.** Roles must come from your auth layer, never from request headers or bodies.
- **Looking for `Wildcard` on the `framework` facade.** Unlike `NewRolePolicy`, `Wildcard` is not re-exported on `framework`—import `access.Wildcard` from `github.com/DonaldMurillo/gofastr/framework/access`.
