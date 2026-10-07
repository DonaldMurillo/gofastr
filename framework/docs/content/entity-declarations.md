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
`POST /posts/{id}/transitions/{key}` when its `States` declare a non-system
move (see [States](states.md)), the `/posts/_events` stream, and
`/posts/llm.md`. Route generation is **on by
default**: `Exposure.CRUD` is a `*bool`, and nil means generate. Omitting the
block is not "declare no surface"; it is "take the default surface".

`/posts/llm.md` describes the routes the entity's mount serves, not the
declaration's full shape. `App.View` entities and any registration built
with `CrudRouteOptions{ReadOnly: true}` get a doc without the write,
batch and Create/Update-column sections — those routes answer 404/405 —
and the `limit` row reports the same cap the List route clamps `?limit`
with (`Pagination.MaxListLimit`, never above 1000, 100 when unset). A doc
that advertises routes or limits the server refuses sends agents into
404s; the generators read the mount, not the declaration.
The type column of its field tables and the SDK reference screens
(`gofastr docs sdk`) share one label table, `schema.FieldTypeLabel`; the
SDK form adds the wire-format notes (decimal arrives as a string, image
and file fields as URLs), and an unknown type labels as `string`.

The skip side of pagination is bounded the same way the limit side is:
an explicit `?offset=` beyond the handler's ceiling is refused with
`400` instead of being handed raw to `OFFSET` (a per-request deep-skip
scan). The ceiling is `CrudHandler.MaxOffset` when set, otherwise the
page cap × 1000 — 100,000 at the default `Pagination.MaxListLimit` —
and it can be moved per handler with `WithMaxOffset`, never removed.
Page-derived offsets keep their own pinned behaviour: a huge `?page=`
serves the empty window (200), with the arithmetic overflow guarded in
`pagination.OffsetForPage`.

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
    Exposure: &framework.ExposureConfig{CRUD: &noCRUD}, // no generated routes or tools
})
```

The entity is still registered, migrated, and usable from Go
(`app.CrudHandler("posts")` builds a handler from the registry whether routes
were mounted or not, and hooks and typed queries work as usual). Setting
`MCP: true` alongside `CRUD: false` is a registration error, not a silent
mismatch: MCP CRUD tools dispatch through the routes.

No generated route reaches the rows of a `CRUD: false` entity, including
routes that belong to OTHER entities. Another entity's `?include=` of a
relation that targets it, and a `?rel.field=` filter across such a relation,
answer **403** with the include refusal ("include targets entity users, which
you may not read"). A cascade write through a parent route that would create,
update or ManyToMany-link its rows is refused the same way. In-process Go
(`app.CrudHandler`, typed repos, `EagerLoad`) is unaffected.

`CRUD: false` turns off the **generated** surface, and only that. Anything in
the entity's `Endpoints` list is registered either way, so an entity with
`CRUD: false` and a declared endpoint still answers HTTP on that endpoint's
path, and an `Endpoint` with `MCP: true` still registers its tool regardless
of `Exposure.MCP`. To end up with no surface at all, the entity needs
`CRUD: false` **and** no endpoints; if it keeps endpoints, they carry their
own access rules (an `Endpoint`'s HTTP handler inherits the route's
middleware chain, its MCP twin does not — see `Endpoint.MCPGate`).

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

## Blank form values

An HTML form posts every control it has, so a number, date, or relation
the user left blank arrives as `""`. Create and update treat that as
"not provided" for every field type except `String` and `Text`: a blank
optional field takes its declared `Default` on create and leaves the
column alone on update, and a blank required field fails with
`is required` rather than `must be an integer`. Empty text stays an
empty string, since that is a value a user can mean. A filled number
input arrives as text too: an `Int` field takes `"42"` and a `Float`
field takes `"42.5"` as the number each spells, and a `Float` refuses
hex, underscore separators, `NaN` and `Inf`. `Decimal` stays a string. The
entityui forms (`framework/entityui`) rely on this; a JSON client gets
the same treatment.

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
field) declare how entities associate with one another:
- `type`: `belongs_to`, `has_many`, `has_one`, or `many_to_many`.
- `name`: the relation property name used in JSON payloads, include queries, and nested filters.
- `entity`: the target entity name.
- `foreign_key`: the referencing column (`belongs_to`, `has_many`, `has_one`).
- `through`: for `many_to_many`, the intermediate pivot table name.
- `local_key`: for `many_to_many`, the column referencing the parent entity in the pivot table.
- `foreign_key_target`: for `many_to_many`, the column referencing the target entity in the pivot table.
- `on_delete`: optional foreign key referential integrity action (`no_action`, `restrict`, `cascade`, `set_null`).
- `cascade_write`: optional boolean enabling atomic nested mutations within a single transaction.

### Cascade Writes & Atomic Nested Mutations
When `cascade_write: true` (or `.WithCascadeWrite(true)`) is enabled, nested child maps or slices can be sent directly inside parent `POST` and `PUT`/`PATCH` payloads.
All writes execute inside the parent's database transaction (`inTx`). If any child validation or constraint check fails, the transaction is completely rolled back, leaving zero orphaned rows.
- **`belongs_to`**: The child is validated and created/updated before the parent, and the resulting child ID is automatically injected into the parent foreign key.
- **`has_one`**: The child is created or updated after the parent. If an ID is provided or an existing child is found, it is updated in-place (with ownership verification); otherwise it is created.
- **`has_many`**: An array of child objects is processed. Items carrying an ID belonging to the parent are updated; items without an ID are inserted.
- **`many_to_many`**: An array of target IDs or child objects is processed. Existing IDs are verified against caller tenant/owner scope and linked in the pivot table; objects carrying new fields are created or updated.
- **Authorization**: Each nested create or update must pass the gates the target's own route applies: owner, tenant, the signed-in session requirement, and its `access` permission. A public parent does not make its children public: an anonymous `POST` that nests a row for a session-gated child is refused, the same answer the child's route gives. `WithServerWrites` skips these checks. The in-process API (`CreateOne`, `UpdateOne`) skips only the session requirement, as it does for the parent.
- **Events**: Child `entity.created`/`entity.updated` events publish after the parent's transaction commits. A cascade that rolls back publishes nothing. The parent's own event carries the parent row only: the HTTP response nests the children, but the event payload (live bus and outbox alike) does not, so each child arrives as its own event under its own entity. On the SSE stream that event passes the child's read gate and its AfterGet redaction. Durable outbox consumers get the child's raw row, like any other staged record, and own their masking (see [Events](events.md)).
- **Attach-Only Semantics**: Cascade updates are additive (upsert/attach). Omitted children or omitted ManyToMany links are not deleted or unlinked on update; explicit deletion or detaching must be handled via child endpoints.

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

**A BelongsTo FK is a write-side trust boundary too.** A create or
update whose body sets a BelongsTo FK (an `order_id`, an
`author_id`) is refused with **404** when the target row exists but
the caller cannot read it under the target's own owner, tenant, and
read scopes — the same status reading that foreign row gives, so the
refusal leaks no existence. The write resolves the exact predicate
the read side (`?include=`, `EagerLoad`) resolves. Callers holding
an explicit cross-scope grant pass unchanged: a
`owner.AllowCrossOwner(ctx)` marker, the target's `CrossOwnerRead`
permission, `tenant.AllowCrossTenant`, or a `crud.WithServerWrites`
context (host-driven writes: jobs, imports, admin tooling, which own
their references the way they own ReadOnly column writes). A NULL or
absent FK, an unscoped target, or a target the registry does not
know is skipped — no registry means no read-side resolver either.
The FK's existence check (`FOREIGN KEY` on both dialects) still runs
underneath; the scope gate is what answers "someone else's row".

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

**The Decider is consulted.** The lift routes through
`access.CanResource` with `Ref{Type: <entity>, ID: ""}`, like every
sibling gate: a resource-aware `Decider` returning `DecisionDeny` for
the entity keeps a policy-granted caller inside their owner scope. The
widening reads the role policy AND the per-resource authority.

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

`CRUD=false` also keeps user rows off other entities' routes: a
`BelongsTo("author", "users", "author_id")` relation on `posts` stays
usable from Go, but `GET /posts?include=author` and
`GET /posts?author.email_like=…` answer 403 instead of returning or
filtering by user columns (`email`, `roles`). To show an author's name,
put it on a separate entity with its own CRUD and access rules, or render
it server-side.

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
  this is fail-closed: no policy in context means no widening. The check
  routes through `access.CanResource` with `Ref{Type: <entity>, ID: ""}`,
  so a resource-aware `Decider` returning `DecisionDeny` keeps a
  policy-granted caller inside the filter.
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

**Write responses are filtered.** The row a write hands back is a read, so
it honours the scope. When the row an update, `_batch` update item,
`UpdateOne`, `BatchUpdateMany` or `UpsertOne` produced is outside the
caller's scope, the response carries only its id (`{"id": "n2"}`) instead of
the stored columns; the write itself still happens. A caller who is
unrestricted, or whose row stays inside the scope, gets the full row as
before. Create responses are unchanged: they echo what the caller just
sent.

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
| `_ne`    | `!=` (not equals)                          |
| `_gt`    | `>`                                        |
| `_gte`   | `>=`                                       |
| `_lt`    | `<`                                        |
| `_lte`   | `<=`                                       |
| `_like`  | literal `contains` (`LIKE '%value%'`)      |
| `_in`    | `IN (…)`, comma-separated, capped at 1000 |

`_ne` follows `=`'s NULL semantics rather than SQL three-valued intuition:
a row whose column is NULL matches neither `=` nor `!=`, and no
`OR column IS NULL` arm is added. Combine with `_in` (or a `?where=`
`ne`/`in` leaf) when you want "everything except these".

`_like` on a number, date, Bool or JSON column, or `_gt`/`_gte`/`_lt`/`_lte`
on a Bool or JSON column, is a 400: see "Operators must suit the field's
type" below.

**Field names cannot shadow an operator suffix.** A queryable field whose
name or wire name is another queryable field's plus a suffix from the
table above (`status` beside `status_ne`) is refused at entity
registration: the parser tries the suffix first, so `?status_ne=` would
filter `status` and the `status_ne` column could never be filtered at
all. Rename one field or set a `WireName`.

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

Operators are the same set as the flat params: `eq, ne, gt, lt, gte, lte,
like, in` — `ne` is the `!=` twin of `_ne` above, NULL semantics included.

**Operators must suit the field's type.** `like` works only on a
String, Text, Enum or UUID column, and the ordered comparisons (`gt`,
`lt`, `gte`, `lte`) are refused on Bool and JSON: SQL orders
`false < true`, but no caller means that, and a JSON blob has no order
a request value can compare against. `eq`, `ne` and `in` suit every
type. The rule is `filter.CheckOpType` (one predicate,
`filter.OpSuitsType`, behind every surface), and every filter surface
applies it: the flat `?field_<op>=` params, `?where=`, Go-built
predicates (`filter.ValidatePredicate` below), the filter DSL, relation
filters (`?author.active_like=`, `crud.NestedFilter`) and include-
scoped filters — and every surface that ADVERTISES operators (the
OpenAPI spec, the generated CLI's flags, the MCP list tool, `llm.md`,
the SDK docs and readmes) derives its per-field set from the same
predicate, so no client is offered a filter the server answers 400. A
refusal is a 400 naming the operator, the key sent and the column
type.

**Safety.** Every field is validated against the entity's schema
(Hidden fields rejected; the same value-disclosure-oracle rationale as
flat filters); every value is a bound placeholder, never string-
interpolated; unknown fields/operators, malformed JSON, or a tree
exceeding the depth (8) or node (64) bounds return **400**. The whole
tree compiles to **one** parenthesized WHERE clause, so it AND-composes
with owner, tenant, and soft-delete scopes exactly like `?q=`: a user
OR-group can never widen past those scopes. `?where=` combines (AND)
with any flat `?field_op=` params on the same request.

## Predicates in Go (`ParsePredicate`, `ValidatePredicate`)

Server code builds the same trees the URL surfaces parse. `dsl.ParsePredicate`
turns filter text — a list view's `Where`, the admin's filter chips and
query box, the `?filter=` URL parameter — into a `*filter.Predicate` that
has already been validated against the entity's fields:

```go
pred, err := dsl.ParsePredicate(`status in ["open", "past_due"] and amount >= 10000`, fields)
```

The grammar is small and strict: comparisons with `=`, `!=`, `<`, `>`,
`<=`, `>=`, a literal-substring `contains`, and `in` over a bracketed
list, combined with `and`/`or` and parentheses. Keywords are
case-insensitive; `and` binds tighter than `or`. Values are
double-quoted strings (escaping `\"` and `\\`), bare numbers, and
`true`/`false`. Blank text returns a nil predicate; input over 8 KiB is
refused; errors name the byte offset and quote only a short scrubbed
excerpt. `dsl.ParseSort` is the sibling for a view's `Sort`
(`amount DESC, number ASC`), returning `filter.ParsedSort` values under
the same field rules as `?sort=`. Both live in
[the query DSL page](query-dsl.md).

`filter.ValidatePredicate(p, fields)` is the check both URL parsing and
`ParsePredicate` already ran, exposed for trees built in Go (a view func
that depends on who is looking, say). It runs every `?where=` check over
the tree: each leaf field exists and is neither Hidden nor NoQuery, the
operator is known and suits the field's type, an `in` leaf carries 1 to
1000 values, and the tree stays within the depth (8) and node (64)
bounds. It never writes to the input — the tree may be shared across
goroutines — and returns a resolved deep copy: wire aliases resolved to
columns, Bool leaves carrying their coercion marker. That copy is the
tree to hand `BuildPredicate`. Field names in a predicate are spliced
into SQL by `BuildPredicate` (values are bound placeholders), so this
check is what keeps a hand-built tree safe to compile.

Pass a validated tree to the in-process list through
`crud.ListOptions.Where`. It is ANDed with everything the list already
applies — flat filters, nested filters, search, owner, tenant,
soft-delete and read scopes — as one parenthesized clause, so an `or`
inside it cannot widen past a scope. The tree is validated again inside
`ListAll`/`CountAll`, because a caller may have built it by hand; a
refusal names the offending leaf. `ListOptions.Filters` is held to the
flat parser's rules too: an entry naming an unknown, Hidden or NoQuery
column, or an operator the column's type refuses, returns an error
instead of reaching SQL, and a `WireName` resolves to its column.
`ListOptions.Fields` narrows the columns a list reads to the named subset plus `id` (always included);
unknown and Hidden names are refused, never silently dropped (a NoQuery
column may be read: NoQuery keeps it out of filters and sorts only),
and masking hooks still run on whatever came back. `CountAll` applies
both identically, so a page count matches its rows.

Two aggregates run under the same scopes and compute in the database,
over every match rather than a page:

```go
total, err := invoices.SumAll(ctx, "amount", crud.ListOptions{Where: open})
groups, err := invoices.GroupCountAll(ctx, "status", crud.ListOptions{}, 50)
```

`SumAll` returns the database's own decimal text ("0" over no rows), so
a `decimal` column sums as `NUMERIC` on Postgres, exactly; it takes a
visible int, float or decimal field. `GroupCountAll` returns each stored
value with its count, ordered by value, a bool as `true`/`false`; a
positive limit caps the groups, so ask for one more than you draw to
tell a capped result from a whole one. Both refuse `Fields`, `Sorts`,
`Limit`, `Offset` and `Includes`, and a Hidden field. Under
`WithReadHooks`, an entity with `AfterList` hooks refuses both with
`crud.ErrAggregateMasked`: those hooks mask rows after the query, and a
total the database computed would count what they hide. Read the rows
with `ListAll` there.

## Restoring and purging soft-deleted rows

For a `soft_delete: true` entity, the in-process handler offers the pair
the admin's Deleted view is built on:

```go
err := handler.RestoreOne(ctx, id) // clears deleted_at, like an update
err := handler.PurgeOne(ctx, id)   // hard-deletes the trashed row
```

`RestoreOne` behaves like an update: it asks the entity's update
permission and any installed Decider about the one record, runs the
`BeforeUpdate`/`AfterUpdate` hooks, writes an audit row with the
operation `restore`, and emits `entity.updated`. `PurgeOne` mirrors the
delete path (`BeforeDelete`/`AfterDelete`, audit operation `purge`,
`entity.deleted`). Both answer `crud.ErrNoSoftDelete` on an entity
without soft delete, and `crud.ErrNotSoftDeleted` when the row is not
soft-deleted — purge never skips the soft delete, so a live row cannot be
hard-deleted past the retention window soft delete exists for.

Visibility follows the write scopes: a row the caller cannot see under
owner and tenant scoping answers the same not-found error a read of that
id gives, so another owner's id discloses nothing. Both are in-process
only (no HTTP routes); `crud.WithServerWrites` skips the permission
question the way it does for every other trusted write, while owner and
tenant context stay required.

## Screen hints (`Display`)

`EntityConfig.Display` tells the admin and the generated screens how to draw
the entity: its names, the columns a list opens with, named views, where
fields sit on the record, the sidebar placement, and per-field hints. It is
a pointer config like `Scope` and `Exposure`: nil means every default, and
the framework copies it deeply at registration, so editing the value you
passed afterwards changes nothing the app checked or serves.

Display changes how a screen draws, never what the API accepts: a field's
own `Hidden`, `ReadOnly` and `NoQuery` keep their meaning and screens honour
them first. A label change is also invisible to generated clients — `Display`
is not part of the SDK schema hash, so relabelling never reports as drift.

```go
app.Entity("invoices", framework.EntityConfig{
    Fields: []schema.Field{ /* … */ },
    Display: &framework.DisplayConfig{
        Singular:   "Invoice",
        Plural:     "Invoices",
        TitleField: "number",               // names a record in lists, drawers, breadcrumbs
        Columns:    []string{"number", "amount", "status"},
        Nav:        &framework.EntityNav{Group: "billing", Icon: "receipt", Order: 1},
        Views: []framework.ListView{
            {Key: "open", Where: `status = "open"`, Sort: "due_on ASC"},
        },
        Facets:    []string{"status"},      // one-click filters: Enum, Bool or Relation fields
        PageSizes: []int{10, 25, 50},       // each within Pagination.MaxListLimit
        NoBulk:    true,                    // NoDuplicate turns off one action the same way
    },
})
```

The same block in a JSON entity declaration (`entity.EntityDeclaration`),
under the entity with snake_case keys. `display` has no flat shorthand:
every key lives under it, and an unknown key is a decode error.

```json
{
  "name": "invoices",
  "fields": [
    {"name": "number", "type": "string", "required": true},
    {"name": "status", "type": "enum", "values": ["draft", "open", "paid"]},
    {"name": "issued_on", "type": "date"},
    {"name": "due_on", "type": "date"},
    {"name": "paid_on", "type": "date"}
  ],
  "display": {
    "singular": "Invoice",
    "title_field": "number",
    "columns": ["number", "status"],
    "nav": {"group": "billing", "icon": "receipt", "order": 1},
    "views": [{"key": "open", "where": "status = \"open\"", "sort": "due_on ASC"}],
    "facets": ["status"],
    "card": {"title": "number", "badge": "status", "meta": ["status"]},
    "fields": {
      "number": {"placeholder": "INV-0001"},
      "status": {"locked": true}
    },
    "form": {
      "main": [
        {"row": ["number"]},
        {"section": "dates", "help": "When the invoice moves",
         "items": [{"row": ["issued_on", "due_on"]}, "paid_on"]}
      ],
      "side": ["status"]
    },
    "page_sizes": [10, 25]
  }
}
```

### Every setting

| `Singular`, `Plural` | Names for nav, headings and buttons. The key under them (`entity.<entity>.singular`) translates; the value is the English fallback, and without Display the entity name is — singularized for `Singular` (so `invoices` labels one record "Invoice"), title-cased for `Plural` |
| --- | --- |
| `Description` | One line under the list heading |
| `TitleField` | The field that names a record in lists, drawers, pickers and breadcrumbs; may not be `Hidden`. Unset, a `name` or `title` field names it, else the first `String` column that is not omitted or `NoQuery`, else the singular |
| `Columns` | The columns a list opens with, before the viewer picks their own |
| `Views` | Named starting points for the list, shown as tabs. `Key`, optional `Label`, a DSL `Where`, a `Sort`, an optional `As` (`"table"`, the default, or `"cards"`; anything else is refused), and `Default` (at most one view may set it) |
| `Facets` | Enum, Bool or Relation fields offered as one-click filters |
| `Form` | Where fields sit on the record: `Main` and `Side` columns of `FormItem`s |
| `Card` | The fields a card shows when a list is drawn as cards: `Title`, `Subtitle`, `Badge`, `Meta` |
| `Fields` | Per-field hints keyed by field name: `Label`, `Help`, `Placeholder`, `Locked` (drawn read-only on screens; the API may still write it), `Omit` (left out of forms and columns; the API still returns it), `ShowWhen` (`field = value` or `field in [...]` on an editable Enum or Bool field). None of the three may take a Required field with no `Default` |
| `PageSizes` | The page-size menu; every entry is positive and within `Pagination.MaxListLimit` when that is set |
| `NoDuplicate`, `NoBulk` | Turn off the Duplicate row action, or every bulk action, for this entity |

A `FormItem` is exactly one of: a bare field name (YAML: `"status"`,
`{"field": …}` is refused), a `Row` of one to three distinct fields, or a
`Section` — a key, optional `Help` and `Collapsed`, and its own `Items`,
which take the same three shapes again. Sections nest at most two deep.
Fields the form leaves out are appended at the end of `Main` in schema
order (minus the auto-generated system fields), so adding a field never
makes it vanish.

### The boot check

Registration (`Registry.Register` → `Entity.Validate`) refuses the entity
when any name Display holds is wrong, so a typo or a stale name after a
rename fails the app at boot instead of rendering a blank column per
request:

- **Fields.** Every name in `Columns`, `TitleField`, `Facets`, `Card`,
  `Form` (items, rows, nested sections) and the keys of `Fields` must
  exist and not be `Hidden`.
- **Duplicates.** `Columns`, `Facets` and `PageSizes` are menus; a
  repeated entry is refused, naming the duplicate.
- **Facet types.** A facet must be an Enum, Bool or Relation field, and
  not `NoQuery`.
- **Keys.** View keys, form section keys and the nav group are lowercase
  ASCII slugs (`^[a-z][a-z0-9_]*$`, no dots), unique within their list, and
  not `all` or `deleted` (the screens own those). At most one view sets
  `Default`. `entity.ValidKey(s)` reports whether a string follows this
  grammar. `States` move keys follow a stricter one, checked with the rest
  of the states block by `entity.ValidateStates` (see
  [States](states.md)).
- **The form.** Each item sets exactly one of field, row or section; a row
  holds one to three distinct fields; only a section carries `Items`,
  `Help` and `Collapsed`, and holds at least one item; sections nest at
  most two deep; a field appears once across `Main` and `Side`.
- **`Omit`, `Locked`, `ShowWhen`.** Refused on a Required field with no
  `Default` and no auto-generation: an omitted field is not on the
  form, a locked one never submits (the save path drops it), and a
  conditionally hidden one is disabled while its condition does not
  hold — no form could create the record.
- **`PageSizes`.** Every entry is positive, appears once, and is within
  `Pagination.MaxListLimit` when that is set.
- **`As`.** A view's `As` is `"table"` (or empty, the same thing) or
  `"cards"`; anything else is refused.

A view's `Where` and `Sort` and a field's `ShowWhen` are parsed with the
query DSL when the app registers the entity — `App.Entity` and
`GroupEntity` both run the check (`framework/entity` cannot import the
DSL) — and a bad one fails registration the same way: a `Where` naming
an unknown, Hidden or NoQuery field; a `Sort` outside the `?sort=`
grammar (`amount DESC, number ASC`; a direction defaults to ASC); or a
`ShowWhen` that is anything but `field = value` or `field in [...]`
over an editable Enum or Bool field whose values it names. The
translation keys Display's names produce (`entity.<entity>.*`,
`nav.groups.<key>`) are listed on the
[Internationalization](i18n.md) page.

## States (a state machine)

`EntityConfig.States` names the Enum field holding a record's state and the
moves that change it. Unless `Advisory` is set, the CRUD handler refuses any
write that changes the state field or a stamp outside a move. A create starts
at an `Initial` value, an update writing the stored value back passes, and
the change itself is one move: `POST /<entity>/{id}/transitions/<key>`, the
MCP tool `<entity>_<key>`, or `RunTransition` in Go.

```go
States: &framework.StatesConfig{
    Field:   "status",
    Initial: []string{"draft"},
    Transitions: []framework.Transition{
        {Key: "issue", From: []string{"draft"}, To: "issued", Stamp: "issued_on"},
    },
}
```

The declaration key is `states`, snake_case inside it, unknown keys refused.
Stamps, per-move permissions, System moves, the boot check, the audit trail
and the `crud.WithStateOverride` escape hatch are on the
[States](states.md) page.

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
  `_events` feed (`Watch<Entity>`, a blocking SSE loop). For an entity
  with `States`, the state field is read-only in the client and each
  non-system move is one call ([States](states.md)). Setting the
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

**Integer precision on the wire.** JSON request bodies are decoded with
exact number handling, so an integer literal sent to an `Int` column
persists exactly as written — including values above 2^53
(`9007199254740993`), which a plain `encoding/json` decode would
silently round before validation ever saw it. A number that reaches an
`Int` column as a float at or beyond ±2^53 (only possible through an
in-process caller that decoded its own JSON into `float64` first) is
refused with `400` rather than rounded: crud cannot tell a
legitimately-round value from one that lost a digit, and silently
storing a different number than was sent is the one unrecoverable
outcome. Send large integers as JSON integer literals or strings; both
spellings round-trip exactly. The same rule covers `_batch` items,
`UpsertOne` caller-supplied increment primary keys, and the typed query
update paths.

Every successful single-record response has one stable envelope:

```json
{"data":{"id":"p1","title":"Hello"}}
```

This applies to create (`201`), get (`200`), PUT (`200`), and PATCH (`200`).
Lists keep `{"data":[...]}` plus pagination metadata.

Errors are JSON everywhere in the API namespace, whatever missed. A handled
record miss (e.g. `GET /api/posts/404-nope`) answers
`application/json` with `{"error":"not found","success":false,"code":404}`.
A write the database refuses on a constraint answers `409 Conflict`, not
`500`: a UNIQUE violation (a duplicate key) and a FOREIGN KEY violation
(a create or update that points at a missing row, or a delete of a row that
other rows still reference) on SQLite, Postgres and MySQL. The body names
neither the constraint nor the table; the driver's message goes to the
server log only. When the refused constraint is one the entity declares
(a `unique` field, a unique column index, a relation's foreign key) the
body carries the fields the caller sent in the validation shape, so a
form shows the refusal on the control:
`{"error":"conflict","success":false,"code":409,"fields":{"number":["is already in use"]}}`.
A relation's message is "refers to a record that does not exist". A
column the caller did not send (the owner column of a per-account
index) is not named, and a conflict on a `hidden` or `no_query` field
the caller sent stays bare, so a probe cannot learn which value exists. SQLite's foreign-key refusal names no column, so on
SQLite it stays bare too.
A path no route ever owned — `/api/anything/else`, including on apps with
no DB and therefore no CRUD routes — answers `404` with an RFC 9457
`application/problem+json` document (`type`/`title`/`status`/`detail`)
instead of the UI host's HTML 404 page, so every machine client probing the
API gets a machine-readable answer. The guarded namespace is
`AppConfig.APIPrefix` (`WithAPIPrefix`), `/api` by default; misses outside
it keep the host's normal 404 rendering.

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
- `{entity}_<key>` for every non-system move its
  [States](states.md) declare; the update permission gates it like
  `{entity}_update`

The tools use the same validation and CRUD handler behavior as HTTP routes.

Each tool carries its operation's `Exposure.Access` permission as a
`WithToolGate` gate: `list` and `get` need `Access.Read`, `create`,
`update` and `delete` need theirs. A signed-in caller without
`posts:write` does not see `posts_create` in `tools/list`, and calling it
by name is refused before the router runs. A resource-aware Decider is
asked about the entity (`Ref{Type}`), as the route asks for `list` and
`create`. The route judges `get`, `update` and `delete` per record
(`Ref{Type, ID}`), so with a Decider on the context those three stay
listed and the route decides; a role policy, which does not depend on the
record, still hides them.

The gate judges only what the `/mcp` request's own context shows. When it
carries no user (the credentials may still be resolved on the
redispatch), or no role policy and no Decider (a policy mounted on a
route group runs only on the redispatch), the tool stays listed and the
route decides, as before. The same holds for an entity declared with
`app.GroupEntity`: the group's `WithAccess` and `Use` middleware run only
on the redispatch and may install another policy, so its tools are never
gated. To get the hiding, declare the entity with `app.Entity` and mount
`framework.AccessMiddleware` with `app.Use` so `/mcp` passes through it.
Owner and tenant scoping stay on the route.

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
