# API prefix & versioning

GoFastr gives you three escalating levels of API versioning, from a
one-line global prefix to side-by-side `v1`/`v2` with per-version field
projections and deprecation headers. Pick the smallest one that fits.

- **One version, prefixed** → `WithAPIPrefix` (most apps).
- **Several versions at once** → route groups (`App.Group` + `App.GroupEntity`).
- **Versions that deprecate, sunset, and reshape payloads** → the
  experimental `framework/experimental/apiversions` package.

The prefix applies everywhere GoFastr generates paths — REST routes,
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

| Where | Behaviour under `WithAPIPrefix("/api/v1")` |
| --- | --- |
| **REST routes** | mounted at `/api/v1/<table>` (list/get/create/update/delete, `_batch`, `_events`, …). |
| **Custom `Endpoints`** | a **relative** `Endpoint.Path` (`"{id}/publish"`) resolves under the prefixed table path → `/api/v1/posts/{id}/publish`. An **absolute** path (`"/health/posts"`) bypasses the prefix — the escape hatch for mounting outside the API namespace. |
| **OpenAPI** (`/openapi.json`) | every operation path carries the prefix (`/api/v1/posts`), and `servers` stays `[{ url: "/" }]`. A documented path IS the path you request. |
| **MCP tools** | `posts_list` / `posts_get` / `posts_create` / … dispatch against the prefixed path, so an agent driving the app over MCP reaches the same routes as REST. |

Because the prefix is part of one declaration, you never hand-edit the
spec or the tool paths — they can't drift from the routes.

> **Changed:** the spec used to keep bare operation paths (`/posts`) and carry
> the prefix in `servers` instead. Both forms resolve to the same URL, and a
> servers-aware client (Swagger UI, most SDK generators) was never affected.
> The old form did mislead anything that reads `paths` literally — which is
> most agents, and was also the 2026-07-26 backend eval's grader. If you have a
> consumer that concatenated `servers[0].url` with each path key, drop the
> concatenation: the path key is now complete on its own.

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
and MCP namespace, so the two versions are independently gated and
independently described. Register each entity into a version with
`app.GroupEntity(group, name, config)`.

### What you must set for versioned entities

Two things do **not** auto-disambiguate when the same entity name lives
under two groups — you must configure them per group:

1. **MCP tool names.** Without a namespace, both versions try to
   register `posts_list` → the second panics with a duplicate-tool
   error. Give each group an MCP namespace so tools are named
   `v1.posts.list` / `v2.posts.list`:

   ```go
   v1 := app.Group("/api/v1", routegroup.WithMCPNamespace("v1"))
   v2 := app.Group("/api/v2", routegroup.WithMCPNamespace("v2"))
   ```

   Entities registered via `App.Entity` (no group) keep the historical
   flat `posts_list` names — the namespace only applies inside a group
   that sets one.

2. **OpenAPI tags (optional but recommended).** Without a tag, both
   versions' operations land under the same schema-component name and
   tag in `/openapi.json`. Set a per-version tag so the spec stays
   organized:

   ```go
   v1 := app.Group("/api/v1",
       routegroup.WithMCPNamespace("v1"),
       routegroup.WithOpenAPITag("v1"))
   ```

### What the registry does for you

The entity registry keys on `(name, version)` — the version being the
group's full prefix (e.g. `/api/v1`). So the same entity name
coexists across groups without colliding. `Registry.Get(name)` resolves
the unversioned entity (registered via `App.Entity`), or the sole
version if only one exists. When multiple versions exist and none is
unversioned, `Get` returns an ambiguity error — use
`Registry.GetVersioned(name, "/api/v1")` to pick one.

### Shared table, shared schema

Two versions of one entity **share one database table**. The table name
is derived from the entity name (or set explicitly), and since both
versions mount the same entity name, they point at the same physical
table. This is the key constraint that makes side-by-side versions safe
and cheap: one table, one set of columns, two API surfaces reading and
writing it.

Because the versions share the table, the migration system treats their
declared fields as a **union**:

- **Additive differences migrate automatically.** If v2 declares a
  column v1 does not (the most common reason to cut a v2 — adding a
  field), boot auto-migrate creates that column via `ALTER TABLE ADD
  COLUMN`, exactly as a new column on a single-version entity is
  created. You do not configure anything; the column exists after boot.
- **The table is migrated once, from the union** — not once per version.
  A re-boot is a no-op even with multiple versions.
- **Single-version entities are completely unchanged.** The union is a
  superset view applied only when a name has multiple versions; one
  version per name takes the identical path as before.

A column both versions declare **must have a physically identical
definition**. If v1 declares `summary TEXT` and v2 declares
`summary INTEGER`, that is a misconfiguration — the DDL emitted at boot
would depend on which version the migrator saw first. **Registration
panics at boot** (not at migrate time) with a message naming the entity,
the table, the conflicting column, and both versions. The check derives
the column type from `migrate.SQLType` — the same function that emits the
DDL — so it catches every attribute that changes the physical column:

| Compared (must match) | Ignored (free to differ) |
| --- | --- |
| the rendered SQL type — `Type`, `RawType`, **and `String.Max`** (which selects `VARCHAR(n)`) | `Hidden` (wire visibility) |
| `Unique` | `WireName` (JSON key override) |
| `Required` (NOT NULL) | `ReadOnly` (request-body acceptance) |
| `AutoGenerate` (DEFAULT strategy) | `Min` / `Pattern` / `Values` (validation) |
| `Default` value | `To` / `Many` (relation metadata) |
| primary-key-ness (`Name == PrimaryKey`) | field ordering |

Wire-only and validation-only differences are never conflicts — they
are the whole point of versioning. `apiversions.ApplyToEntityConfig`
relies on exactly this: `Exclude` sets `Hidden=true` and `Rename` sets
`WireName`, both of which the conflict check deliberately ignores
because neither changes the physical column. (`String.Max` is the one
validation knob that DOES reach the DDL — `VARCHAR(n)` — so it is
compared; `Min`, `Pattern`, and `Values` do not.)

Beyond column definitions, structural invariants also panic at
registration, because a silent violation would corrupt the shared table
or the rows in it. The set is whatever one physical table cannot survive
the versions disagreeing about — read it as a rule, not a count: adding
a check does not retire the ones already here, so a numbered list goes
stale the moment it is written.

- **Same table.** Two versions of one name MUST target the same physical
  table. Registering `posts` at `Table: "posts_v1"` and `Table: "posts_v2"`
  is rejected — the union merges by name and would keep only one table,
  silently dropping the other version's. Use distinct entity names if you
  need distinct tables.
- **No mandatory exclusive column.** A `Required` column with no
  `Default` and no `AutoGenerate` that ONE version declares but the other
  does not is rejected — the shared table gains a `NOT NULL` column the
  other version can never supply, so every complete request through the
  older version fails at the database. Give the column a default, make it
  auto-generated, or declare it in both versions.
- **Same managed posture.** Mixing `Unmanaged: true` and managed versions
  of one name is rejected — an unmanaged representative suppresses
  migration for the whole union, so a managed version's columns would
  never be created. (A view or external table that legitimately shares a
  managed table is a different shape — distinct entity name, exempt from
  the column check by design.)
- **Same row reachability.** Two versions must agree on which rows a
  request can see: `MultiTenant`, `OwnerField`, and `SoftDelete` must
  match across versions of one name. The versions share one physical
  table and therefore one row set, so a tenant-scoped v1 beside an
  unscoped v2 meant v2 read what v1 hid, an `OwnerField`-free version
  read other users' rows, and a non-soft-delete version hard-deleted
  rows the other version expects to restore. The weaker version is a
  bypass of the stronger one; the registry rejects the mismatch at
  registration.
- **Same reachability gate.** For the same reason, two versions must
  agree on `Access`, `Public`, and `CrossOwnerRead`. These decide
  whether a request is allowed to ask at all, and they are enforced
  per-version against that one shared table — so a v2 declaring
  `Public: true` beside a session-required v1 makes every row of the
  table anonymously readable, and a v2 with a blank `Access` block
  skips the RBAC permission v1 requires. Row scoping and the gate in
  front of it are the same property from two directions.
- **At most one Seed.** Two versions may not both declare a `Seed` —
  functions cannot be compared for equality, so the second is ambiguous.
  The sole seed runs regardless of which version declares it.

Named indices and foreign keys on the shared table follow the same rule:
if two versions declare an index by the same name, or a relation on the
same FK column, their definitions (columns, expression, uniqueness,
ordering for indices; target table, key, and relation type for FKs) must
match — otherwise the union would keep one and silently violate the
other's declared invariant.

> **See also:** [migrations](migrations.md) for the full additive-only
> contract — boot never drops, renames, or retypes; destructive changes
> require a reviewed migration file (`migrate generate`).

### OpenAPI output for versioned entities

Each versioned entity gets its own:
- **Path** — `/api/v1/posts` and `/api/v2/posts` (not the bare `/posts`).
- **Schema component** — `posts_api_v1` / `posts_api_v2` (non-colliding).
- **Operation IDs** — `list_posts_api_v1` / `list_posts_api_v2`.
- **Tag** — the group's `OpenAPITag` if set, else `posts_api_v1`.

Unversioned entities (via `App.Entity`) keep the historical bare names
(`/posts`, schema `posts`, tag `posts`) — no change for existing apps.

---

## 3. Deprecation, sunsets & field projections — `apiversions`

For the full lifecycle (announce a version, deprecate it with a sunset
date, reshape payloads between versions) use the **experimental**
`framework/experimental/apiversions` package. It builds on route groups
and adds the version-lifecycle pieces.

> **Status:** experimental. The API may change; it lives under
> `framework/experimental/` and is not part of the stable API.

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
for that version.

**How it works:** `Exclude` sets the field's `Hidden` flag (the column
stays in the shared table, just hidden from that version's wire output).
`Rename` sets the field's `WireName` — the JSON key clients see, without
changing the DB column name. Both versions read and write the same
underlying table; the projection is purely a wire-level concern.

A `Rename` target must not collide with the wire key another column
already claims, or the entity is refused at `Define` time. Note that the
key a bare column claims is its **case-converted** name — `author_id`
claims `authorId` under the default camelCase — so renaming a field to
`authorId` alongside an `author_id` column is a collision even though the
column names differ. Two fields sharing one wire key would split reads
from writes silently: a body posted under that key lands on whichever
column the handler cached first, while filters resolve it independently.

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
- **Forgetting `WithMCPNamespace` on versioned groups.** Two versions of the
  same entity produce identical flat MCP tool names (`posts_list`), and the
  second registration panics. Always set `routegroup.WithMCPNamespace("v1")`
  (and `"v2"`) on version groups so tools disambiguate as `v1.posts.list`.
- **Using the `apiversions` package in stable production without pinning.**
  The `framework/experimental/apiversions` package has an API that can still change.
  Treat it like a preview: write tests that compile against the types you use
  so a breaking rename fails your build rather than silently misbehaving at
  runtime.
- **Declaring the same column with incompatible schemas across versions.**
  Two versions of one entity share one DB table, so a column both declare
  must have a physically identical definition (type, nullability,
  uniqueness, default, auto/PK). v1 `summary TEXT` + v2 `summary INTEGER`
  panics at registration — see [Shared table, shared schema](#shared-table-shared-schema).
  Add the column in v2 only, or make both definitions match.
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
