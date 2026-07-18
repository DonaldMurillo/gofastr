# Entity Declarations

> ⚠️ **Auto-CRUD is secure-by-default; per-user data still needs
> `OwnerField`.** An entity exposed via `app.Entity(...)` (or
> `app.GroupEntity(...)`) that declares none of `OwnerField`, `Access`,
> or `Public` requires an authenticated session for **every** operation
> — List/Get/Create/Update/Delete all 401 an anonymous caller. That
> closes anonymous read/write, but it does **not** scope rows by user:
> without `OwnerField`, every authenticated user still reads (and can
> overwrite) every other user's rows. For per-user data:
>
> ```go
> app.Entity("logs", entity.EntityConfig{
>     Fields:     []schema.Field{ /* … */ },
>     OwnerField: "user_id", // CRUD auto-scopes by current user; auto-stamps on Create
> })
> ```
>
> When `battery/auth` is imported, the framework's owner extractor is
> wired automatically — no extra setup needed. See the **Per-user
> scoping (`OwnerField`)** section below for details, and **Default CRUD
> authentication** below for the session-requirement contract (including
> the `Public` opt-out for genuinely public entities — a contact form, a
> blog's comments).

An entity is registered in Go with `app.Entity(name, framework.EntityConfig{…})`.
This is the primary, fully-supported way to declare an entity:

```go
app.Entity("posts", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "title", Type: schema.String, Required: true},
        {Name: "body", Type: schema.Text},
        {Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "draft"},
        {Name: "author_id", Type: schema.Relation, To: "users"},
    },
})
```

The same entity shape can also be **declared in a `gofastr.yml` blueprint**
and emitted as Go by the CLI — see [Blueprints](blueprints.md), the single
declaration format the `gofastr generate` codegen pipeline reads. The
`EntityDeclaration` / `FieldDeclaration` types documented below
(`framework/entity/declaration.go`) are the in-memory shape the blueprint
loader decodes a blueprint's `entities:` list into before converting each to
an `EntityConfig` via `.Config()`. They are not loaded from standalone files.

For Go-defined configs, `RegisterEntities` is sugar over multiple
`Entity(...)` calls. Map iteration order is randomised, but FK ordering
is still handled correctly because AutoMigrate sorts entities
topologically:

```go
app.RegisterEntities(map[string]entity.EntityConfig{
    "foods":  foodsConfig,
    "meals":  mealsConfig,
    "users":  usersConfig,
})
```

## `Entity` vs `TryEntity`

`app.Entity(name, config)` **panics** on a misconfiguration — fail-fast,
ideal for static hand-written declarations where a bad config is a bug
you want surfaced immediately. When the config is generated or untrusted
(an AI-authored field, a dynamic schema, a user-supplied declaration) and
one bad entity should not crash the process, use `TryEntity`, which
returns the error instead (and recovers panics from deeper validation):

```go
if err := app.TryEntity(name, cfg); err != nil {
    log.Printf("skipping invalid entity %q: %v", name, err)
    continue
}
```

`Entity` is a thin panicking wrapper over `TryEntity`.

## Seeding

`EntityConfig.Seed` runs once per entity after `AutoMigrate` creates the
table. The framework tracks completion in the `_gofastr_seeded` ledger;
subsequent restarts short-circuit on the ledger row. Errors abort
`App.Start`, so a failed seed prevents a half-up server.

```go
app.Entity("foods", entity.EntityConfig{
    Fields: []schema.Field{ /* … */ },
    Seed: func(ctx context.Context, db *sql.DB) error {
        _, err := db.ExecContext(ctx, `INSERT INTO foods (name)
            VALUES ('apple'), ('banana') ON CONFLICT DO NOTHING`)
        return err
    },
})
```

`Seed` should be idempotent. The ledger is best-effort tracking that
survives normal restarts but cannot guarantee atomicity between user
inserts and the ledger row; prefer `INSERT … ON CONFLICT DO NOTHING` or
a pre-check inside `Seed`.

### Embedded seed data (`SeedFS` + `SeedPath`)

Single-binary deploys benefit from seeding from `//go:embed` data rather
than loose JSON files on disk:

```go
//go:embed seed/foods.json
var seedFoods embed.FS

app.Entity("foods", entity.EntityConfig{
    Fields:   []schema.Field{ /* … */ },
    SeedFS:   seedFoods,
    SeedPath: "seed/foods.json",
    Seed: func(ctx context.Context, db *sql.DB) error {
        raw, err := entity.SeedDataFromContext(ctx)
        if err != nil {
            return err
        }
        var rows []FoodRow
        if err := json.Unmarshal(raw, &rows); err != nil {
            return err
        }
        for _, r := range rows {
            // …INSERT…
        }
        return nil
    },
})
```

`entity.SeedDataFromContext(ctx)` returns the bytes pointed to by `SeedPath`
within `SeedFS`. The framework wires the context just before calling
`Seed`; hosts never need to attach it manually.

`App.Entity` panics at registration time if `SeedFS` is set but
`SeedPath` is empty — a misconfiguration that would otherwise silently
record the entity as seeded with empty data on first run.

### Observability

Attach a `*slog.Logger` so each seed emits structured lifecycle events:

```go
ctx := migrate.WithSeedLogger(context.Background(), logger)
// (the framework calls migrate.RunSeeds with the App's lifecycle ctx
// during App.Start, so this matters mostly for tests + custom flows)
```

Events: `seed ledger read` (once per RunSeeds), `seed start`, `seed
done` (with elapsed duration), `seed skip` (when the ledger already
records the entity), `seed failed` (on error). When no logger is
attached, events go to a discard handler.

## Blueprint entity shape

Inside a `gofastr.yml` blueprint, each entry in the `entities:` list maps
onto the `EntityDeclaration` fields below. The same field-type vocabulary
applies whether you write the entity in Go (`EntityConfig`) or in a blueprint:

```yaml
entities:
  - name: posts
    table: posts
    soft_delete: true
    multi_tenant: false
    owner_field: user_id
    access:
      read: posts:read
      create: posts:write
      update: posts:write
      delete: posts:admin
    public: false   # default; see "Default CRUD authentication" below
    crud: true
    mcp: true
    fields:
      - name: title
        type: string
        required: true
        max: 200
      - name: body
        type: text
      - name: status
        type: enum
        values: [draft, published]
        default: draft
      - name: author_id
        type: relation
        to: users
```

### Field keys

Each entry under `fields:` accepts:

| Key | Type | Meaning |
|---|---|---|
| `name` | string | Column name (required). |
| `type` | string | One of the field types above (`string`, `text`, `int`, `float`, `decimal`, `bool`, `enum`, `date`, `timestamp`, `uuid`, `json`, `image`, `file`, `relation`). |
| `required` | bool | NOT NULL + presence validation. |
| `unique` | bool | Unique constraint on the column. |
| `default` | scalar | Default value. |
| `max` / `min` | number | Length (strings) or value (numbers) bounds. |
| `values` | list | Allowed values for `type: enum`. |
| `pattern` | string | Regex the value must match (validated on write). |
| `auto_generate` | string | Auto-populate strategy, e.g. `uuid` on an id column — the generated field never appears in write forms. |
| `read_only` | bool | Accepted from the DB/generator but silently skipped on client writes (create/update). Server code can persist it by wrapping the context with `crud.WithServerWrites` on the in-process API. |
| `hidden` | bool | Excluded from generated UI grids, forms, MCP tool schemas, AND from API responses; silently skipped on client create/update. Server code can persist it via `crud.WithServerWrites` (the value is stored but still not returned — `visibleFields` shapes the projection). |
| `to` | string | For `type: relation`, the target entity. |

Relation *blocks* (the `relations:` list, distinct from a `relation`
field) take `type` (`belongs_to`, `has_many`, `has_one`), `name`,
`entity`, and `foreign_key`.

`owner_field` mirrors `EntityConfig.OwnerField` — set it to the column
that holds the row owner's id (e.g. `user_id`) and the blueprint-declared
entity gets the same per-user auto-CRUD scoping as a Go-declared one
(see **Per-user scoping** below). Omit the key to keep pre-existing
behaviour. `gofastr generate --from=gofastr.yml` emits `OwnerField:` into
the generated `app.Entity(...)` registration, so the scoping survives code
generation.

`access` mirrors `EntityConfig.Access` (`framework.AccessControl`) — the
per-operation RBAC permission required by auto-CRUD. Keys are `read`
(List + Get), `create`, `update`, and `delete`; each value is a permission
string such as `posts:write`. A blank or omitted key leaves that operation
un-gated by RBAC (owner and tenant scoping still apply); omit the whole map
for no RBAC gating at all. When set, auto-CRUD refuses a request whose
context lacks the permission with **403** — the roles + policy must be in
the request context first: mount `framework.AccessMiddleware` with a policy
(`battery/auth` only supplies the authenticated user whose roles you feed
into it; it does not satisfy the gate by itself — see
[access-control](access-control.md)). `gofastr generate
--from=gofastr.yml` emits the map as `Access: framework.AccessControl{...}`
in the generated `app.Entity(...)` registration, so blueprint-declared
entities get the same fail-closed enforcement as Go-declared ones.

### Default CRUD authentication

Auto-CRUD is secure-by-default. An entity that declares **none** of
`owner_field`, `access`, or `public` requires an authenticated session for
**every** operation — List/Get/Create/Update/Delete all refuse an
anonymous caller with **401**. Before this, a plain entity with no
`owner_field`/`access` had zero enforcement: an anonymous `POST
/api/<entity>` returned 201 and persisted the row.

`owner_field` and `access` already take over gating for an entity — set
either one and this default session requirement no longer applies (their
own contracts, described above and in **Per-user scoping**, govern the
entity instead — including any operation an `access:` block leaves
un-gated, "as today").

For an entity that's genuinely meant to be open to anonymous callers — a
public contact form, a blog's comments, a newsletter signup — declare
`public: true`. This is a full, deliberate opt-out: every operation,
reads AND writes, is reachable anonymously, matching the framework's
pre-secure-by-default behaviour for that entity. It is **not** a partial
"reads only" relaxation — an entity that wants public reads but gated
writes uses `access:` instead (a blank `read:` + a real `create:`
permission leaves List/Get open while Create still requires the
permission):

```yaml
entities:
  - name: announcements
    public: true    # anonymous read AND write — a public entity
    fields:
      - name: title
        type: string
        required: true

  - name: posts
    access:
      create: posts:write   # blank read: + a real create: → public reads, gated writes
    fields:
      - name: title
        type: string
        required: true
```

`gofastr generate` prints a warning at the end of every run listing every
entity left publicly readable/writable (i.e. every `public: true`
declaration), so the open surface of a generated app is never silent.

`gofastr dev`'s auto-registered entity MCP tools (and any `mcp: true`
entity in production) dispatch through the same router + middleware chain
as REST, so they inherit this session requirement automatically — no
separate MCP-level auth wiring is needed. An anonymous MCP `posts_create`
call against a non-public entity is refused exactly like the REST route.

```yaml
entities:
  - name: posts
    owner_field: user_id    # the column is auto-created; no field needed
    fields:
      - name: title
        type: string
        required: true
```

You do **not** declare the owner column as a field: `gofastr generate`
synthesizes it as a hidden string column, so AutoMigrate creates it while it
stays out of generated forms and tables. The framework manages it end to end —
`CreateOne` stamps it from the current user and every read scopes by it. (A
field you *do* declare with the owner's name always wins and is left untouched.)
`owner_field` alone satisfies the per-user PII gate, so it does not need an
`access:` block; add one only when you also want role-based API gating on top of
ownership:

```yaml
entities:
  - name: posts
    owner_field: user_id
    access:
      read: posts:read      # List + Get
      create: posts:write
      update: posts:write
      delete: posts:admin
    fields:
      - name: title
        type: string
        required: true
```

Supported field types: `string`, `text`, `int`, `float`, `decimal`, `bool`,
`enum`, `uuid`, `timestamp`, `date`, `json`, `relation`, `image`, and `file`.

A `relation` field with a `to` target (e.g. a field named `author_id`, type
`relation`, `to: users`) declares a *BelongsTo*: the field's own column
holds the foreign key. `Define` derives a matching `Config.Relations` entry
automatically, so AutoMigrate emits the FK constraint and `?include=author_id`
eager-loads the related row — you do not have to declare the relation twice. An
explicit relation you declare for the same name always wins and is never
overwritten. Has-many relations (`many: true`) keep their FK on the *other*
table and must be declared explicitly via `HasMany`/`Relations`.

### Column naming

The `name` you put in a field declaration is the SQL column name verbatim —
case preserved, no snake-casing applied. A field named `flareVerdict` creates
a column called `flareVerdict`, not `flare_verdict`. The same name is also the
JSON property on REST responses when the app's JSON casing is left at the
default (`camel`). Set it app-wide with
`framework.WithConfig(framework.AppConfig{JSONCase: crud.CaseSnake})`, or per
handler with `CrudHandler.WithJSONCase(crud.CaseSnake)`.

If you want snake_case columns, write them snake_case in the declaration:
`flare_verdict` → column `flare_verdict`. The framework never rewrites field
names; the only auto-casing happens at the JSON layer (via `AppConfig.JSONCase`
/ `CrudHandler.WithJSONCase`), which converts column names to/from `camel` or
`snake` on the wire and leaves the underlying column untouched.

Rule of thumb: name fields in whatever case you want the column to be in.
camelCase is the convention used in the example apps; snake_case is the
SQL-traditional choice. Pick one per project and stick with it.

## Per-user scoping (`OwnerField`)

Set `EntityConfig.OwnerField` to the DB column that holds the row owner's
id, and auto-CRUD becomes per-user automatically:

| Operation | Behaviour with `OwnerField: "user_id"` |
|---|---|
| `GET /api/<entity>` (List)   | `WHERE user_id = <ctx user id>` injected into both the data and count queries. |
| `GET /api/<entity>/{id}` (Get) | `WHERE id = ? AND user_id = <ctx user id>`. Cross-user requests return 404. |
| `POST /api/<entity>` (Create) | `user_id` is stamped from the current request — clients can omit it (or send it; it's overwritten). |
| `PUT /api/<entity>/{id}` / `PATCH /api/<entity>/{id}` (Update) | UPDATE is scoped by owner. Cross-user requests return 404. |
| `DELETE /api/<entity>/{id}` (Delete) | DELETE is scoped by owner. Cross-user requests return 404. |

The owner id comes from `framework/owner.Get(ctx)`. Any battery that
registers an extractor wires this up — `battery/auth` does so in
`init()`, pulling from `auth.GetCurrentUser(ctx).GetID()`. If no
extractor is registered, `OwnerField` is inert (no scoping, no
stamping) — so adding the field to an entity config in an app that
hasn't wired auth is harmless.

Pair with **session middleware** so cookie-authenticated requests
appear as a User in context:

```go
app.Use(auth.SessionMiddleware(mgr))
```

JWT-authenticated requests (via `auth.RequireAuth`) already populate
the User in context.

For blueprint-declared entities this rule is lint-enforced: an
auto-exposed entity (`crud` defaults on, or `mcp: true`) with PII-shaped
field names and no `owner_field` / `access` / `multi_tenant` while
`app.auth` is disabled is an **error** from `gofastr validate`, a
prominent warning from `gofastr generate`, and an `unscoped-pii` finding
from `gofastr audit lint`. See [blueprints](blueprints.md) → "Unscoped
PII".

### Letting a role read every owner's rows (`CrossOwnerRead`)

Owner scoping keeps each user's rows private, but some roles *should* see
every owner's data on **reads** — a staff dashboard, a support tool, an
analytics aggregate over user-owned rows. `CrossOwnerRead` is the
declarative knob for that: name an RBAC permission, and when the request
context holds it, owner scoping is lifted for List/Get/Count (HTTP and
in-process) on that entity. Writes stay owner-scoped, always.

```go
app.Entity("tickets", entity.EntityConfig{
    Fields:         []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "subject", Type: schema.String}},
    OwnerField:     "user_id",
    CrossOwnerRead: "tickets:read:all", // staff who hold this can read every user's tickets
})
```

```yaml
# gofastr.yml
entities:
  - name: tickets
    owner_field: user_id
    cross_owner_read: tickets:read:all
    fields:
      - {name: subject, type: string}
```

Grant the permission to the role that should see across owners:

```go
policy := access.NewRolePolicy()
policy.Grant("staff", "tickets:read:all")
app.Use(access.Middleware(policy, func(ctx context.Context) []string {
    // resolve roles from the authenticated user
    return []string{"staff"}
}))
```

The admin battery's wildcard grant (`*`) passes any permission check, so
an entity opted in via `CrossOwnerRead` is fully visible in the back
office automatically.

**Fail-closed.** When no access policy is in the request context (an
un-wired request, or the caller's roles don't include the permission),
owner scoping stays **on** — the widening never happens implicitly. This
is the secure-by-default answer: opt in explicitly, and only when the
policy says yes.

**Read-only.** `CrossOwnerRead` never touches Create/Update/Delete —
those stay owner-scoped. A staff member can *see* every ticket but
cannot PUT/PATCH/DELETE another user's row through the auto-CRUD surface.
Cross-user writes still return 404. Multi-tenant isolation is also
preserved: a granted context in tenant A never sees tenant B rows.

Requires `OwnerField` (it only makes sense on an owner-scoped entity);
`entity.Define` panics when `CrossOwnerRead` is set without it, and the
blueprint decoder returns a validation error for the same mismatch.

### Reading across owners (`owner.AllowCrossOwner`) — in-process escape hatch


Owner scoping is correct for user-facing CRUD, but some
app-legitimate work is *inherently* cross-owner: computing "spots
remaining" for a class from `capacity − COUNT(bookings across ALL
members)`, or reading a whole waitlist to promote the oldest entry (which
belongs to another member). Those aggregates can't be expressed through a
per-user-scoped read.

`owner.AllowCrossOwner(ctx)` is the sanctioned escape. It returns a
context that lifts owner scoping for the **in-process Go CrudHandler
methods** — `ListAll`, `CountAll`, `GetOne`, and (because they share the
scope helpers) the mutate-by-id methods. It is the owner-side twin of
`tenant.AllowCrossTenant` for multi-tenant entities.

```go
import "github.com/DonaldMurillo/gofastr/framework/owner"

// "Spots remaining" for a class — a count over EVERY member's bookings,
// not just the caller's. bookings.OwnerField == "user_id".
func spotsRemaining(ctx context.Context, bookings *crud.CrudHandler, classID string, capacity int) (int, error) {
    taken, err := bookings.CountAll(owner.AllowCrossOwner(ctx), crud.ListOptions{
        Filters: []filter.ParsedFilter{{Field: "class_id", Op: "eq", Value: classID}},
    })
    if err != nil {
        return 0, err
    }
    return capacity - taken, nil
}
```

**Reach for this only when the cross-owner read is the whole point** —
an aggregate, a queue, an admin lookup. It is NOT a convenience for
"I couldn't figure out the scoped API"; the default scoped read is what
you want for anything a user sees about *their own* data. Two hard rules:

- **Server-side Go only.** The context key is unexported, so the
  auto-generated HTTP CRUD endpoints have **no path** to this marker —
  they stay owner-scoped, always. Never derive it from a header, query
  param, or request body, and never plumb it onto the request context of
  an auto-CRUD route.
- **No built-in permission check.** `AllowCrossOwner` lifts the *owner*
  requirement; it does not authorize anything. Gate the caller yourself
  (a route access rule, an `access.Can` check, or the fact that it only
  runs inside trusted server code) before you widen the scope.

### Auth entities are NOT auto-private

When you register the `users` / `sessions` entities for `battery/auth`,
use the pre-built configs so they don't get exposed via REST or MCP:

```go
app.Entity("users",    auth.UserEntityConfig())    // CRUD=false, MCP=false
app.Entity("sessions", auth.SessionEntityConfig()) // CRUD=false, MCP=false
```

`auth.UserEntityFields()` and `auth.SessionEntityFields()` remain for
hosts that want full control; the `*EntityConfig()` helpers are the
safer default.

## Free-text search (`SearchFields` + `?q=`)

Set `SearchFields` to a slice of DB column names and List requests
carrying `?q=<term>` perform a multi-field, case-insensitive free-text
search across them:

```go
app.Entity("articles", entity.EntityConfig{
    Fields:       []schema.Field{{Name: "title", Type: schema.String}, {Name: "body", Type: schema.Text}},
    SearchFields: []string{"title", "body"},
})
```

```yaml
# gofastr.yml
entities:
  - name: articles
    search_fields: [title, body]
    fields:
      - {name: title, type: string}
      - {name: body, type: text}
```

A request like `GET /api/articles?q=go%20concurrency` tokenizes the term
on whitespace (deduped, capped at 8 tokens), and AND-composes one
`LOWER(col) LIKE '%token%'` condition per token across the declared
fields. Every token must match (AND); within one token, any field may
match (OR). The conditions AND safely with owner, tenant, and soft-delete
scopes — the query builder wraps each WHERE clause in parens.

**Case contract.** `LOWER()` is ASCII-only on SQLite and locale-aware on
Postgres, so matching is ASCII-case-insensitive everywhere. Unicode case
folding is a Postgres bonus. The token is lowercased before building the
LIKE pattern so the comparison is consistent across dialects.

**Back-compat.** An entity WITHOUT `SearchFields` ignores `?q=` exactly
as before — no behavioural change.

**The `q`-column edge case.** An entity WITH `SearchFields` that also
has a physical column named `q`: plain `?q=value` means **search** (the
OpEq filter on the `q` column is dropped). Suffixed ops (`?q_like=`,
`?q_gt=`, …) still filter the column normally.

Column names must be known, non-Hidden, and String/Text-typed;
`entity.Define` panics otherwise (the blueprint decoder returns a
validation error). A Hidden column would turn `?q=` into a
value-disclosure oracle — the same rationale as ParseFilters' hidden
stripping.

In-process callers get the same behaviour via `ListOptions.Search`:

```go
rows, err := handler.ListAll(ctx, crud.ListOptions{Search: "go concurrency"})
```

Setting `Search` on an entity without `SearchFields` returns an error
(fail loud, matching the unknown-sort policy).

## Nested predicate filters (`?where=`)

The flat `?field_op=value` params AND-compose. When you need **boolean
logic** — OR-groups, nested AND/OR — pass a predicate tree as a JSON
value in `?where=`:

```
GET /api/tickets?where={"or":[
  {"field":"status","value":"open"},
  {"and":[
    {"field":"priority","op":"eq","value":"high"},
    {"field":"assignee","value":"me"}
  ]}
]}
```

compiles to `WHERE ((status = $1) OR ((priority = $2) AND (assignee =
$3)))`. A node is either a **leaf** (`{"field","op","value"}`, `op`
defaults to `eq`; use `"values":[...]` or a comma string with
`op:"in"`) or a **group** (`{"and":[...]}` / `{"or":[...]}`).

Operators are the same set as the flat params: `eq, gt, lt, gte, lte,
like, in`.

**Safety.** Every field is validated against the entity's schema
(Hidden fields rejected — the same value-disclosure-oracle rationale as
flat filters); every value is a bound placeholder, never string-
interpolated; unknown fields/operators, malformed JSON, or a tree
exceeding the depth (8) or node (64) bounds return **400**. The whole
tree compiles to **one** parenthesized WHERE clause, so it AND-composes
with owner, tenant, and soft-delete scopes exactly like `?q=` — a user
OR-group can never widen past those scopes. `?where=` combines (AND)
with any flat `?field_op=` params on the same request.

## Code Generation

Generate Go from a `gofastr.yml` blueprint:

```bash
gofastr generate --from=gofastr.yml
```

This scaffolds the owned entity package into `entities/` at the module root:

- `register.go` with `RegisterAll(app *framework.App)` — the fixed seam.
  It carries no entity name, so adding an entity never edits it.
- one `<entity>.go` per declared entity: model struct, typed column
  constants, typed repository, lifecycle subscriptions, and its own
  `app.Entity(...)` registration that self-registers via `init()`. A new
  entity is a new file; existing files are never rewritten.
- `client/client.go` with a standalone Go HTTP client. Setting the
  client's `Token` field sends it as `Authorization: Bearer <token>` on
  every request — pair with a scoped API token
  ([auth](auth.md#service-accounts--scoped-api-tokens)); leave empty for
  public or cookie-authenticated APIs.

A blueprint that declares `app.module` also emits a flat `package main` at the
root (`main.go` plus `app.go`, `screens_register.go`, one `screen_<name>.go`
per screen, and `stubs.go` for endpoint/seed stubs). These are owned Go you
read, edit, and commit — no `DO NOT EDIT` header. See
[Blueprints](blueprints.md) for the full blueprint shape, including the
[generated screen file layout](blueprints.md#generated-screen-files). To add
in-page dynamic behavior to those screens (sort, paginate, mutate without a
reload), build islands — the cookbook is
[interactive-patterns](interactive-patterns.md).

Useful flags:

- `--from=<blueprint.yml>` selects the blueprint to generate from (required).
- `--dry-run` lists generated files without writing.
- `--json` emits machine-readable output.
- `--out=<dir>` scaffolds into a subpackage instead of the module root (also
  settable as `app.output_dir` in the blueprint) — useful for monorepos and
  examples that host their own Go test package.
- `--force` overwrites existing files. `generate` is one-shot: with no
  `--force` it refuses to write into a directory that already holds any target
  file, listing the conflicts, rather than clobbering owned code.
- `--add` writes only the files that don't already exist, never overwriting.
  Pass a partial yml (e.g. just new entities) to add pieces to an existing
  project. Entity declaration orders continue after the existing set. See
  [Additive generation](blueprints.md#additive-generation---add).

### Scaffold subcommands

For a fast stub with no yml, `generate entity|screen <name>` synthesizes a
minimal one-piece fragment and runs it through the same additive path as
`--add` — so the new entity/screen continues the project's declaration order,
existing files are never overwritten, and `--out`, `--dry-run`, and `--json`
work as above. `--force` and `--add` are rejected (scaffolding is additive):

* `gofastr generate entity posts` — `entities/posts.go` with one placeholder
  `name` field (a required string) you rename; CRUD stays default.
* `gofastr generate screen contact` — `screen_contact.go` at `/contact` with
  a heading + stub paragraph whose `Render` you replace.

See [Quick scaffolds](blueprints.md#quick-scaffolds-generate-entityscreen) in
the Blueprints guide for the relationship between stubs and full yml.

For arbitrary configured generators (not a full app blueprint), use a
`gofastr.codegen.yml` extension config. See [Codegen](codegen.md) for
config discovery, the extension protocol, and manifest-based cleaning.

## Mounting under a prefix (`APIPrefix`)

By default an entity's CRUD routes mount at its bare name — `GET /posts`,
`POST /posts/_batch`, `GET /posts/_events`. To move every auto-CRUD route under
a path prefix (the usual `/api`), set `AppConfig.APIPrefix` (or the
`framework.WithAPIPrefix` option):

```go
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithConfig(framework.AppConfig{APIPrefix: "/api"}),
)
app.Entity("posts", framework.EntityConfig{ /* … */ })
// → GET /api/posts, POST /api/posts/_batch, GET /api/posts/_events
```

This is the clean fix when a page/screen wants the same path as an entity (a
home page at `/posts` vs. the `posts` CRUD): put the data routes under `/api`
and let the UI own the bare paths. The generated OpenAPI spec expresses the
prefix via its server URL, so `/openapi.json` stays consistent, and **MCP tool
names are unchanged** (`posts_list`, not `api_posts_list`). `GroupEntity`
routes are unaffected — a route group owns its own prefix. Leaving `APIPrefix`
empty keeps the bare mounts, so adding it is never a breaking change.

> **Common mistake:** registering a screen at `/posts` while a `posts` entity
> mounts there too. Without `APIPrefix` you'll get a route-conflict panic naming
> the colliding path; set `APIPrefix` (or mount the page elsewhere) to resolve it.

### CRUD verbs and response envelopes

Each writable entity mounts `POST /<entity>`, `PUT /<entity>/{id}`, and
`PATCH /<entity>/{id}`. Both PUT and PATCH are sparse: validation and SQL
updates apply only to the fields present in the JSON body, so neither verb
nulls an omitted column — they are wired to the same update path and differ
only in the HTTP method clients use to express intent. Both use the same
access, owner and tenant scopes, update hooks, audit pre-image, and
transaction path. The generated typed client exposes both `Update<Entity>`
(PUT) and `Patch<Entity>` (PATCH); the MCP update tool uses PATCH. Because
PATCH must distinguish "field absent" from "field set to its zero value"
(`false`, `0`, `""`), `Patch<Entity>` takes a dedicated `<Entity>Patch`
struct whose fields are pointers (`*bool`, `*int`, …): a `nil` field is
omitted from the body (left untouched), while a non-nil pointer sets the
field even when it points at a zero value. `Update<Entity>` and
`Create<Entity>` keep the value-typed `<Entity>Input`.

Every successful single-record response has one stable envelope:

```json
{"data":{"id":"p1","title":"Hello"}}
```

This applies to create (`201`), get (`200`), PUT (`200`), and PATCH (`200`).
Lists keep `{"data":[...]}` plus pagination metadata. Error and DELETE
responses are unchanged.

## MCP Tools

When an entity sets `"mcp": true`, GoFastr registers CRUD tools:

- `{entity}_list`
- `{entity}_get`
- `{entity}_create`
- `{entity}_update`
- `{entity}_delete`

The tools use the same validation and CRUD handler behavior as HTTP routes.

In the dev loop (`gofastr dev`; opt-out `GOFASTR_DEV_MCP=0`) these tools
register for **every CRUD-enabled entity** — no per-entity `mcp: true`
needed — so the local agent can read and write app data. Production
keeps the explicit flag as the only path. Entities with `crud: false`
(e.g. the auth battery's users/sessions configs) are never implied:
MCP tools dispatch through the CRUD routes, so no routes means no
tools, in dev or out.

## Custom Endpoints

Custom endpoint handlers are Go behavior and should be registered from Go code:

```go
app.Entity("posts", framework.EntityConfig{
    Fields: []schema.Field{{Name: "title", Type: schema.String}},
    Endpoints: []framework.Endpoint{{
        Method: http.MethodPost,
        Path: "{id}/publish",
        Handler: publishHandler,
        MCP: true,
        Name: "posts_publish",
        MCPHandler: publishTool,
    }},
})
```

Endpoint paths can be absolute (`/posts/{id}/publish`) or relative to the
entity table path (`{id}/publish`). Both `{id}` and `:id` parameter syntax are
accepted.

Note the auth asymmetry: the HTTP `Handler` runs behind the route
middleware chain, but the `MCPHandler` twin is invoked directly — no
route middleware, so no per-caller auth of its own. If the HTTP side is
protected, gate the MCP side to match by wrapping it:
`MCPHandler: mcp.Gated(auth.MCPRole("admin"), publishTool)` — see
[plugins](plugins.md) → MCP tool gating.

### Typed input/output schemas

By default a custom endpoint is shapeless to generators: OpenAPI emits a bare
`{type: object}` request/response and the MCP tool advertises an empty
`{type: object}` input schema — useless SDK stubs and agent tools. Describe the
request body and the success (200) response with the **optional** `InputSchema`
and `OutputSchema` fields. Both take `[]schema.Field` — the same representation
the entity's own CRUD schema is built from, so OpenAPI and the generated MCP
tool consume one source:

```go
Endpoints: []framework.Endpoint{{
    Method: http.MethodPost,
    Path:   "{id}/publish",
    Handler: publishHandler,
    MCP:     true,
    MCPHandler: publishTool,
    InputSchema: []schema.Field{
        {Name: "notify", Type: schema.Bool, Required: true},
    },
    OutputSchema: []schema.Field{
        {Name: "published_at", Type: schema.String},
    },
}}
```

With these set, the OpenAPI operation gains a typed `requestBody` (non-GET only)
and a typed 200 response, and the MCP tool advertises `InputSchema` as its tool
input schema. Both fields are optional: leave them `nil` to keep the historical
`{type: object}` behaviour byte-for-byte. `InputSchema` is ignored on `GET`/
`HEAD` endpoints, which carry no request body.

## Common mistakes

- **Exposing per-user data without `OwnerField`.** The warning at the
  top of this page is the #1 footgun: auto-CRUD with no `OwnerField`
  lets every authenticated user read (and write) every row. Set it on
  any entity holding per-user data — List/Get/Update/Delete scope to
  the current user and Create stamps the column automatically.
- **Reaching for `public: true` to fix a 401 in dev.** The 401 an
  anonymous entity returns by default (see **Default CRUD
  authentication** above) is the framework working as intended — the
  fix is almost always to send a session, not to declare the entity
  `public`. `public: true` opens BOTH reads and writes to anyone; use it
  only for content that's genuinely meant to be public (a contact form,
  a blog's comments), never as a quick way past a login wall during
  development.
- **Setting `OwnerField` in an app that never wires an owner
  extractor.** Without a registered extractor the field is inert — no
  scoping, no stamping, no error. Importing `battery/auth` registers
  one in `init()`; pair it with `auth.SessionMiddleware` so
  cookie-authenticated requests carry a user.
- **Setting `Access` and forgetting the policy middleware.** The CRUD
  gate is fail-closed: a context without the permission gets 403 — so
  without `framework.AccessMiddleware` (with a policy feeding roles
  into the context), *every* request to that operation 403s, including
  legitimate ones. `battery/auth` alone does not satisfy the gate.
- **Expecting a `relation` field to model has-many.** A relation field
  declares a BelongsTo — the FK lives in the field's own column, and
  the matching relation is derived for you. Has-many keeps its FK on
  the *other* table and must be declared explicitly via
  `HasMany`/`Relations`.
- **Writing a non-idempotent `Seed`.** The `_gofastr_seeded` ledger is
  best-effort: it survives normal restarts but cannot guarantee
  atomicity between your inserts and the ledger row. Use
  `INSERT … ON CONFLICT DO NOTHING` (or a pre-check) so a re-run is
  harmless.
