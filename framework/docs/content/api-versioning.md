# API prefix & versioning

Pick the smallest versioning layer your app actually needs:

| Need | Use |
| --- | --- |
| One API under `/api` or `/api/v1` | `framework.WithAPIPrefix` |
| `v1` and `v2` live side by side | Route groups (`App.Group` + `App.GroupEntity`) |
| Deprecation/Sunset headers + per-version field projections | `framework/experimental/apiversions` |

Prefixes apply consistently across REST routes, `/openapi.json`, and generated MCP tools so clients, SDKs, and agents always agree on paths.

## 1. Single global prefix (`WithAPIPrefix`)

```go
app := framework.NewApp(
	framework.WithDB(db),
	framework.WithAPIPrefix("/api/v1"),
)
app.Entity("posts", framework.EntityConfig{ /* ... */ })
```

Or via `AppConfig`:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
-->
```go
framework.NewApp(framework.WithConfig(framework.AppConfig{APIPrefix: "/api/v1"}))
```

`"api"`, `"/api"`, and `"/api/"` all normalize to `"/api"`. Relative custom `Endpoint.Path` values (`"{id}/publish"`) mount beneath the prefixed entity path; leading-slash absolute paths (`"/health/posts"`) bypass the prefix. OpenAPI operation paths carry the full prefixed path directly (`servers` stays `[{"url": "/"}]`).

## 2. Side-by-side versions with route groups

Serve multiple versions simultaneously by registering each version in its own route group with a distinct MCP namespace (preventing duplicate tool name panics) and OpenAPI tag:

```go
v1 := app.Group("/api/v1",
	routegroup.WithMCPNamespace("v1"),
	routegroup.WithOpenAPITag("v1"),
)
v2 := app.Group("/api/v2",
	routegroup.WithMCPNamespace("v2"),
	routegroup.WithOpenAPITag("v2"),
)

app.GroupEntity(v1, "posts", postsV1Config)
app.GroupEntity(v2, "posts", postsV2Config)
```

Look up a specific version programmatically with `Registry.GetVersioned("posts", "/api/v1")`.

### Shared table, shared schema

Two versions of the same entity name share **one physical database table**, and auto-migrate applies the union of their columns (additive columns in `v2` migrate once automatically).

To prevent silent schema corruption or security bypasses across versions sharing one table, registration panics at startup if versions disagree on:
- **Physical column definitions:** rendered SQL type (`Type`, `RawType`, `Max` on `schema.String`), `Unique`, `Required`, `AutoGenerate`, `Default`, or primary-key membership. (Wire-only fields — `Hidden`, `WireName`, `ReadOnly`, validation bounds — are free to differ.)
- **Structural & security invariants:** physical `Table` name, `PrimaryKey` columns/order, `Unmanaged` posture, exclusive `Required` columns with no default/auto-generate, row scoping (`MultiTenant`, `TenantField`, `OwnerField`, `SoftDelete`), access gates (`Access`, `Public`, `CrossOwnerRead`), duplicate `Seed` functions, or conflicting named indexes / foreign keys.

Relations and `many_to_many` pivots (`entity.ResolveTarget`, `entity.PivotEntity`) resolve at the source's own version first, then unversioned, then the sole registered version (refusing ambiguous multi-version pivots with `include_pivot`).

## 3. Deprecation, sunsets, and projections (`apiversions`, experimental)

`framework/experimental/apiversions` wraps route groups with RFC deprecation headers and per-version field reshaping:

```go
import "github.com/DonaldMurillo/gofastr/framework/experimental/apiversions"

v1 := apiversions.Version(app.Router(), "v1",
	apiversions.WithDeprecation(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), "/v2"),
)
v1.Use(v1.DeprecationMiddleware()) // emits Deprecation, Sunset, and Link headers

v2 := apiversions.Version(app.Router(), "v2")

ps := apiversions.NewProjectionSet(
	&apiversions.Projection{Version: "v1", Exclude: []string{"summary"}},
	&apiversions.Projection{Version: "v2"},
)

app.GroupEntity(v1.Group(), "posts", apiversions.ApplyToEntityConfig(basePostsConfig, ps, "v1"))
app.GroupEntity(v2.Group(), "posts", apiversions.ApplyToEntityConfig(basePostsConfig, ps, "v2"))
```

- `Projection.Include` / `Exclude` hide columns on the wire (`Hidden: true`) without altering the shared table.
- `Projection.Rename` remaps JSON keys (`WireName`) while keeping the underlying DB column intact (colliding wire keys panic at `Define`).
- Every name in `Include`, `Exclude`, and `Rename` keys must refer to a declared or injected column name (`internal_notes`, not `internalNotes`), or startup panics.

## Common mistakes

- **Mixing `WithAPIPrefix` and `App.Group`:** `App.GroupEntity` mounts at the group's exact prefix and ignores `WithAPIPrefix`. Spell the full prefix on the group (`app.Group("/api/v1")`).
- **Omitting `routegroup.WithMCPNamespace` on versioned groups:** causes a duplicate MCP tool name panic on the second registration.
- **Declaring incompatible types or security settings across versions of one entity:** `v1` and `v2` share one table, so column SQL types, `MultiTenant`, `OwnerField`, `SoftDelete`, and `Access` must match.
- **Calling `apiversions.WithDeprecation` without `v1.Use(v1.DeprecationMiddleware())`:** configuring deprecation metadata does not emit `Deprecation`/`Sunset`/`Link` headers until the middleware is attached.
- **Passing camelCase wire names to `Projection.Exclude` or `Rename`:** pass the declared Go/DB column name (`internal_notes`), not its wire alias.
