# API prefix & versioning

GoFastr gives you three escalating levels of API versioning, from a
one-line global prefix to side-by-side `v1`/`v2` with per-version field
projections and deprecation headers. Pick the smallest one that fits.

- **One version, prefixed** → `WithAPIPrefix` (most apps).
- **Several versions at once** → route groups (`App.Group` + `App.GroupEntity`).
- **Versions that deprecate, sunset, and reshape payloads** → the
  experimental `framework/experimental/apiversions` package.

The prefix flows through *every* surface GoFastr generates — REST routes,
the OpenAPI document, and the MCP tools — so a client, an SDK generator,
and an AI agent all see the same paths.

---

## 1. A single global prefix — `WithAPIPrefix`

Mount every auto-CRUD entity route under one prefix:

```go
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithAPIPrefix("/api/v1"),
)
app.Entity("posts", entity.EntityConfig{ /* … */ })
```

`posts` now serves at `/api/v1/posts`, `/api/v1/posts/{id}`,
`/api/v1/posts/_batch`, and so on. The bare `/posts` path is **not**
mounted. The prefix is also settable via config:

```go
framework.NewApp(framework.WithConfig(framework.AppConfig{APIPrefix: "/api/v1"}))
```

Input is normalised: `"api"`, `"/api"`, and `"/api/"` all become
`"/api"`. An empty prefix (the default) keeps the historical bare
`/posts` mount, so this is fully backward-compatible.

### What the prefix touches

| Surface | Behaviour under `WithAPIPrefix("/api/v1")` |
| --- | --- |
| **REST routes** | mounted at `/api/v1/<table>` (list/get/create/update/delete, `_batch`, `_events`, …). |
| **OpenAPI** (`/openapi.json`) | the prefix is expressed as the spec's **server URL** (`servers: [{ url: "/api/v1" }]`); operation paths stay bare (`/posts`). Generated SDKs prepend the server URL, so they call `/api/v1/posts`. |
| **MCP tools** | `posts_list` / `posts_get` / `posts_create` / … dispatch against the prefixed path, so an agent driving the app over MCP reaches the same routes as REST. |

Because the prefix is part of one declaration, you never hand-edit the
spec or the tool paths — they can't drift from the routes.

---

## 2. Several versions side by side — route groups

`WithAPIPrefix` is a single, app-wide prefix. To serve `v1` **and** `v2`
at the same time (e.g. during a migration window), give each version its
own route group and register entities into it:

```go
app := framework.NewApp(framework.WithDB(db))

v1 := app.Group("/api/v1")
v2 := app.Group("/api/v2")

// Same entity, both versions:
app.GroupEntity(v1, "posts", postsV1Config)
app.GroupEntity(v2, "posts", postsV2Config)
```

Each group carries its own middleware stack, access policy, OpenAPI tag,
and MCP namespace, so the two versions are independently
gated and independently described. Register each entity into a version with
`app.GroupEntity(group, name, config)`.

---

## 3. Deprecation, sunsets & field projections — `apiversions`

For the full lifecycle (announce a version, deprecate it with a sunset
date, reshape payloads between versions) use the **experimental**
`framework/experimental/apiversions` package. It builds on route groups
and adds the version-lifecycle pieces.

> **Status:** experimental. The API may change; it lives under
> `framework/experimental/` and is not part of the stable surface.

### Mount a version and deprecate it

```go
import "github.com/DonaldMurillo/gofastr/framework/experimental/apiversions"

// v1 is deprecated, sunset on 2026-12-01, superseded by /api/v2.
v1 := apiversions.Version(app.Router(), "v1",
    apiversions.WithDeprecation(
        time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
        "/api/v2",
    ),
)
v1.Use(v1.DeprecationMiddleware()) // adds Deprecation / Sunset / Link headers

v2 := apiversions.Version(app.Router(), "v2")

app.GroupEntity(v1.Group(), "posts", postsV1Config)
app.GroupEntity(v2.Group(), "posts", postsV2Config)
```

`Version(router, "v1", …)` creates a route group at `/v1` with the MCP
namespace and OpenAPI tag set to the version. Every response from a
deprecated version then carries:

```
Deprecation: true
Sunset: <RFC 1123 date>
Link: </api/v2>; rel="successor-version"
```

Unsafe replacement URLs (non-`http(s)` schemes, embedded CR/LF) are
dropped — the `Link` header is a clickable client hint and must not become
a phishing or header-smuggling primitive.

### Reshape payloads per version — projections

When `v2` adds or hides fields, declare a **projection set** instead of
duplicating the entity:

```go
ps := apiversions.NewProjectionSet(
    // v1 hides the field that v2 adds.
    &apiversions.Projection{Version: "v1", Exclude: []string{"summary"}},
    &apiversions.Projection{Version: "v2"}, // all fields
)

app.GroupEntity(v1.Group(), "posts", apiversions.ApplyToEntityConfig(basePostsConfig, ps, "v1"))
app.GroupEntity(v2.Group(), "posts", apiversions.ApplyToEntityConfig(basePostsConfig, ps, "v2"))
```

A `Projection` selects fields with `Include` (allow-list; empty = all),
narrows them with `Exclude`, and can remap JSON keys per version with
`Rename`. `ApplyToEntityConfig` returns a copy of the base config shaped
for that version, so `v1` clients never see `summary`.

---

## Choosing an approach

| You need… | Use |
| --- | --- |
| One API, under `/api` or `/api/v1` | `WithAPIPrefix` (§1) |
| `v1` and `v2` live at once, same code | route groups (§2) |
| Deprecation headers, sunset dates, per-version field shapes | `apiversions` (§3) |

## Common mistakes

- **Mixing `WithAPIPrefix` with `App.Group` manually.** `WithAPIPrefix` is
  applied app-wide at `Start()` time. If you also call `app.Group("/api/v1")`
  and register entities there, those entities receive the prefix twice
  (`/api/v1/api/v1/posts`). Use `WithAPIPrefix` **or** route groups, not both.
- **Not propagating the prefix to the OpenAPI `servers` list.** GoFastr does
  this automatically when you use `WithAPIPrefix`. If you wire your own prefix
  via a middleware or reverse-proxy rewrite, update `AppConfig.OpenAPIServers`
  to match — otherwise SDK generators and agents read incorrect base paths.
- **Using the `apiversions` package in stable production without pinning.**
  The `framework/experimental/apiversions` package has an unstable API surface.
  Treat it like a preview: write tests that compile against the types you use
  so a breaking rename fails your build rather than silently misbehaving at
  runtime.
- **Registering the same entity name into two groups with identical configs.**
  Both groups serve the same handler state. If a `BeforeList` hook scopes by
  version, it sees the same hook registry for both — there's one entity, one
  registry, two routes. Per-version hook logic needs a per-version entity
  config (see `apiversions.ApplyToEntityConfig`).
- **Forgetting `DeprecationMiddleware`.** Calling `apiversions.Version` with
  `WithDeprecation` configures the deprecation metadata but does **not**
  automatically add the response headers — you must also call
  `v1.Use(v1.DeprecationMiddleware())`.

## See also

- [Entity declarations](entity-declarations.md) — the config you version.
- Route groups (`App.Group` / `App.GroupEntity`) for prefix + middleware + MCP namespacing.
