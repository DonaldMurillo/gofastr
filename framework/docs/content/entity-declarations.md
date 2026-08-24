# Entity declarations

> ⚠️ **Auto-CRUD is secure-by-default; per-user data still needs
> `OwnerField`.** An entity exposed via `app.Entity(...)` (or
> `app.GroupEntity(...)`) that declares none of `OwnerField`, `Access`,
> or `Public` requires an authenticated session for **every** operation:
> List/Get/Create/Update/Delete all 401 an anonymous caller. That
> closes anonymous read/write, but it does **not** scope rows by user:
> without `OwnerField`, every authenticated user still reads (and can
> overwrite) every other user's rows. For per-user data:
>
> ```go
> app.Entity("logs", entity.EntityConfig{
>     Fields: []schema.Field{ /* … */ },
>     Scope:  &entity.ScopeConfig{OwnerField: "user_id"}, // CRUD auto-scopes by current user; auto-stamps on Create
> })
> ```
>
> When `battery/auth` is imported, the framework's owner extractor is
> wired automatically: no extra setup needed. See the **Per-user
> scoping (`OwnerField`)** section below for details, and **Default CRUD
> authentication** below for the session-requirement contract (including
> the `Public` opt-out for genuinely public entities, such as a contact form, a
> blog's comments).

An entity is registered in Go with `app.Entity(name, framework.EntityConfig{…})`.

Related behavior is grouped so security and delivery choices are visible at a
glance:

```go
crud := true
app.Entity("tickets", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "title", Type: schema.String, Required: true},
    },
    Scope: &framework.ScopeConfig{
        OwnerField: "user_id",
        SoftDelete: true,
    },
    Pagination: &framework.PaginationConfig{
        CursorFields: []string{"created_at", "id"},
        MaxListLimit: 100,
    },
    Exposure: &framework.ExposureConfig{
        CRUD: &crud,
        MCP: true,
        Access: framework.AccessControl{Read: "tickets:read"},
    },
})
```

Blueprint entities accept the same `scope:`, `pagination:`, and `exposure:`
maps. In Go, the grouped sub-configs (`ScopeConfig`, `PaginationConfig`,
`ExposureConfig`) are the only form; the historical flat `EntityConfig`
fields were removed. In a blueprint YAML you may still use the flat
shorthand keys, but declaring a flat key and its grouped key with
*different* values is a hard decode error, not a silent precedence rule.
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

## What registering an entity publishes

That declaration has no `Exposure` block, and it still mounts the whole REST
surface: `GET /posts`, `GET /posts/{id}`, `POST /posts`, `PUT` and `PATCH` on
`/posts/{id}`, `DELETE /posts/{id}`, the batch endpoints on `/posts/_batch`,
the `/posts/_events` stream, and `/posts/llm.md`. Route generation is **on by
default**: `Exposure.CRUD` is a `*bool`, and nil means generate. Omitting the
block is not "declare no surface"; it is "take the default surface".

MCP tools are the opposite. They are **off by default** and need
`Exposure: &framework.ExposureConfig{MCP: true}`. The one exception is the dev
loop: under `gofastr dev`, every CRUD-enabled entity also serves its data
tools so a local agent can read and write app data without a per-entity
opt-in. A production binary registers none of them without the explicit flag.

To publish nothing at all, say so:

```go
noCRUD := false
app.Entity("posts", framework.EntityConfig{
    Fields:   []schema.Field{ /* … */ },
    Exposure: &framework.ExposureConfig{CRUD: &noCRUD}, // no routes, no MCP tools
})
```

The entity is still registered, migrated, and usable from Go
(`app.CrudHandler("posts")` builds a handler from the registry whether or not
routes were mounted, and hooks and typed queries work as usual); it just has
no HTTP or MCP surface. Setting `MCP: true` alongside `CRUD: false` is a
registration error, not a silent mismatch: MCP tools dispatch through the
routes.

Default-on routes are not default-open routes. Every one of them refuses an
anonymous caller with 401 unless the entity declares `OwnerField`, `Access`,
or `Public`. See **Default CRUD authentication** below, and note the limit
called out in the banner at the top of this page: a session requirement is
not row scoping. Without `OwnerField`, every authenticated user reads and can
overwrite every other user's rows.

The same entity shape can also be **declared in a `gofastr.yml` blueprint**
and emitted as Go by the CLI; see [Blueprints](blueprints.md), the single
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

`app.Entity(name, config)` **panics** on a misconfiguration: fail-fast,
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

Registration is atomic with respect to configuration errors: every
check that can reject a declaration runs before the registry, router,
or MCP server is touched. A rejected declaration leaves no registry
entry, no route, and no MCP tool, and a corrected retry under the same
name succeeds, which is the property the authoring loop above depends on.

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
a pre-check inside `Seed`. Across replicas, the framework serializes
the seed phase behind a Postgres advisory lock (distinct from the
migration lock) so only one replica runs an entity's Seed for a given
boot race; the ledger then makes it run once globally.

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
`SeedPath` is empty: a misconfiguration that would otherwise silently
record the entity as seeded with empty data on first run.

### Observability

Attach a `*slog.Logger` so each seed emits structured lifecycle events:

<!-- gofastr:compile
stmt: _ = ctx
import migrate "github.com/DonaldMurillo/gofastr/framework/migrate"
import "context"
import "log/slog"
var logger = slog.Default()
-->
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
    read_scope:      # which ROWS a caller reads; see "Row-level read scoping"
      filter:
        - field: status
          op: eq
          value: published
    public: false   # default; see "Default CRUD authentication" below
    crud: true
    mcp: true
    renames:        # old column: new column; see migrations.md
      headline: title
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

### Booleans that gate access are not guessed

Most boolean keys accept anything YAML calls false-ish. Six do not, because
for them a mis-read value opens something up rather than closing it:
`scope.multi_tenant`, `scope.soft_delete`, a field's `hidden`, `no_query`,
`read_only`, and a screen's `access.auth`. These require a real `true` or
`false` and error on anything else.

The reason is YAML 1.2, which `core/yaml` implements: `yes`, `on`, `y`, and
`1` are **strings**, not booleans. Written `auth: yes`, the value used to
read as false and the screen was registered with no policy: publicly
reachable, with no error to say so. `multi_tenant: yes` dropped tenant
scoping the same way. Keys where false is the inert direction (`public`,
`mcp`, `crud`, `enabled`) keep the lax reading.

### Field keys

Each entry under `fields:` accepts:

| Key | Type | Meaning |
|---|---|---|
| `name` | string | Column name (required). |
| `type` | string | One of the field types above (`string`, `text`, `int`, `float`, `decimal`, `bool`, `enum`, `date`, `timestamp`, `uuid`, `json`, `image`, `file`, `relation`). |
| `required` | bool | NOT NULL + presence validation. |
| `unique` | bool | Unique constraint on the column. |
| `default` | scalar | Value written when the field is omitted on create. Checked against the field's own rules when the entity is registered; see "Defaults are validated at registration" below. |
| `max` / `min` | number | Length (strings) or value (numbers) bounds. |
| `values` | list | Allowed values for `type: enum`. |
| `pattern` | string | Regex the value must match (validated on write). |
| `auto_generate` | string | Auto-populate strategy: `uuid` (random UUID v4, the default `id`), `increment` (database-assigned integer, `SERIAL` on Postgres, `INTEGER PRIMARY KEY` rowid alias on SQLite; the column is omitted from INSERT so the sequence/rowid assigns it), or `timestamp`. The generated field never appears in write forms. |
| `read_only` | bool | Accepted from the DB/generator but silently skipped on client writes (create/update). Server code can persist it by wrapping the context with `crud.WithServerWrites` on the in-process API. |
| `hidden` | bool | Excluded from generated UI grids, forms, MCP tool schemas, AND from API responses; silently skipped on client create/update. Server code can persist it via `crud.WithServerWrites` (the value is stored but still not returned; `visibleFields` shapes the projection). |
| `no_query` | bool | Returned in responses, but rejected by filters, `?sort=` (including alongside `?cursor=`), `?where=`, `?q=` search, the DSL, and nested `?rel.field=`. Rejected at generate time in `search:`, `filters:`, a `stat_card` `source.filter` or summed `source.field`, and a chart `group_by`; `entity.Define` panics if it names one in `SearchFields` or a cursor field. For values the caller may only see in transformed form; see "Masked fields" below. |
| `to` | string | For `type: relation`, the target entity. |

### Defaults are validated at registration

A `default` is the value the create path writes when the request body omits
the field. It goes into the same column a client-sent value would, so it is
checked against the same field rules, `values`, `pattern`, `min`/`max`,
`required`, and the type itself, when the entity is registered.

A default that fails those rules fails the declaration. `app.Entity` panics
and `app.TryEntity` returns the error, both naming the field:

```go
{Name: "flags", Type: schema.JSON, Default: "draft"}
// entity "things": field "flags" has an invalid Default "draft": must be valid JSON
```

Before this check the mismatch was per-request and per-dialect. A create that
omitted `flags` returned 500 against a Postgres `JSONB` column (`invalid input
syntax for type json`) and stored `draft` unchanged in SQLite's `TEXT` column,
while a caller who *sent* `"draft"` got a 400 naming the field. The same holds
for an `enum` default outside `values` and a `required` string defaulted to
`""`.

Two spellings are accepted that a request body could not use, because a Go
declaration is not JSON:

- `decimal` written as a number. `{Type: schema.Decimal, Default: 0}` is
  equivalent to `Default: "0"`; both render `DEFAULT 0` in DDL and bind as a
  number on insert.
- `timestamp` and `date` written as a `time.Time`.

A default on an `auto_generate` field is not validated, because the create path
never writes it: the generated value takes that slot. It still becomes the
column's DDL `DEFAULT`.

### Masked fields

An `AfterGet` / `AfterList` hook can rewrite a field on the way out: a
card number to its last four digits, a note redacted for non-owners.
That changes what the caller reads. It does not change what the database
filtered and sorted on.

Left alone, the stored value is still a live column, so a caller
recovers it a character at a time from which rows come back:

```
GET /cards?number_like=4111    → 1 row   ┐ every response still
GET /cards?number_like=4112    → 0 rows  ┘ reads "****1111"
```

`no_query` closes that. The field stays in the response and the query
surface refuses it:

```yaml
fields:
  - name: number
    type: string
    no_query: true    # masked by a hook; never filterable or sortable
```

```
GET /cards?number_like=4111    → 400 field "number" cannot be filtered
```

Use `hidden` instead when the caller should not see the value at all:
`hidden` also removes the field from responses, and hides the fact that
the column exists. `no_query` is for when they must see *something*.

`no_query` refuses the query surface; it does not mask anything by
itself. The hook is what rewrites the value, so declare both, and
register the hook on `AfterGet` **and** `AfterList`; each response path
runs the one matching the shape it serves.

The admin's edit form reads the row twice and treats any column the hook
rewrites as write-only: rendered empty (or with an explicit
"— unchanged —" option on a checkbox, enum, or relation picker), left
alone unless you supply a value. It compares the two reads rather than
looking at `no_query`, so a hook that *transforms* rather than masks,
normalising a phone number or rounding a currency, also makes its columns
write-only in the admin. Do that work in `BeforeCreate`/`BeforeUpdate`
if the column should stay editable.

Relation *blocks* (the `relations:` list, distinct from a `relation`
field) take `type` (`belongs_to`, `has_many`, `has_one`), `name`,
`entity`, and `foreign_key`.

`owner_field` mirrors `Scope.OwnerField`: set it to the column
that holds the row owner's id (e.g. `user_id`) and the blueprint-declared
entity gets the same per-user auto-CRUD scoping as a Go-declared one
(see **Per-user scoping** below). Omit the key to keep pre-existing
behaviour. `gofastr generate --from=gofastr.yml` emits `OwnerField:` inside a
`Scope: &framework.ScopeConfig{…}` block in the generated `app.Entity(...)`
registration, so the scoping survives code generation.

`access` mirrors `Exposure.Access` (`framework.AccessControl`): the
per-operation RBAC permission required by auto-CRUD. Keys are `read`
(List + Get), `create`, `update`, and `delete`; each value is a permission
string such as `posts:write`. A blank or omitted key leaves that operation
un-gated by RBAC (owner and tenant scoping still apply); omit the whole map
for no RBAC gating at all. When set, auto-CRUD refuses a request whose
context lacks the permission with **403**: the roles + policy must be in
the request context first: mount `framework.AccessMiddleware` with a policy
(`battery/auth` only supplies the authenticated user whose roles you feed
into it; it does not satisfy the gate by itself; see
[access-control](access-control.md)). `gofastr generate
--from=gofastr.yml` emits the map as `Access: framework.AccessControl{...}`
inside an `Exposure: &framework.ExposureConfig{…}` block in the generated
`app.Entity(...)` registration, so blueprint-declared entities get the same
fail-closed enforcement as Go-declared ones.

### Default CRUD authentication

Auto-CRUD is secure-by-default. An entity that declares **none** of
`owner_field`, `access`, or `public` requires an authenticated session for
**every** operation: List/Get/Create/Update/Delete all refuse an
anonymous caller with **401**. Before this, a plain entity with no
`owner_field`/`access` had zero enforcement: an anonymous `POST
/api/<entity>` returned 201 and persisted the row.

`owner_field` and `access` already take over gating for an entity: set
either one and this default session requirement no longer applies (their
own contracts, described above and in **Per-user scoping**, govern the
entity instead, including any operation an `access:` block leaves
un-gated, "as today").

For an entity that's genuinely meant to be open to anonymous callers, such as a
public contact form, a blog's comments, or a newsletter signup, declare
`public: true`. This is a full, deliberate opt-out: every operation,
reads AND writes, is reachable anonymously, matching the framework's
pre-secure-by-default behaviour for that entity. It is **not** a partial
"reads only" relaxation: an entity that wants public reads but gated
writes uses `access:` instead (a blank `read:` + a real `create:`
permission leaves List/Get open while Create still requires the
permission):

```yaml
entities:
  - name: announcements
    public: true    # anonymous read AND write: a public entity
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
declaration), so a generated app's public entities are never a silent
surprise.

`gofastr dev`'s auto-registered entity MCP tools (and any `mcp: true`
entity in production) dispatch through the same router + middleware chain
as REST, so they inherit this session requirement automatically: no
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
stays out of generated forms and tables. The framework manages it end to end:
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
eager-loads the related row, so you do not have to declare the relation twice. An
explicit relation you declare for the same name always wins and is never
overwritten. Has-many relations (`many: true`) keep their FK on the *other*
table and must be declared explicitly via `HasMany`/`Relations`.

### Column naming

The `name` you put in a field declaration is the SQL column name verbatim:
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

Set `Scope.OwnerField` to the DB column that holds the row owner's
id, and auto-CRUD becomes per-user automatically:

| Operation | Behaviour with `OwnerField: "user_id"` |
|---|---|
| `GET /api/<entity>` (List)   | `WHERE user_id = <ctx user id>` injected into both the data and count queries. |
| `GET /api/<entity>/{id}` (Get) | `WHERE id = ? AND user_id = <ctx user id>`. Cross-user requests return 404. |
| `POST /api/<entity>` (Create) | `user_id` is stamped from the current request; clients can omit it (or send it; it's overwritten). |
| `PUT /api/<entity>/{id}` / `PATCH /api/<entity>/{id}` (Update) | UPDATE is scoped by owner. Cross-user requests return 404. |
| `DELETE /api/<entity>/{id}` (Delete) | DELETE is scoped by owner. Cross-user requests return 404. |

The owner column is created for you: when no field named like the
`OwnerField` column is declared, `entity.Define` injects it as a hidden
string column: AutoMigrate creates it, Create stamps it, and it stays
out of responses, forms, and the OpenAPI spec. A field you *do* declare
with that name always wins and is left untouched.

The owner id comes from `framework/owner.Get(ctx)`. Any battery that
registers an extractor wires this up: `battery/auth` does so in
`init()`, pulling from `auth.GetCurrentUser(ctx).GetID()`. If no
extractor is registered, `OwnerField` is inert (no scoping, no
stamping), so adding the field to an entity config in an app that
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
every owner's data on **reads**: a staff dashboard, a support tool, an
analytics aggregate over user-owned rows. `CrossOwnerRead` is the
declarative knob for that: name an RBAC permission, and when the request
context holds it, owner scoping is lifted for List/Get/Count (HTTP and
in-process) on that entity. Writes stay owner-scoped, always.

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import "github.com/DonaldMurillo/gofastr/framework/entity"
import "github.com/DonaldMurillo/gofastr/core/schema"
-->
```go
app.Entity("tickets", entity.EntityConfig{
    Fields: []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "subject", Type: schema.String}},
    Scope:  &entity.ScopeConfig{
        OwnerField:     "user_id",
        CrossOwnerRead: "tickets:read:all", // staff who hold this can read every user's tickets
    },
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

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/access"
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import "context"
-->
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
owner scoping stays **on**: the widening never happens implicitly. This
is the secure-by-default answer: opt in explicitly, and only when the
policy says yes.

**Read-only.** `CrossOwnerRead` never touches Create/Update/Delete:
those stay owner-scoped. A staff member can *see* every ticket but
cannot PUT/PATCH/DELETE another user's row through auto-CRUD.
Cross-user writes still return 404. Multi-tenant isolation is also
preserved: a granted context in tenant A never sees tenant B rows.

Requires `OwnerField` (it only makes sense on an owner-scoped entity);
`entity.Define` panics when `CrossOwnerRead` is set without it, and the
blueprint decoder returns a validation error for the same mismatch.

### Reading across owners (`owner.AllowCrossOwner`): in-process escape hatch


Owner scoping is correct for user-facing CRUD, but some
app-legitimate work is *inherently* cross-owner: computing "spots
remaining" for a class from `capacity − COUNT(bookings across ALL
members)`, or reading a whole waitlist to promote the oldest entry (which
belongs to another member). Those aggregates can't be expressed through a
per-user-scoped read.

`owner.AllowCrossOwner(ctx)` is the sanctioned escape. It returns a
context that lifts owner scoping for the **in-process Go CrudHandler
methods**: `ListAll`, `CountAll`, `GetOne`, and (because they share the
scope helpers) the mutate-by-id methods. It is the owner-side twin of
`tenant.AllowCrossTenant` for multi-tenant entities.

```go
import "github.com/DonaldMurillo/gofastr/framework/owner"

// "Spots remaining" for a class: a count over EVERY member's bookings,
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

**Reach for this only when the cross-owner read is the whole point**:
an aggregate, a queue, an admin lookup. It is NOT a convenience for
"I couldn't figure out the scoped API"; the default scoped read is what
you want for anything a user sees about *their own* data. Two hard rules:

- **Server-side Go only.** The context key is unexported, so the
  auto-generated HTTP CRUD endpoints have **no path** to this marker:
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

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import "github.com/DonaldMurillo/gofastr/battery/auth"
-->
```go
app.Entity("users",    auth.UserEntityConfig())    // CRUD=false, MCP=false
app.Entity("sessions", auth.SessionEntityConfig()) // CRUD=false, MCP=false
```

`auth.UserEntityFields()` and `auth.SessionEntityFields()` remain for
hosts that want full control; the `*EntityConfig()` helpers are the
safer default.

## Row-level read scoping (`Exposure.ReadScope`)

`Exposure.Access` answers **whether** a caller may read an entity.
`Exposure.ReadScope` answers **which rows** they see. It exists for the
most ordinary content posture there is: anonymous visitors see published
rows, signed-in editors see drafts.

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import "github.com/DonaldMurillo/gofastr/framework/entity"
import "github.com/DonaldMurillo/gofastr/core/schema"
-->
```go
app.Entity("posts", entity.EntityConfig{
    Fields: []schema.Field{
        {Name: "status", Type: schema.String, Default: "draft"},
        {Name: "title", Type: schema.String},
    },
    Exposure: &entity.ExposureConfig{
        Public: true, // reads (and writes) are open; ReadScope narrows the rows
        ReadScope: &entity.ReadScopeConfig{
            Filter: []entity.RowPredicate{
                {Field: "status", Op: "eq", Value: "published"},
            },
            Unrestricted: "", // any signed-in caller reads every row
        },
    },
})
```

The predicates are conditions on the entity's **own columns**, and they
AND together. Each `RowPredicate` names a `Field`, an `Op` (`eq`, `neq`,
`in`, `not_in`; an empty `Op` means `eq`), and either `Value` (single-value
ops) or `Values` (`in` / `not_in`). There is **no OR form** in this
version: a row must satisfy every predicate. Model "one of several
values" with `in`, not with multiple declarations.

`Unrestricted` decides who reads **every** row:

- **Non-empty**: it names an RBAC permission. A caller holding it reads
  every row; everyone else gets the filter. Like every permission check
  this is fail-closed: no policy in context means no widening.
- **Empty**: any caller **with a session** reads every row, and an
  anonymous caller gets the filter. That is the posture above, and it is
  a weak one on purpose: "any signed-in user" means exactly that, not an
  editor role. If drafts should be limited to editors, give the entity an
  `Unrestricted` permission and grant it to the editor role.

The filter applies to every read of the entity's own table: List, Get,
count, cursor and stream variants, the in-process API (`GetOne`,
`ListAll`, `CountAll`), typed queries, and, when the entity is the
target of a relation, `?include=`, eager loading, and `?rel.field=`
subqueries. A filtered-out row answers **404** on Get, not 403: the
caller must not learn it exists.

**Writes are not filtered.** Update, delete, and the upsert write do not
carry the predicate in this version; a write is authorized by the write
gates (owner, tenant, `Access`), not by the read posture. If callers can
write but not read everything, they can still modify a row they cannot
see.

`Access` does not close that on its own. An `Access` block checks whether the
caller holds a permission for the OPERATION, not whether they may touch a
particular row, so a caller with `update` can still update a row `ReadScope`
hides from them. To make write permission depend on the row, use
`Scope.OwnerField` or `Scope.MultiTenant`, which narrow the write itself, or
decide it in a `BeforeUpdate` / `BeforeDelete` hook, which sees the row.

A declaration is validated at registration and a bad one fails the app's
start (`app.Entity` panics, `app.TryEntity` returns the error, both naming
the field): `Field` must be a declared column, must not be `Hidden` (a
predicate on a masked column leaks its values through the row set), `Op`
must be one of the four, and `in`/`not_in` require a non-empty `Values`
while the single-value ops require an empty one. A typo must never
silently serve every row.

An entity with no `ReadScope` (or an empty `Filter`) is untouched: a
true no-op for every existing entity.

### Blueprint spelling (`read_scope:`)

In a `gofastr.yml` blueprint the same scope is declared under the entity,
beside `access:` (or nested under `exposure:`; the two spellings must
agree if both appear). This is the `posts` entity of
`examples/portfolio/gofastr.yml`, trimmed to the relevant keys:

```yaml
entities:
  - name: posts
    crud: true
    read_scope:
      filter:
        - field: status
          op: eq
          value: published
    access:
      read:            # blank: anonymous callers may read the entity
      create: content:write
      update: content:write
      delete: content:admin
    fields:
      - name: title
        type: string
        required: true
      - name: status
        type: enum
        values: [draft, published, archived]
        default: draft
```

Anonymous callers get published rows only; any caller with a session reads
every row. To name a permission instead, set `unrestricted:`:

```yaml
    read_scope:
      unrestricted: content:review
      filter:
        - field: approved
          op: eq
          value: "true"
```

A boolean predicate takes the string `"true"` or `"false"`; the framework
binds it as the column's bool type, exactly as `?approved=true` would.
Quote it in YAML so it stays a string.

The blueprint decoder validates the block where a typo matters, and fails
the generate with the YAML location: unknown keys at any level, an `op`
outside `eq`/`neq`/`in`/`not_in`, `value` and `values` together, a
predicate with no `field`, an undeclared or `Hidden` `field`, and a
`read_scope:` that declares neither a filter nor an `unrestricted`. The
framework re-checks at registration; the decode check exists so a broken
posture fails at `gofastr generate`, not at app boot.

One seed caveat: seed rows resolve `@entity.field=value` references
through the read-scoped list path. A seed row cannot reference a row the
scope hides: the lookup finds nothing and the insert fails its foreign
key at boot. Seed draft demo rows on published parents, or not
at all.

## Free-text search (`SearchFields` + `?q=`)

Set `SearchFields` to a slice of DB column names and List requests
carrying `?q=<term>` perform a multi-field, case-insensitive free-text
search across them:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import "github.com/DonaldMurillo/gofastr/framework/entity"
import "github.com/DonaldMurillo/gofastr/core/schema"
-->
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
scopes; the query builder wraps each WHERE clause in parens.

**Case contract.** `LOWER()` is ASCII-only on SQLite and locale-aware on
Postgres, so matching is ASCII-case-insensitive everywhere. Unicode case
folding is a Postgres bonus. The token is lowercased before building the
LIKE pattern so the comparison is consistent across dialects.

**Back-compat.** An entity WITHOUT `SearchFields` ignores `?q=` exactly
as before: no behavioural change.

**The `q`-column edge case.** An entity WITH `SearchFields` that also
has a physical column named `q`: plain `?q=value` means **search** (the
OpEq filter on the `q` column is dropped). Suffixed ops (`?q_like=`,
`?q_gt=`, …) still filter the column normally.

Column names must be known, non-Hidden, and String/Text-typed;
`entity.Define` panics otherwise (the blueprint decoder returns a
validation error). A Hidden column would turn `?q=` into a
value-disclosure oracle: the same rationale as ParseFilters' hidden
stripping.

In-process callers get the same behaviour via `ListOptions.Search`:

```go
rows, err := handler.ListAll(ctx, crud.ListOptions{Search: "go concurrency"})
```

Setting `Search` on an entity without `SearchFields` returns an error
(fail loud, matching the unknown-sort policy).

## Flat filters (`?field_op=value`)

The List endpoint filters on any known, non-Hidden column via query
params:

```
GET /api/tickets?status=open&priority_gte=2&assignee_in=me,you&sort=-created_at
```

| Suffix   | Operator                                   |
| -------- | ------------------------------------------ |
| _(none)_ | `=` (equals)                               |
| `_gt`    | `>`                                        |
| `_gte`   | `>=`                                       |
| `_lt`    | `<`                                        |
| `_lte`   | `<=`                                       |
| `_like`  | literal `contains` (`LIKE '%value%'`)      |
| `_in`    | `IN (…)`, comma-separated, capped at 1000 |

**Repeating `_in` unions.** `?tag_in=a,b&tag_in=c` matches all three.
Every occurrence of the key contributes; the 1000-entry cap
(`filter.MaxINListEntries`) counts the union, and the same cap applies to
relation-scoped lists like `?author.name_in=` (which are refused when the
related entity is owner-scoped or multi-tenant, unless the caller already holds
cross-owner or cross-tenant access for that axis; see
[access control](access-control.md)). A list over the cap is a
**400** naming the field and both counts, never a silent truncation,
for the same reason unknown filters fail closed below.

**Unknown filters fail closed (strict).** A misspelled or unrecognized
top-level filter, `?stauts=open`, or a suffixed op on a non-field like
`?scor_gt=5`, returns a **400** naming the bad key (with a "did you
mean" suggestion when a field is an unambiguous near-match), rather than
being silently dropped. Silently dropping a filter returns an
**unfiltered** result set: a broken client reads the whole table, and an
attacker's probe is indistinguishable from a real query. This mirrors the
existing fail-closed policy for `?sort=` and `?where=`.

Hidden columns are rejected with the **same** "unknown filter" wording as
a nonexistent column, so the error can't be used to distinguish
hidden-from-absent (the value-disclosure-oracle rationale; this also
holds for nested relation filters like `?author.password_hash_like=`).
Reserved list controls (`sort`, `page`, `limit`, `per_page`, `offset`,
`cursor`, `direction`, `where`, `fields`, `include`, `trashed`, `stream`,
`q`) and nested relation filters (dotted keys like `?author.name=alice`)
are never treated as unknown filters. A declared **column** whose name
collides with a control word (say a field literally named `stream`) still
filters: a known field always wins over the reserved-word skip, so it is
never silently swallowed.

**Custom params.** An endpoint that reads its own non-column query params
(e.g. a `BeforeList` hook scoping on `?region=eu`) declares them so strict
parsing skips them without disabling typo protection for real fields:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import "github.com/DonaldMurillo/gofastr/framework/entity"
-->
```go
app.Entity("things", entity.EntityConfig{
    AllowedFilterParams: []string{"region"}, // consumed by a BeforeList hook
})
```

**Escape hatch.** To tolerate *arbitrary* extra params (e.g. legacy
tracking params) rather than an enumerated set, opt back into the old
drop-silently behavior with `EntityConfig.LenientFilters: true`. Prefer
`AllowedFilterParams` or fixing the caller: a dropped filter is a
data-exposure hazard.

## Nested predicate filters (`?where=`)

The flat `?field_op=value` params AND-compose. When you need **boolean
logic**, OR-groups or nested AND/OR, pass a predicate tree as a JSON
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
(Hidden fields rejected; the same value-disclosure-oracle rationale as
flat filters); every value is a bound placeholder, never string-
interpolated; unknown fields/operators, malformed JSON, or a tree
exceeding the depth (8) or node (64) bounds return **400**. The whole
tree compiles to **one** parenthesized WHERE clause, so it AND-composes
with owner, tenant, and soft-delete scopes exactly like `?q=`: a user
OR-group can never widen past those scopes. `?where=` combines (AND)
with any flat `?field_op=` params on the same request.

## Code Generation

Generate Go from a `gofastr.yml` blueprint:

```bash
gofastr generate --from=gofastr.yml
```

This scaffolds the owned entity package into `entities/` at the module root:

- `register.go` with `RegisterAll(app *framework.App)`: the fixed seam.
  It carries no entity name, so adding an entity never edits it.
- one `<entity>.go` per declared entity: model struct, typed column
  constants, typed repository, lifecycle subscriptions, and its own
  `app.Entity(...)` registration that self-registers via `init()`. A new
  entity is a new file; existing files are never rewritten.
- `client/client.go` with a standalone Go HTTP client covering every
  CRUD operation per entity: list/get/create/update/patch/delete, the
  atomic `_batch` endpoints (`BatchCreate<Entity>` /
  `BatchUpdate<Entity>` / `BatchDelete<Entity>`, returning the
  `{committed, results[]}` envelope even on rollback), and the live
  `_events` feed (`Watch<Entity>`, a blocking SSE loop). Setting the
  client's `Token` field sends it as `Authorization: Bearer <token>` on
  every request: pair with a scoped API token
  ([auth](auth.md#service-accounts--scoped-api-tokens)); leave empty for
  public or cookie-authenticated APIs.

A blueprint that declares `app.module` also emits a flat `package main` at the
root (`main.go` plus `app.go`, `screens_register.go`, one `screen_<name>.go`
per screen, and `stubs.go` for endpoint/seed stubs). These are owned Go you
  read, edit, and commit; no `DO NOT EDIT` header. See
[Blueprints](blueprints.md) for the full blueprint shape, including the
[generated screen file layout](blueprints.md#generated-screen-files). To add
in-page dynamic behavior to those screens (sort, paginate, mutate without a
  reload), build islands: the cookbook is
[interactive-patterns](interactive-patterns.md).

Useful flags:

- `--from=<blueprint.yml>` selects the blueprint to generate from (required).
- `--dry-run` lists generated files without writing.
- `--json` emits machine-readable output.
- `--out=<dir>` scaffolds into a subpackage instead of the module root (also
  settable as `app.output_dir` in the blueprint); useful for monorepos and
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
`--add`, so the new entity/screen continues the project's declaration order,
existing files are never overwritten, and `--out`, `--dry-run`, and `--json`
work as above. `--force` and `--add` are rejected (scaffolding is additive):

* `gofastr generate entity posts`: `entities/posts.go` with one placeholder
  `name` field (a required string) you rename; CRUD stays default.
* `gofastr generate screen contact`: `screen_contact.go` at `/contact` with
  a heading + stub paragraph whose `Render` you replace.

See [Quick scaffolds](blueprints.md#quick-scaffolds-generate-entityscreen) in
the Blueprints guide for the relationship between stubs and full yml.

For arbitrary configured generators (not a full app blueprint), use a
`gofastr.codegen.yml` extension config. See [Codegen](codegen.md) for
config discovery, the extension protocol, and manifest-based cleaning.

To ship the API as a branded terminal client for your customers,
with token auth, filter/sort/pagination flags, batch verbs, and a live `watch`
feed, run `gofastr generate cli` from the app root. See
[Ship your API as a CLI](app-cli.md).

## Mounting under a prefix (`APIPrefix`)

By default an entity's CRUD routes mount at its bare name: `GET /posts`,
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
routes are unaffected: a route group owns its own prefix. Leaving `APIPrefix`
empty keeps the bare mounts, so adding it is never a breaking change.

> **Common mistake:** registering a screen at `/posts` while a `posts` entity
> mounts there too. Without `APIPrefix` you'll get a route-conflict panic naming
> the colliding path; set `APIPrefix` (or mount the page elsewhere) to resolve it.

### CRUD verbs and response envelopes

Each writable entity mounts `POST /<entity>`, `PUT /<entity>/{id}`, and
`PATCH /<entity>/{id}`. Both PUT and PATCH are sparse: validation and SQL
updates apply only to the fields present in the JSON body, so neither verb
nulls an omitted column: they are wired to the same update path and differ
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

### `json` fields round-trip

A `type: json` field (`schema.JSON` in Go) carries a whole JSON document.
Send it as a value, not as a string:

```
POST /api/policies
{"name":"pro","features":{"seats":5,"beta":true}}

→ 201 {"data":{"id":"…","name":"pro","features":{"seats":5,"beta":true}}}
```

The column stores JSON text (`JSONB` on PostgreSQL, `TEXT` on SQLite) and
reads back parsed, so what a client sends is what it reads back: on
create, update, get, list, cursor pages, `?stream=true`, and rows pulled
in through `?include=`.

Three rules worth knowing:

- **A string is JSON text, stored verbatim.** `{"features":"{\"seats\":5}"}`
  writes the same document as the object form. That is what an admin
  textarea submits.
- **Absent and `null` are the same thing**: both leave the column NULL and
  read back as `null`. `{}` is distinct: it stores and returns `{}`.
- **Text that is not JSON reads back unchanged**, so a legacy `TEXT` column
  promoted to `json` keeps serving its existing rows.

## MCP Tools

When an entity sets `"mcp": true`, GoFastr registers CRUD tools:

- `{entity}_list`
- `{entity}_get`
- `{entity}_create`
- `{entity}_update`
- `{entity}_delete`

The tools use the same validation and CRUD handler behavior as HTTP routes.

In the dev loop (`gofastr dev`; opt-out `GOFASTR_DEV_MCP=0`) these tools
register for **every CRUD-enabled entity**, with no per-entity `mcp: true`
needed, so the local agent can read and write app data. Production
keeps the explicit flag as the only path. Entities with `crud: false`
(e.g. the auth battery's users/sessions configs) are never implied:
MCP tools dispatch through the CRUD routes, so no routes means no
tools, in dev or out.

`/openapi.json` follows the same rule: an entity with `crud: false`
contributes no paths, because the server answers 404 for every one of
them and an SDK generated from the spec would ship methods that cannot
work. Its schema component stays, so hand-written `Endpoints` that speak
the entity's shape still have something to reference. An unset `crud`
means "auto" and is exposed, matching the router.

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

Under `WithAPIPrefix` a **relative** path resolves under the prefixed table
path: `WithAPIPrefix("/api")` mounts `{id}/publish` on entity `posts` at
`POST /api/posts/{id}/publish`, alongside that entity's CRUD routes. An
absolute path bypasses the prefix; use it to mount outside the entity's API
namespace.

Note the auth asymmetry: the HTTP `Handler` runs behind the route
middleware chain, but the `MCPHandler` twin is invoked directly: no route
middleware, so no per-caller auth of its own. **The twin therefore defaults
to requiring an authenticated caller.** Declare something stricter with
`MCPGate`, or opt out with `MCPPublic` for an endpoint that really is
anonymous over HTTP too:

```go
entity.Endpoint{
    Method: "POST", Path: "{id}/publish", MCP: true,
    Handler:    publishHTTP,
    MCPHandler: publishTool,
    MCPGate:    auth.MCPRole("admin"), // default: any authenticated caller
}
```

See [plugins](plugins.md) → MCP tool gating.

### Typed input/output schemas

By default a custom endpoint is shapeless to generators: OpenAPI emits a bare
`{type: object}` request/response and the MCP tool advertises an empty
`{type: object}` input schema: useless SDK stubs and agent tools. Describe the
request body and the success (200) response with the **optional** `InputSchema`
and `OutputSchema` fields. Both take `[]schema.Field`: the same representation
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
  any entity holding per-user data: List/Get/Update/Delete scope to
  the current user and Create stamps the column automatically.
- **Reaching for `public: true` to fix a 401 in dev.** The 401 an
  anonymous entity returns by default (see **Default CRUD
  authentication** above) is the framework working as intended: the
  fix is almost always to send a session, not to declare the entity
  `public`. `public: true` opens BOTH reads and writes to anyone; use it
  only for content that's genuinely meant to be public (a contact form,
  a blog's comments), never as a quick way past a login wall during
  development.
- **Setting `OwnerField` in an app that never wires an owner
  extractor.** Without a registered extractor the field is inert: no
  scoping, no stamping, no error. Importing `battery/auth` registers
  one in `init()`; pair it with `auth.SessionMiddleware` so
  cookie-authenticated requests carry a user.
- **Setting `Access` and forgetting the policy middleware.** The CRUD
  gate is fail-closed: a context without the permission gets 403, so
  without `framework.AccessMiddleware` (with a policy feeding roles
  into the context), *every* request to that operation 403s, including
  legitimate ones. `battery/auth` alone does not satisfy the gate.
- **Expecting a `relation` field to model has-many.** A relation field
  declares a BelongsTo: the FK lives in the field's own column, and
  the matching relation is derived for you. Has-many keeps its FK on
  the *other* table and must be declared explicitly via
  `HasMany`/`Relations`.
- **Writing a non-idempotent `Seed`.** The `_gofastr_seeded` ledger is
  best-effort: it survives normal restarts but cannot guarantee
  atomicity between your inserts and the ledger row. Use
  `INSERT … ON CONFLICT DO NOTHING` (or a pre-check) so a re-run is
  harmless.
