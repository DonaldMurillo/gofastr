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
        Access: framework.AccessControl{Read: "tickets:read", Create: "tickets:write", Update: "tickets:write", Delete: "tickets:write"},
    },
})
```

App spec entities accept the same `scope:`, `pagination:`, and `exposure:`
maps. In Go, the grouped sub-configs (`ScopeConfig`, `PaginationConfig`,
`ExposureConfig`) are the only form; the historical flat `EntityConfig`
fields were removed. In an app spec YAML you may still use the flat
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
an explicit `?offset=`, or a `?page=` whose `(page-1) × limit` skip lies
beyond the handler's ceiling, is refused with `400 offset_too_large`
instead of being handed to `OFFSET` (a per-request deep-skip scan). The
skip is reckoned exactly, so a page or offset too large for an `int`
(`?page=9223372036854775807`) is refused too, on the buffered and the
`?stream=true` path alike, rather than served as page 1. The ceiling is
`CrudHandler.MaxOffset` when set, otherwise the page cap × 1000 — 100,000
at the default `Pagination.MaxListLimit` — and it can be moved per handler
with `WithMaxOffset`, never removed. A page within the ceiling past the
last row answers 200 with no data.

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

The `<table>_list` tool takes the list route's filters as arguments, one per
key (`status`, `amount_gte`, `status_in`), and sends them on as the query
string. An `_in` argument is a JSON array of at least one value, and no
element may hold a comma, because the route splits `_in` values on commas;
the tool refuses either rather than drop the filter or split the value. A
JSON field's argument is any JSON value, compared as JSON: `{"payload": [1,
2]}` matches the stored array `[1,2]`.

No generated route reaches the rows of a `CRUD: false` entity, including
routes that belong to OTHER entities. Another entity's `?include=` of a
relation that targets it, and a `?rel.field=` filter across such a relation,
answer **403** with the include refusal ("include targets entity users, which
you may not read"). A cascade write through a parent route that would create,
update or ManyToMany-link its rows is refused the same way. In-process Go
(`app.CrudHandler`, typed repos, `EagerLoad`) is unaffected.

A `belongs_to` foreign key in a write body is not refused: `POST /posts
{"author_id": …}` onto a `CRUD: false` `users` writes, as it does from a
token holding no `users:read` scope. The key is a column of the caller's
own row and the write returns that row, never the target's fields. The
key is still checked against the target's own scopes (owner, tenant,
soft delete, `ReadScope`, its Before hooks): a row the caller may not
read answers the same 404 as a row that does not exist.

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

The same entity shape can also be **declared in a `gofastr.yml` app spec**
and emitted as Go by the CLI; see [App specs](app-spec.md), the single
declaration format the `gofastr generate` codegen pipeline reads. The
`EntityDeclaration` / `FieldDeclaration` types documented below
(`framework/entity/declaration.go`) are the in-memory shape the app spec
loader decodes an app spec's `entities:` list into before converting each to
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
the user left blank arrives as `""`. For every field type except
`String` and `Text`, a blank is not a value of the type: a blank
optional field takes its declared `Default` on create and clears the
column to null on update, and a blank required field fails with
`is required` rather than `must be an integer`. A blank `Image` or
`File` on update keeps the stored file, since a file input cannot
carry it back. An upsert reads a blank as a create does when no row
holds the key and as an update does when one does. A blank in a key
column is never dropped, so an `Int` key part still refuses it, and a
`""` in any caller-chosen key column, a one-column key included, is
refused with `400` (`is required`): no route could name that row again.
Empty
text stays an
empty string, since that is a value a user can mean. A filled number
input arrives as text too: an `Int` field takes `"42"` and a `Float`
field takes `"42.5"` as the number each spells, and a `Float` refuses
hex, underscore separators, `NaN` and `Inf`. `Decimal` stays a string. The
entityui forms (`framework/entityui`) rely on this; a JSON client gets
the same treatment. A `Timestamp` field also takes what
`<input type=datetime-local>` sends, `YYYY-MM-DDTHH:MM` with optional
seconds and fraction, read as UTC like the filter reads it.

Before hooks see the body after this cleanup, so a typed
`OnBeforeCreate[T]` or `OnBeforeUpdate[T]` decodes a form post: a blank
optional `Int` is absent (nil on update), `"2"` is the integer 2, and a
checkbox's `"true"` is the boolean. A value the cleanup cannot read
reaches the hook as sent, and validation refuses it after the hooks run
unless a hook replaced it. The same cleanup runs again after the hooks,
for any value they set.

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
or MCP server is touched. That includes a route the mux would refuse
as overlapping an existing one, such as `GET /{kind}/_events` next to
the entity's `GET /posts/{id}`, and endpoints that overlap each other or
the CRUD routes. A rejected declaration leaves no registry
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

## App spec entity shape

Inside a `gofastr.yml` app spec, each entry in the `entities:` list maps
onto the `EntityDeclaration` fields below. The same field-type vocabulary
applies whether you write the entity in Go (`EntityConfig`) or in an app spec:

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
| `auto_generate` | string | Auto-populate strategy: `uuid` (random UUID v4, the default `id`), `increment` (database-assigned integer, `BIGSERIAL` on Postgres, `INTEGER PRIMARY KEY` rowid alias on SQLite; the column is omitted from INSERT so the sequence/rowid assigns it), or `timestamp`. The generated field never appears in write forms. |
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

A `bool` default written as text (`Default: "true"`, or `"false"`, `"1"`,
`"0"`) is stored as the boolean, as the same text in a body is, so a
`?field=true` filter finds the defaulted rows on SQLite too.

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
- **`belongs_to`**: The child is validated and created/updated before the parent, and the resulting child ID is automatically injected into the parent foreign key. A foreign key this write cannot set (`ReadOnly`, `Hidden`, or the owner column on an update) refuses the nested write with 400 `validation.cascade_key_not_writable` before any child is written, since the parent could not point at the child.
- **`has_one`**: The child is created or updated after the parent. If an ID is provided or an existing child is found, it is updated in-place (with ownership verification); otherwise it is created.
- **`has_many`**: An array of child objects is processed. Items carrying an ID belonging to the parent are updated; items without an ID are inserted.
- **The child's link column**: A `has_one` or `has_many` child that is created points at the parent through its own foreign key, so the child must declare that field and its create must write it. A foreign key that is `ReadOnly`, `Hidden`, the child's tenant column, or not declared refuses the nested create with 400 `validation.cascade_key_not_writable` and rolls the parent back, instead of storing a child with no parent.
- **`many_to_many`**: An array of target IDs or child objects is processed. Existing IDs are verified against caller tenant/owner scope and linked in the pivot table; objects carrying new fields are created or updated. A link already present is skipped; for a registered pivot entity "present" is read under the pivot's own tenant, owner and soft-delete scope, so a soft-deleted link is not present and re-linking it hits the pivot's key. A link insert that breaks a unique constraint answers **409** and rolls the whole write back, unless the link turns out to exist on a recheck: then another request wrote the same pair at the same moment and the write succeeds.
- **Authorization**: Each nested create or update must pass the gates the target's own route applies: owner, tenant, the signed-in session requirement, and its `access` permission. A public parent does not make its children public: an anonymous `POST` that nests a row for a session-gated child is refused, the same answer the child's route gives. `WithServerWrites` skips these checks. The in-process API (`CreateOne`, `UpdateOne`) skips only the session requirement, as it does for the parent.
- **Events**: Child `entity.created`/`entity.updated` events publish after the parent's transaction commits. A cascade that rolls back publishes nothing. Inside a caller's transaction (`App.InTx`, a handler bound to a `*sql.Tx`) they wait until the whole operation succeeds, so a failed cascade announces no child. The parent's own event carries the parent row only: the HTTP response nests the children, but the event payload (live bus and outbox alike) does not, so each child arrives as its own event under its own entity. On the SSE stream that event passes the child's read gate and its AfterGet redaction. Durable outbox consumers get the child's raw row, like any other staged record, and own their masking (see [Events](events.md)).
- **Child keys**: A nested child's keys are read the way its parent's are. Over HTTP that is the child's wire keys; two keys naming one column (`bodyText` and `body_text` on a camel-case handler) answer 400 `validation.keys_fold` on the child's path, as the parent's own keys do. In process (`CreateOne`, a typed repo) the key `MarshalEntity` derives counts too, so a `Line2` field's `line2` writes `line_2`.
- **Read scope**: A cascade reaches the child entity's rows the way `?include=` does, so it honours the child's read scope: its `ReadScope`, and the clauses its own `BeforeList` hooks append (plus `BeforeGet` for `has_one` and `belongs_to`). An item that names an existing child the scope hides, by a bare id or with fields to update, answers **404**, the same as a child that is not there, and writes nothing; a `has_one` update with no id treats a hidden existing child as absent. A child the write creates, or moves, out of that scope comes back as its id alone.
- **Attach-Only Semantics**: Cascade updates are additive (upsert/attach). Omitted children or omitted ManyToMany links are not deleted or unlinked on update; explicit deletion or detaching must be handled via child endpoints.

`owner_field` mirrors `Scope.OwnerField`: set it to the column
that holds the row owner's id (e.g. `user_id`) and the declared
entity gets the same per-user auto-CRUD scoping as a Go-declared one
(see **Per-user scoping** below). Omit the key to keep pre-existing
behaviour. `gofastr generate app gofastr.yml` emits `OwnerField:` inside a
`Scope: &framework.ScopeConfig{…}` block in the generated `app.Entity(...)`
registration, so the scoping survives code generation.

`access` mirrors `Exposure.Access` (`framework.AccessControl`): the
per-operation RBAC permission required by auto-CRUD. Keys are `read`
(List + Get), `create`, `update`, and `delete`; each value is a permission
string such as `posts:write`. A blank or omitted `read` leaves List and Get
un-gated by RBAC. A blank or omitted `create`, `update` or `delete` refuses
that operation with **403** for every caller, admins included; write
`anyone` (`framework.Anyone` in Go) to open it on purpose. Owner and tenant
scoping still apply either way. Omit the whole map for no RBAC gating at
all. When set, auto-CRUD refuses a request whose
context lacks the permission with **403**: the roles + policy must be in
the request context first: mount `framework.AccessMiddleware` with a policy
(`battery/auth` only supplies the authenticated user whose roles you feed
into it; it does not satisfy the gate by itself; see
[access-control](access-control.md)). `gofastr generate app
gofastr.yml` emits the map as `Access: framework.AccessControl{...}`
inside an `Exposure: &framework.ExposureConfig{…}` block in the generated
`app.Entity(...)` registration, so declared entities get the same
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
entity instead: a blank `read:` stays open, a blank write is refused,
and `anyone` opens a write).

For an entity that's genuinely meant to be open to anonymous callers, such as a
public contact form, a blog's comments, or a newsletter signup, declare
`public: true`. This is a full, deliberate opt-out: every operation,
reads AND writes, is reachable anonymously, matching the framework's
pre-secure-by-default behaviour for that entity. It is **not** a partial
"reads only" relaxation: an entity that wants public reads but gated
writes uses `access:` instead (a blank `read:` + a real `create:`
permission leaves List/Get open while Create still requires the
permission, and the blank `update:` and `delete:` are refused):

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
      create: posts:write   # blank read: → public reads; blank update/delete → refused
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
A tool call whose own arguments are wrong (an empty record id, a filter
on a JSON field, an empty or comma-holding `_in` list) answers as
JSON-RPC invalid params with the reason, not "internal tool error", and
is not logged as a server error.

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

A `decimal` column is `DECIMAL(19,4)`. Postgres stores every digit of
that. SQLite has no exact decimal type: it stores the value as an
integer, or as a 64-bit float past about 15 significant digits, so
`"90071992547409.1234"` reads back rounded there. Keep money that needs
all 19 digits on Postgres, or store it as an `int` count of the smallest
unit.

A create or update refuses a `decimal` its column cannot store as sent,
with a 400 naming the field, on both dialects:

| Value | Refused when | Code |
| --- | --- | --- |
| `"1e-1001"`, `"1e1001"` | the exponent is past ±1000 (`schema.MaxDecimalExponent`) | `decimal_exponent` |
| `"1e16"`, `"12345678901234567"` | it needs more digits before the point than the column has (15 for `DECIMAL(19,4)`) | `decimal_integer_digits` |
| `"1.23456"`, `"0.00001"` | it needs more digits after the point than the column's scale (4 for `DECIMAL(19,4)`) | `decimal_fraction_digits` |

Postgres refused the first two at write time (22P02, 22003) as a 500,
and rounded the third to `1.2346`, while SQLite stored each one as
written. Refusing past the scale, instead of rounding in Go, means both
dialects keep the number the client sent. Trailing zeros are not
counted: `"1.23450000"` is `1.2345` and fits. A `RawType` of
`DECIMAL(p,s)` or `NUMERIC(p,s)` (`NUMERIC(p)` is scale 0) is the
column, so its own precision and scale are the limit: `RawType:
"NUMERIC(10,2)"` takes `"12345678.12"` and refuses `"1.123"`. Any other
`RawType`, a bare `NUMERIC` included, bounds only the exponent.
`schema.DecimalColumn(f)` returns the precision and scale the check
uses. A `Default` is held to the same limit at registration. A value an
older build stored past the column on SQLite still reads back; the next
update that sends it again is refused.

A `decimal` field's `Min` and `Max` are compared in exact decimal
arithmetic, reading the float bound as its shortest spelling: `0.1` is
0.1 and `1e30` is 10^30, not their binary expansions. A whole-number
bound that needs 16 or more significant digits (2^60, spelled in full or
computed) is its exact binary value.

An `int` field holds a 64-bit integer on both dialects: `INTEGER` on
SQLite (64-bit there) and `BIGINT` on Postgres (`BIGSERIAL` for an
`increment` key). Postgres's `INTEGER` is 32-bit, and an `int` column
used to be one, so `2147483648` passed validation and failed the insert.
A table an older build made keeps its `INTEGER` columns: the schema
diff takes them as in sync rather than retyping every `int` column in
the app. Widen one with a hand-written migration (see
[migrations](migrations.md), "Generating migrations from entity changes").

An `int` field takes a JSON number only when its text is a whole number:
`3`, `3.0` and `3e2` pass, `1.0000000000000001` is refused even though
it rounds to the float `1`. A `bool` field takes a number the same way,
only when its text is exactly 0 or 1: `1.0` and `10e-1` pass,
`1.0000000000000001` and `1e-400` are refused. A `date` field reads back as `YYYY-MM-DD` on
every read path and in hooks; a typed read into a `time.Time` field
still decodes it as midnight UTC. An in-process write (`CreateOne`,
`UpdateOne`, `UpsertOne`, batch, the typed repository) also takes a
`date` as a Go `time.Time` or RFC 3339 text at exact midnight, and
stores it as `YYYY-MM-DD`, so a row read back and saved again keeps its
date. A `json` field reads back as the JSON
text it holds, so a document that is the string `"123"` stays a string.

A JSON number past the float64 range (`1e400`) is stored as written in a
`json` field, by a single write and a `_batch` item alike. Any other column answers a validation 400 naming the
field, `not_finite` (or `not_integer` on an `int`), not `invalid_json`.

A `relation` field with a `to` target (e.g. a field named `author_id`, type
`relation`, `to: users`) declares a *BelongsTo*: the field's own column
holds the foreign key. `Define` derives a matching `Config.Relations` entry
automatically, so AutoMigrate emits the FK constraint and `?include=author_id`
eager-loads the related row, so you do not have to declare the relation twice. An
explicit relation you declare for the same name always wins and is never
overwritten. Has-many relations (`many: true`) keep their FK on the *other*
table and must be declared explicitly via `HasMany`/`Relations`.

A relation column is `TEXT`, except when the target's key is an `int`
field: then it takes the key's integer type, `BIGINT` on Postgres and
`INTEGER` on SQLite. Postgres refuses a `TEXT` foreign key to an integer
column, and on SQLite a `TEXT` column stores `10` as `'10'`, so a range
filter (`?author_id_gt=2`) or a sort on the foreign key compared text and
put 10 before 2. A target key that is itself a relation column to an
`int` key counts the same way, however long the chain. A `TEXT` foreign
key an older build made on SQLite shows up in the schema diff as a
destructive retype; boot never applies it, and the table rebuild a
reviewed migration runs stores the keys as integers. Either way a write
stores such a reference in canonical decimal:
`"01"`, `"+1"` and `1.0` are stored as `1`, and a value that names no
integer (`"owls"`) is a 400 with the reason `not_integer`.

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
A `relation` field with `many: true` gets the same scope gate on the
id its own column stores, though it has no derived relation and no FK
constraint. An update that sends back the id the row already stores
is not checked again, for either kind.

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
with that name always wins and is left untouched. A declared owner
column, like the `MultiTenant` tenant column, still appears in no
OpenAPI request body, since the server sets both: the create body
neither lists nor requires them.

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

For declared entities this rule is lint-enforced: an
auto-exposed entity (`crud` defaults on, or `mcp: true`) with PII-shaped
field names and no `owner_field` / `access` / `multi_tenant` while
`app.auth` is disabled is an **error** from `gofastr validate`, a
prominent warning from `gofastr generate`, and an `unscoped-pii` finding
from `gofastr audit lint`. See [app specs](app-spec.md) → "Unscoped
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
app spec decoder returns a validation error for the same mismatch.

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

A write made under `AllowCrossOwner` publishes its `_events` update to
the row's owner, not the actor: the event's owner and tenant come from
the record's own columns, so promoting another member's booking reaches
that member's stream and not yours. A record that does not carry those
columns (a Hidden owner field, which an update's response leaves out, or
a delete, whose payload is the key alone) takes them from the stored
row, read before the write changes it. The same holds for the outbox
row's owner and for update, delete, batch, transition, restore and
purge writes alike.

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

### App spec spelling (`read_scope:`)

In a `gofastr.yml` app spec the same scope is declared under the entity,
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

The app spec decoder validates the block where a typo matters, and fails
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
LIKE pattern so the comparison is consistent across dialects. The
lowering is Unicode-aware on purpose, so an accented capital (`É`) finds a
stored `é` on Postgres; the same rule folds compatibility look-alikes onto
ASCII (the Kelvin sign `K` finds `kilo`), on `?q=` and `_like` alike. That
is accepted, not refused.

**Unstorable text is a 400.** A `?q=` holding invalid UTF-8 or a NUL byte
answers 400 naming `q`, like any other filter value: Postgres refuses it
in a text bind. `filter.SearchConditions` returns that error.

**Back-compat.** An entity WITHOUT `SearchFields` ignores `?q=` exactly
as before: no behavioural change.

**The `q`-column edge case.** An entity WITH `SearchFields` that also
has a physical column named `q`: plain `?q=value` means **search** (the
OpEq filter on the `q` column is dropped). Suffixed ops (`?q_like=`,
`?q_gt=`, …) and the explicit `?q[eq]=value` still filter the column
normally.

Column names must be known, non-Hidden, and String/Text-typed;
`entity.Define` panics otherwise (the app spec decoder returns a
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
| `_like`  | case-insensitive literal `contains` (`LOWER(col) LIKE '%value%'`) |
| `_in`    | `IN (…)`, comma-separated, capped at 1000 |

**The bracket form.** Every operator also has a word you can put in
brackets after the field name: `?price[gte]=5`, `?status[in]=a,b`,
`?title[like]=foo`, `?check_in[eq]=2026-11-01`. The words are the
suffixes without the underscore, plus `eq` for equals: `eq`, `ne`,
`gt`, `gte`, `lt`, `lte`, `like`, `in`. The text before the brackets is
always the whole field name (or its `WireName`), never cut at a suffix,
so `check_in[eq]` filters the `check_in` field and `check[in]` filters
`check` with `IN`. `%5B` and `%5D` decode to the same key. Any other word
in the brackets (`?price[within]=5`) is a **400** naming it. A bracket
on an unknown or Hidden field is the same unknown-filter 400 any unknown
key gets, so the form tells nobody a Hidden field exists. A bracket key
follows every rule a suffix key does: it is sent once (`[in]` reads every
occurrence), its operator must suit the field's type, and `?f_in=` beside
`?f[in]=` are two keys, two IN lists ANDed.

`sort` names a field, with a `-` prefix for descending. Several fields
go comma-separated (`sort=status,-created_at`) or as repeated `sort`
keys, at most 16 in all; an unknown field is a 400.

`_ne` follows `=`'s NULL semantics rather than SQL three-valued intuition:
a row whose column is NULL matches neither `=` nor `!=`, and no
`OR column IS NULL` arm is added. Combine with `_in` (or a `?where=`
`ne`/`in` leaf) when you want "everything except these".

`_like` on a number, date or Bool column, `_gt`/`_gte`/`_lt`/`_lte`
on a Bool column, and any filter on a JSON column is a 400: see
"Operators must suit the field's type" below. `_like` follows the `?q=` case contract (see "Case
contract" above): `?status_like=OPEN` matches `open` on both dialects,
where a bare `LIKE` was case-sensitive on Postgres only.

Every key is its own predicate, ANDed: `?score=5&score_gt=3` keeps both,
and the same query string always compiles to the same SQL. Two `_in`
keys that name one column, its name and its `WireName`
(`?author_id_in=1,2&writer_in=3`), are two lists ANDed, not one union;
so are the same two terms in a scoped include filter
(`include=comments(author_id_in=1|2,writer_in=3)`). Code that builds its
own one-filter-per-value `filter.ParsedFilter` lists tags each value with
its key (`pf.InList(key)`), and a renderer merges adjacent values into
one `IN (...)` only while `f.SameINList(next)` holds.

**A filter key is sent once.** A key other than `_in` repeated in one
query string (`?price_lt=100&price_lt=5`) is a **400** naming the key,
`LenientFilters` included (`filter.repeated_filter`). A nested relation
filter follows the same rule: `?author.age_lt=100&author.age_lt=5`, or
the bracket form `?author.age[lt]=…` twice, is the same 400 naming the
dotted key. Before, the first value ran and the rest were
dropped without a word, so the result was wider than asked and a proxy
that reads the last value saw a different query from the one the
database ran. Only `_in` reads every occurrence (below). A field whose
name ends in a suffix but whose name before it is no field (`opt_in`
with no `opt` field) filters as itself, at the top level, in
`?rel.field=` and in `include=rel(…)`.

**A bare key that reads two ways is a 400.** When an entity has both
`check` and `check_in`, `?check_in=x` could mean `check_in = x` or
`check IN (x)`. The parser does not guess: it answers **400**
(`filter.ambiguous_filter`) naming both readings and the keys that pick
one, `?check_in[eq]=x` or `?check[in]=x`. Hidden fields never count, but
NoQuery fields do. This holds at the top level (`LenientFilters`
included), in `?rel.check_in=` and in `include=rel(check_in=…)`. Keys
that read one way are unchanged. Such a pair of fields registers fine;
the generated OpenAPI document, MCP list tool, CLI and SDK readmes spell
the two readings in bracket form and keep the suffix form for every
other key.

**A wire key names one column.** Two fields resolving to one wire key
(`WireName`, or a bare column's name and its camelCase form) are refused
at `Define`. So is a `WireName` that is another field's column name: a
body names a field by either, and entityui's forms post by column name,
so `content` would write whichever field publishes as `content`.
`CrudHandler.WireKey(column)` returns the key a column's value sits under
in the rows `ListAll` and `GetOne` return.

**Repeating `_in` unions.** `?tag_in=a,b&tag_in=c` matches all three
(the same key repeated; a different key on the same column ANDs, above).
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
`?scor_gt=5` or `?scor[gt]=5`, returns a **400** naming the bad key (with a "did you
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
data-exposure hazard. Lenient drops keys that name no field (a bracket
on an unknown field included); a key that names a field but cannot be
read, an ambiguous bare key or an unknown bracket word, is still a 400.

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
defaults to `eq`; use `"values":[...]` or a comma string in `"value"`
with `op:"in"`, not both) or a **group** (`{"and":[...]}` /
`{"or":[...]}`, non-empty, with no leaf member beside it).

The tree decodes strictly, the rule every client-borne JSON body
follows (a `?cursor=` token included): a key repeated in one object, two
keys that differ only in case (`"value"` and `"VALUE"`), an unknown
member (`"vaule"`), a member name in another case (`"Field"`), and a
member the node's op does not read (`"values"` on an `eq` leaf) are each
a **400**. Plain JSON decoding kept the last duplicate and dropped the
rest, so the tree that ran was not the one a proxy reading the first key
saw, and a typo compared the field with the empty string.

Operators are the same set as the flat params: `eq, ne, gt, lt, gte, lte,
like, in` — `ne` is the `!=` twin of `_ne` above, NULL semantics included.

**Operators must suit the field's type.** `like` works only on a
String, Text, Enum or UUID column, and the ordered comparisons (`gt`,
`lt`, `gte`, `lte`) are refused on Bool: SQL orders `false < true`, but
no caller means that. A JSON column takes no operator at all, plain
equality included: SQLite stores JSON as text and compared the text,
while Postgres compares JSONB values, so `?meta={"x":1.0}` found a stored
`{"x":1}` on Postgres and nothing on SQLite, and key order and spacing
split them the same way. Neither engine can give the other's answer in
SQL, so both refuse. Filter on a scalar column instead, or promote the
JSON key you query into its own field. `eq`, `ne` and `in` suit every
other type. The rule is `filter.CheckOpType` (one predicate,
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

**Values must be of the field's type.** Every surface above, plus a
`?cursor=` token's keyset values and the DSL's `after()`, checks each
value with `filter.CoerceValue` and binds it as that type:

| Column type | Accepted value | Bound as |
| ----------- | -------------- | -------- |
| Int | a base-10 integer | `int64` |
| Float | a decimal literal (`1`, `-2.5`, `1e3`); no hex, `_`, `NaN` or `Inf` | `float64` |
| Decimal | a decimal literal | the text sent, every digit kept |
| Bool | exactly what `strconv.ParseBool` accepts: `1 t T TRUE true True` and `0 f F FALSE false False` | `bool` |
| Timestamp | RFC 3339; a zone-less `YYYY-MM-DDTHH:MM[:SS[.fff]]` (what `<input type=datetime-local>` sends) or the same with a space for the `T`; or a bare `YYYY-MM-DD`. A value with no zone reads as UTC, a bare date as midnight UTC. The UTC year must be 1 or later: Postgres refuses year 0000 | text in the layout crud stores (`schema.TimestampLayout` in `core/schema`: UTC, six fraction digits; `schema.FormatTimestamp` writes it), so `…Z` and `…+02:00` match the stored row on SQLite |
| Date | `YYYY-MM-DD`, year 0001 or later | the text sent |

A Relation column that is a belongs_to's foreign key to an integer key
holds that integer and is checked as an Int (`entity.ValueFields`).
`/openapi.json` types it the same way: an `integer` property and an
integer `?<field>=` filter, where a relation to a `uuid` key is a
`uuid`-format string and one to any other key a plain string.
String, Text, Enum, UUID and other Relation columns take any text. A value of
another type (`?n=abc` on an Int, `?active=yes` on a Bool) is a 400
naming the field and the type on both dialects. `LenientFilters` does not lift it:
lenient mode drops unknown keys, not mistyped values. A `like` value is
a substring and is not type-checked.

Every value, a `like` substring included, must also be one Postgres can
bind, or it is the same 400: text that is valid UTF-8 with no NUL byte
(`?title=a%FFb` and `%00` are refused), a Decimal exponent within ±1000
(`1e1001` is refused), and a Timestamp or Date in year 1 or later (in
UTC for a Timestamp; Postgres has no year 0). Postgres refused each at
bind time with a 500 while SQLite compared the bytes and answered 200.

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
columns, each leaf carrying its column type so values bind typed. That copy is the
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
column, an operator the column's type refuses, or a value not of the
column's type, returns an error
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
        Singular:    "Invoice",
        Plural:      "Invoices",
        TitleFields: []string{"number"},    // names a record in lists, drawers, breadcrumbs
        Columns:     []string{"number", "amount", "status"},
        Nav:         &framework.EntityNav{Group: "billing", Icon: "receipt", Order: 1}, // HideCount drops the row's record count
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
    "title_fields": ["number"],
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
| `TitleFields` | The fields that name a record in lists, drawers, pickers and breadcrumbs, their values joined with " · "; none may be `Hidden`. A `Relation` field contributes the related record's own title, read through that entity's gate: a subscription titled by `customer_id` and `plan_id` reads "Ada Lovelace · Pro". A relation is followed one hop, and a part the caller may not read or that is `NoQuery` is left out. The first carries the list's record link. Unset, a `name` or `title` field names it, else the first `String` column that is not omitted or `NoQuery`, else the singular |
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

- **Fields.** Every name in `Columns`, `TitleFields`, `Facets`, `Card`,
  `Form` (items, rows, nested sections) and the keys of `Fields` must
  exist and not be `Hidden`.
- **Duplicates.** `TitleFields`, `Columns`, `Facets` and `PageSizes`
  refuse a repeated entry, naming the duplicate.
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

## Record ids

`EntityConfig.PrimaryKey` names the primary-key columns in order. Left
nil, the key is the one column `id`, which `Define` injects as a generated
UUID unless `Fields` declares it.

A plain join table keeps that `id` and holds its pair unique with an
index. Every surface then names a row by one value, and another entity
can belong to it:

```go
app.Entity("enrollments", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "student_id", Type: schema.String, Required: true},
        {Name: "course_id", Type: schema.String, Required: true},
        {Name: "grade", Type: schema.String},
    },
    Indices: []entity.Index{{Columns: []string{"student_id", "course_id"}, Unique: true}},
})
```

Name more than one column for a composite key when the columns are the
record's identity: a table an existing schema already keys that way, or
records the API should address by the pair. The links and the limits
below follow from it:

```go
app.Entity("enrollments", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "student_id", Type: schema.String, Required: true},
        {Name: "course_id", Type: schema.String, Required: true},
        {Name: "grade", Type: schema.String},
    },
    PrimaryKey: []string{"student_id", "course_id"},
})
```

`Define` injects no `id` column when `PrimaryKey` leaves it out, and panics
when a key column is repeated or is not a declared field. A composite key
column must not be `Hidden` or `NoQuery`: the API names a record by its
key values, so it has to return and filter on each. Enforce other
uniqueness with a unique index. Every key column, `id` included, is a
string, text, image, file, integer, enum, UUID or relation field (an
image or file column stores its path as text); `Define` panics on
a float, decimal, bool, date, timestamp or JSON one, whose values have
more than one spelling (Postgres reads `2024-1-2` as the date
`2024-01-02`), so one record would answer to several ids. For the same
reason a key column sets no `RawType`, except an integer type such as
`BIGINT` on an `Int` column: `CITEXT` or a Postgres `UUID` matches
either case. A nil `PrimaryKey` means the one `id` column; an
empty `[]string{}` names no column and also panics. An entity always has
a key: only a raw `migrate.Table` describes a table without one. The
table gets
each key column `NOT NULL` and a `PRIMARY KEY (student_id, course_id)`
constraint. Changing the key of an existing table is a migration; see
[Primary-key changes](migrations.md#primary-key-changes).

### Relations and composite keys

A foreign key has one column per column of the key it references, in key
order, and `Relation.ForeignKey` lists them: `[]string{"author_id"}` for a
one-column key. The constructors take the columns as arguments:

```go
app.Entity("orders", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "store", Type: schema.String, Required: true},
        {Name: "no", Type: schema.Int, Required: true},
    },
    PrimaryKey: []string{"store", "no"},
    Relations:  []entity.Relation{entity.HasMany("lines", "lines", "order_store", "order_no")},
})
app.Entity("lines", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "order_store", Type: schema.String},
        {Name: "order_no", Type: schema.Int},
        {Name: "sku", Type: schema.String},
    },
    Relations: []entity.Relation{entity.BelongsTo("order", "orders", "order_store", "order_no")},
})
```

- a `belongs_to` references its target, and its own entity holds the
  columns;
- a `has_one` or `has_many` references its own entity, and the target
  holds the columns;
- a `many_to_many` links two one-column keys through its pivot. Link a
  composite-keyed end through a join entity instead (below).

`entity.CheckRelationKeys` applies the rule, and boot, `?include=`,
nested filters, `EagerLoad` and writes refuse a relation that breaks it:
one column against a composite key is `entity.ErrCompositeKey`, any
other mismatch `entity.ErrForeignKeyShape`. The columns of a longer
foreign key are fields the holding entity declares, spelled as declared
and each typed like the key column it references; boot refuses a
repeated column, an undeclared one (`ORDER_NO` over a field `order_no`
included), a column whose type differs from its key column's (`string`
against `int`), or a column a transition writes (the `states` field or a
move's stamp, since a move changes the reference with no check) before
any DDL runs. The transition rule covers every declaration over the
table, another version or another entity name sharing it included, and
registration applies it, so an app that skips migrations gets it too. The migrator emits one constraint,
`FOREIGN KEY (order_store, order_no) REFERENCES orders(store, no)`, and
an index on the column tuple.

A write sets a `belongs_to`'s multi-column reference whole or not at
all. (A reference only the parent's `has_many` declares gets no foreign
key constraint and no write-side checks, one column or several: declare
the `belongs_to` on the child to have them.) Among the columns the
write takes from the body, one that names some and not the others, or
sets some to null and others to a value, is a 400 carrying
`validation.partial_foreign_key` on each column, server writes
included, so a PATCH cannot leave a line pointing half at its old order
and half at a new one. The check that refuses a reference to a row the
caller cannot read (404) looks the target up by the whole tuple. A
tuple with a null column names no row, as a join on it matches none,
and a blank string is a value.

The check reads each reference as the row will hold it, not as the
body spells it. A column the write does not take from the body (a key
column on update, a `ReadOnly` or `Hidden` field, the owner and tenant
columns) keeps what the row would have without it: a create holds the
field's default, or the caller's tenant for the tenant column, and a
PATCH holds the stored value. An `AutoIncrement` column holds nothing
known until the database assigns it, so a create naming the rest of a
reference through one is refused. So a line keyed on `(order_store,
line_id)` keeps its store on every update: a PATCH of `order_no` alone
moves the line within its stored store, and a PATCH naming both columns
is checked against the stored store, the reference the row ends up
with. A reference keyed through the tenant, `(tenant_id, order_no)`,
reads the caller's tenant. A write that names only columns it does not
take moves nothing, and each reference is read on its own, so moving
one leaves another that shares its kept column alone. An upsert
ends as an insert or a conflict update, so it checks both: the insert
as a create, and the update, which cannot know what a column it keeps
holds (it keeps a generated column's stored value). The bulk
`TypedQuery.UpdateAll` cannot know either, since its rows each hold
their own. Both do know the tenant and owner columns: every row they
touch is the caller's and keeps them, so a reference through either
reads the caller's tenant or owner there too (a bulk update with no
tenant in its context, or under `owner.AllowCrossOwner`, reaches other
callers' rows and does not). Otherwise, a body that moves
part of a reference while the write keeps another of its columns is a
400 carrying `validation.kept_foreign_key`.

A join table keyed on the pair it links is an entity of its own, with
a payload: it belongs to each side, each side has many of it, and a
`many_to_many` can read through it.

```go
app.Entity("courses", framework.EntityConfig{
    Fields: []schema.Field{{Name: "title", Type: schema.String}},
})
app.Entity("students", framework.EntityConfig{
    Fields: []schema.Field{{Name: "name", Type: schema.String}},
    Relations: []entity.Relation{
        entity.HasMany("enrollments", "enrollments", "student_id"),
        entity.ManyToMany("courses", "courses", "enrollments", "student_id", "course_id"),
    },
})
app.Entity("enrollments", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "student_id", Type: schema.String, Required: true},
        {Name: "course_id", Type: schema.String, Required: true},
        {Name: "grade", Type: schema.String},
    },
    PrimaryKey: []string{"student_id", "course_id"},
    Relations: []entity.Relation{
        entity.BelongsTo("student", "students", "student_id"),
        entity.BelongsTo("course", "courses", "course_id"),
    },
})
```

`GET /enrollments/{student}/{course}?include=student,course`,
`GET /students/{id}?include=enrollments.course`, `?include=courses`,
`?enrollments.grade=A` and `?courses.title=Art` all work. A three-way
association is the same shape with three `belongs_to` and a three-column
key.

When a registered entity owns a `many_to_many`'s `Through` table, every
read through the pivot applies that entity's soft delete and its owner,
tenant and read scopes: a soft-deleted enrollment, or another owner's,
links nothing. Its `BeforeList` hooks run on `?include=` and nested
filters too, so a link `GET /enrollments` would not list for the caller
links nothing either, and a hook that refuses refuses the read. Boot refuses the relation when the entity does not
declare both `LocalKey` and `ForeignKeyTarget`, since the migrator then
creates no pivot of its own. `Through` matches the entity's `Table`
ignoring case, and must spell the schema the same way: `public.enrollments`
against a `Table` of `enrollments` (or the reverse) may be one table under
the connection's `search_path`, so boot refuses it and so does every read
through the relation. A `cascade_write` link writes its pivot
row through that entity's own create: its `access` create permission
and `CRUD: false` refuse the link, its tenant and owner columns are
stamped, its hooks run, and its `belongs_to` checks read the row as the
two link columns (matched to its fields ignoring case, as the database
matches them) with every other column at its default: a tagging
that belongs to a grant keyed on its `(post_id, tag_id)` links a tag
only through the caller's grant.

An `AfterList` hook on a composite-keyed child that replaces rows in an
include is matched back to them by the whole key. A `cascade_write`
`has_one` or `has_many` into a composite-keyed child is refused at boot:
it tells an existing child from a new one by the child's one key column.
For the same reason a `cascade_write` relation over a multi-column
foreign key is refused at boot.

The framework names a record by its primary-key columns,
`Entity.KeyColumns()`, in declared order. Reads, writes, the keyset
cursor's tiebreak, `UpsertOne`'s conflict target, event payloads, audit
`record_id`s and hook ids all go through them.

Where an API takes or returns one record id as a string (`GetOne`,
`OnBeforeDelete`, `hook.GetPayload.ID`, an audit row), that string is
`entity.JoinKey` of the key values. For a single-column key, the usual
case, it is the value itself. `entity.SplitKey(id, n)` reverses it and
refuses an id with the wrong number of values, or a composite id spelled
other than `JoinKey` spells it (`a/c1` is the id of `a` and `c1`;
`%61/c1` is refused), with `entity.ErrBadKey`, so a record has exactly
one id. A one-column id is the value itself, so every string is its own
spelling.
`entity.KeyValues(id, cols, fields)` reads an id into the values a lookup
binds, an integer key column's only in canonical decimal (`1`, never `01`).
Pass it `entity.ValueFields(reg, e)` rather than `e.GetFields()`: a
relation column whose target has an `int` key holds an integer, and
`ValueFields` types it `int` (`entity.RelationHoldsInt` answers it for
one column). `entity.RowKey` reads the id out of a row.

On an `Unmanaged` entity (a view, or a table created elsewhere) the
database chose the key column types, and equality may match a second
spelling: a Postgres `UUID`, `CITEXT` or nondeterministic collation
matches either case, a SQLite `NOCASE` column too, and a `CHAR(n)`
matches the value with or without its padding. Each read and write by
id there reads back the key columns of the row it reached and keeps the
row only when its own `RowKey` is the id; a write that reached another
row fails and rolls back. So `GET /claims/550E8400-…/x` is 404 for the
row keyed in lowercase, and a `CHAR(5)` row answers to its padded id
(`ab%20%20%20/s`). A SQLite key declared under a stricter collation
than its column (`PRIMARY KEY (code COLLATE BINARY, scope)` on a
`NOCASE` column) holds rows the id's equality matches together: a read
returns the id's own row, and a write that would reach more than one is
refused with 404. Postgres checks the rows a delete reached through
`RETURNING`; SQLite, which cannot return rows from a virtual table such
as an FTS5 index, first reads the rows the delete's own `WHERE` (key,
owner, tenant, live or trashed) reaches, inside the same transaction. Send ids as the API returns them.

### What a composite key cannot do

Boot refuses a declaration that breaks one of the first four rules,
before any DDL runs:

- A key column is a string, text, integer, enum, UUID or relation field,
  with no `RawType` but an integer type.
- A foreign key onto a composite key lists one column per key column;
  one column is `entity.ErrCompositeKey`.
- A `many_to_many` links two one-column keys. Link a composite-keyed end
  through a join entity that belongs to both sides.
- A `cascade_write` relation has a one-column foreign key, and a
  `cascade_write` `has_one` or `has_many` needs a child keyed on one
  column.
- `Entity.KeyColumn()`, for code that needs one column, returns
  `entity.ErrCompositeKey`. Read `Entity.KeyColumns()` instead.
- A record keeps its key: the edit form shows the key columns read-only,
  and changing the key of a table is a
  [migration](migrations.md#primary-key-changes).

### Composite keys over HTTP and MCP

A composite-key entity's record routes take one path segment per key
column, in key order, each value path-escaped:

| Surface | Single key | `PrimaryKey: {"student_id", "course_id"}` |
|---|---|---|
| REST record | `/api/enrollments/{id}` | `/api/enrollments/{student_id}/{course_id}` |
| State move | `.../{id}/transitions/{key}` | `.../{student_id}/{course_id}/transitions/{key}` |
| OpenAPI path params | `id` | `studentId`, `courseId` (wire names, type string) |
| MCP get/update/delete/move args | `id` | `student_id`, `course_id` |
| Batch update item | `{"id": ..., ...}` | `{"studentId": ..., "courseId": ..., ...}` |
| Batch delete `ids` | the id | `entity.JoinKey` of the values, `"s1/c1"` |

The move's wildcard is `{key}` unless a key column is itself named `key`;
then it is `{key_}`. `crud.KeyPath(cols)` and `crud.TransitionPath(cols)`
build these route tails, and `crud.PathRecordID(r, cols)` reads the id
back off a request, for a handler mounted beside the CRUD routes.
`/api/llm.md` lists the path parameters per key column.

entityui and the admin draw a composite record at `<base>/<v1>/<v2>`.
The create form sets every key column; the edit form shows them
read-only, since a record keeps its key, and a duplicate leaves them
blank, as it does a unique index's fields. A key value holding `/` is
escaped in its segment (`/enrollments/a%2Fb/c1`), and its screen
answers there as REST and MCP do.

The `framework` package re-exports `JoinKey` and `RowKey` for app code
that builds ids without importing `framework/entity`. The generators
take the key the same way the routes do: an app spec's
[`primary_key:`](app-spec.md#composite-keys-primary_key), the
[typed repository](#the-typed-repository), the [Go and JS SDKs](sdk.md)
and the [app CLI](app-cli.md) each name a record by one value per key
column.

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

Generate Go from a `gofastr.yml` app spec:

```bash
gofastr generate app gofastr.yml
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

### The typed repository

Each `<entity>.go` carries a repository that runs the pipeline the HTTP
routes run (transaction, hooks, events, owner and tenant scope) with no
HTTP round trip. For a `posts` entity:

```go
posts := entities.NewPostsRepo(app)

p, err := posts.Create(ctx, entities.PostsInput{Title: "Hello", Published: new(false)})
p, err = posts.Update(ctx, p.ID, entities.PostsPatch{Views: new(0)})
live, err := posts.Query().Where(entities.PostsStatus.Eq(entities.PostsStatusLive)).Find(ctx)
```

- `PostsInput` is the create payload, taken by `Create`, `BatchCreate`
  and `FirstOrCreate`. A required field with no default is a value and
  is always sent. Every other field is a pointer: nil takes the column
  default, and `new(false)` stores false over a `true` default.
- `PostsPatch` is the update payload, taken by `Update` and
  `BatchUpdate`. Every field is a pointer, and nil leaves the field as
  stored. A pointer to `false`, `0` or `""` writes it to a bool, int,
  float, string or text field. For any other type `""` is a blank: it
  clears an optional field to null, and a required one fails
  validation.
- A `cascade_write` relation (`has_one`, `has_many`, `belongs_to`) puts
  its children in both payloads. `PostsInput.Comments` takes
  `[]CommentsInput`; `PostsPatch.Comments` takes
  `[]PostsCommentsNested`, which embeds `CommentsPatch` beside an `ID`.
  A set ID updates that child and an empty one creates a child (for a
  `has_one`, it updates the existing child if there is one).
- Every write returns the row as stored, server-generated fields
  included.
- A timestamp column's `Eq`, `Gt`, `Gte`, `Lt` and `Lte` take a
  `time.Time`, a `*time.Time` or RFC 3339 text and bind it in the
  layout crud stores, so `…10:00:00Z` matches the row stored as
  `…10:00:00.000000Z` on SQLite and ranges compare instants.
- An enum field with declared values gets a value type and one constant
  per value (`PostsStatusValue`, `PostsStatusDraft`, `PostsStatusLive`).
  The model, both payloads and the column
  (`framework.NewEnumColumn[PostsStatusValue]`) take that type, so
  `PostsStatus.Eq` refuses a plain string variable. A value that does not
  camel-case to a Go identifier, or whose constant name another name in
  the file already uses (`past_due` next to `past-due`, or `value`, which
  would repeat `PostsStatusValue`), gets no constant: the file lists it
  in a comment, and `PostsStatusValue("N/A")` still works.
- An entity with `States` gets one `<Entity>Move` constant per move and
  `Transition(ctx, id, move)` (on a composite key, one argument per key
  column in place of `id`, as below), which runs
  `CrudHandler.RunTransition`.
  System moves are included, since the repo is the Go caller they exist
  for. With enforced States, stamps leave both payloads and the state
  field leaves the patch ([States](states.md)).
- A composite-key entity's record methods take one argument per key
  column, in key order: `Get(ctx, studentId, courseId)`, and likewise
  `Update`, `Delete`, `Exists` and `Transition`. The model has no `ID`
  field; its key columns are ordinary fields. `EnrollmentsID(studentId,
  courseId)` spells the record id (`framework.JoinKey` of the values)
  that `BatchUpdate`, `BatchDelete` and the lifecycle subscriptions
  (`OnEnrollmentsDeleted`'s `id`) use.

Hidden and ReadOnly fields stay in both payloads. The pipeline skips them
unless ctx carries `crud.WithServerWrites`, the same rule
`CrudHandler.CreateOne` follows. Fields the pipeline never takes from a
payload are left out: an `auto_generate` column from both, and the owner
field and the `multi_tenant` tenant column from the patch.

The payload and move types share the package with the column vars, so a
field named `input`, `patch` or `move`, or a field `status_value` next
to the enum `status`, fails `gofastr generate` with the name the two
meet in. Rename the field.

An app spec that declares `app.module` also emits a flat `package main` at the
root (`main.go` plus `app.go`, `screens_register.go`, one `screen_<name>.go`
per screen, and `stubs.go` for endpoint/seed stubs). These are owned Go you
  read, edit, and commit; no `DO NOT EDIT` header. See
[App specs](app-spec.md) for the full app spec shape, including the
[generated screen file layout](app-spec.md#generated-screen-files). To add
in-page dynamic behavior to those screens (sort, paginate, mutate without a
  reload), build islands: the cookbook is
[interactive-patterns](interactive-patterns.md).

`gofastr generate app [spec]` reads the app spec named (default
`gofastr.yml`). Useful flags:

- `--dry-run` lists generated files without writing.
- `--json` emits machine-readable output.
- `--out=<dir>` scaffolds into a subpackage instead of the module root (also
  settable as `app.output_dir` in the app spec); useful for monorepos and
  examples that host their own Go test package.
- `--force` overwrites existing files. `generate` is one-shot: with no
  `--force` it refuses to write into a directory that already holds any target
  file, listing the conflicts, rather than clobbering owned code.
- `--add` writes only the files that don't already exist, never overwriting.
  Pass a partial yml (e.g. just new entities) to add pieces to an existing
  project. Entity declaration orders continue after the existing set. See
  [Additive generation](app-spec.md#additive-generation---add).

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

See [Quick scaffolds](app-spec.md#quick-scaffolds-generate-entityscreen) in
the App specs guide for the relationship between stubs and full yml.

For arbitrary configured generators (not a full app spec), use a
`codegen:` section in `gofastr.settings.yml`. See [Codegen](codegen.md) for
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
`Create<Entity>` take `<Entity>Input`, whose fields are pointers too,
except a required field with no default.

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
update paths. A body is one JSON value: anything but whitespace after the
object is refused with `400`.

Every successful single-record response has one stable envelope:

```json
{"data":{"id":"p1","title":"Hello"}}
```

This applies to create (`201`), get (`200`), PUT (`200`), and PATCH (`200`).
Lists keep `{"data":[...]}` plus pagination metadata.

Errors are JSON everywhere in the API namespace, whatever missed. A handled
record miss (e.g. `GET /api/posts/404-nope`) answers
`application/json` with
`{"error":"not found","reason":"not_found","success":false,"code":404}`.
`reason` is the stable code a client branches on; `error` is the text in
the request's locale (see [i18n](i18n.md) → "The error contract").
A write the database refuses on a constraint answers `409 Conflict`, not
`500`: a UNIQUE violation (a duplicate key) and a FOREIGN KEY violation
(a create or update that points at a missing row, or a delete of a row that
other rows still reference) on SQLite, Postgres and MySQL. The body names
neither the constraint nor the table; the driver's message goes to the
server log only. When the refused constraint is one the entity declares
(a `unique` field, a unique column index, a relation's foreign key) the
body carries the fields the caller sent in the validation shape, so a
form shows the refusal on the control:
`{"error":"conflict","reason":"conflict","success":false,"code":409,"fields":{"number":["is already in use"]},"field_reasons":{"number":["unique_taken"]}}`.
A relation's message is "refers to a record that does not exist". A
column the caller did not send (the owner column of a per-account
index) is not named, and a conflict on a `hidden` or `no_query` field
the caller sent stays bare, so a probe cannot learn which value exists. SQLite's foreign-key refusal names no column, so on
SQLite it stays bare too. A NOT NULL refusal on a declared field (a
column the database holds `NOT NULL` that the entity does not mark
`required`) answers `400` in the same shape with "is required" on it,
instead of a `500`; on a `hidden` column the server fills, it stays a
`500`, since the caller cannot fix it.
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

Numbers inside the document read back as `float64` in Go (a hook or a
`FindAll` caller sees what `encoding/json` gives), except an integer past
±2^53, which reads as `int64` so `9007199254740993` comes back exact. An
integer `int64` cannot hold, or one past ±2^53 spelled with a point or
an exponent (`9007199254740993e0`), reads as the `json.Number` it was
written as; a Before-hook sees it the same way, and it is stored as
sent.

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
Their input schemas take what the route takes: a `json` field accepts any
JSON value (an array as well as an object), a `decimal` field takes its
text (`"12.34"`) as the route does, an optional field accepts
`null` (except the state field on a create under enforced
[States](states.md), which must start at an initial value), and the
owner and tenant columns the server stamps on create are never
required. Results carry numbers exactly as the route wrote them, so
an integer above 2^53 is not rounded, and an `int` argument spelled with
a point or an exponent past 2^53 (`9007199254740993e0`) is refused as
the route refuses it, never stored rounded. The route sees the `/mcp` caller's
address (`RemoteAddr`, plus `X-Forwarded-For` and `X-Real-IP` as sent), so
a per-IP rate limit on the entity routes counts each MCP caller on its
own.

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

**Both doors require a signed-in caller by default.** The HTTP route
answers 401 when the request carries no user, sends
`Cache-Control: no-store` on every response, and refuses a cross-site
write that a plain HTML form could send (no `Content-Type`, urlencoded,
multipart or `text/plain`) with 403. A JSON body from another origin
passes; the browser's CORS preflight already stands in front of it. The
`MCPHandler` twin is invoked directly, with no route middleware, so it
carries the same default as its own gate: an authenticated caller.
Declare something stricter for the twin with `MCPGate`, or set
`Public: true` for an endpoint that really is anonymous; `Public` opens
both doors. Under any of the three, a caller holding an API token's scopes
must also hold `<table>:read` for a GET or HEAD endpoint, or
`<table>:write` for any other method, before the twin runs or shows in
`tools/list`: the scope `auth.RequireAPIScopes` asks of the HTTP route.
Sessions are not narrowed.

```go
entity.Endpoint{
    Method: "POST", Path: "{id}/publish", MCP: true,
    Handler:    publishHTTP,
    MCPHandler: publishTool,
    MCPGate:    auth.MCPRole("admin"), // default: any authenticated caller
}
```

See [plugins](plugins.md) → MCP tool gating.

### Endpoints from another file

`App.EntityEndpoints` queues endpoints for an entity that a later
`App.Entity` or `TryEntity` call registers. That call merges them into the
config's `Endpoints`, so they pass the same route and MCP checks as
endpoints written in the config. The generated
`entities/endpoints.gen.go` uses it to attach the
[settings endpoints](endpoints.md) to entity files it does not own:

```go
app.EntityEndpoints("posts", framework.Endpoint{
    Method: http.MethodPost, Path: "{id}/publish", Handler: publishHandler,
})
app.Entity("posts", postsConfig)
```

Calling it after the entity registered panics, and `Start` returns an
error while endpoints are still queued for a name no `App.Entity` call
registered, such as a misspelled table.

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
