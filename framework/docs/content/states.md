# States

An invoice's status is a business rule, not a free field. `EntityConfig.States`
declares the rule beside the entity it governs: which Enum field holds the
state, the values a record may start at, and the named moves that change it.
Unless `Advisory` is set, the state field and every stamp change only through
a move, whatever the write comes from: REST, MCP, `_batch`, a cascade write,
`UpsertOne`, a typed query, a hook. Two whole-database operations stand
outside the rule, `App.ImportData` and `App.EraseUserData`; see
[outside the check](#outside-the-check).

A move is one conditional UPDATE pinned to the values it starts from, so two
callers racing to move the same record cannot both win. The second sees a
409 naming the moves that are open.

## Quickstart

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import "github.com/DonaldMurillo/gofastr/core/schema"
-->
```go
app.Entity("invoices", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "number", Type: schema.String, Required: true},
        {Name: "status", Type: schema.Enum,
            Values:  []string{"draft", "issued", "overdue", "paid", "void"},
            Default: "draft"},
        {Name: "issued_on", Type: schema.Date},
        {Name: "paid_on",   Type: schema.Date},
        {Name: "voided_at", Type: schema.Timestamp},
    },
    States: &framework.StatesConfig{
        Field:   "status",
        Initial: []string{"draft"},
        Transitions: []framework.Transition{
            {Key: "issue", Label: "Issue",
                From: []string{"draft"}, To: "issued", Stamp: "issued_on"},
            {Key: "pay", Label: "Mark paid",
                From: []string{"issued", "overdue"}, To: "paid", Stamp: "paid_on"},
            {Key: "void", Label: "Void",
                From: []string{"draft", "issued"}, To: "void",
                Stamp: "voided_at", Variant: "danger", Permission: "invoices:void"},
            {Key: "mark_overdue", From: []string{"issued"}, To: "overdue", System: true},
        },
    },
})
```

That one declaration publishes, on a writable entity:

- the route `POST /invoices/{id}/transitions/issue` (and `/pay`, `/void`),
  no body. An `APIPrefix` moves it like every auto-CRUD route;
- the MCP tools `invoices_issue`, `invoices_pay` and `invoices_void` beside
  the CRUD tools, when the entity sets `mcp: true`;
- nothing for `mark_overdue`. A System move has no route, no button and no
  tool, so only Go code runs it:

```go
// A nightly job marking invoices past due, not a route.
moved, err := invoices.RunTransition(ctx, id, "mark_overdue")
```

`RunTransition` also works on an `Advisory` entity, where the moves stay as
calls and buttons.

## The same in a declaration JSON

The `states` key on an `EntityDeclaration`, with snake_case keys as the
json tags spell them. Unknown keys inside the block are refused, as they
are in the display block.

```json
{
  "name": "invoices",
  "fields": [
    {"name": "number", "type": "string", "required": true},
    {"name": "status", "type": "enum",
     "values": ["draft", "issued", "overdue", "paid", "void"],
     "default": "draft"},
    {"name": "issued_on", "type": "date"},
    {"name": "paid_on", "type": "date"},
    {"name": "voided_at", "type": "timestamp"}
  ],
  "states": {
    "field": "status",
    "initial": ["draft"],
    "transitions": [
      {"key": "issue", "label": "Issue", "from": ["draft"], "to": "issued", "stamp": "issued_on"},
      {"key": "pay", "label": "Mark paid", "from": ["issued", "overdue"], "to": "paid", "stamp": "paid_on"},
      {"key": "void", "label": "Void", "from": ["draft", "issued"], "to": "void",
       "stamp": "voided_at", "variant": "danger", "permission": "invoices:void"},
      {"key": "mark_overdue", "from": ["issued"], "to": "overdue", "system": true}
    ]
  }
}
```

## Every setting

`StatesConfig`:

| Setting | What it does |
| --- | --- |
| `Field` | The Enum field holding the state. |
| `Initial` | The values a create may set. Empty means the field's `Default` alone; a field with neither is refused at boot. |
| `Transitions` | The moves, in the order screens draw them. |
| `Advisory` | Releases the state field and the stamps to direct writes; the moves stay as calls and buttons. |

Each `Transition`:

| Setting | What it does |
| --- | --- |
| `Key` | Names the move in its route (`POST <api>/<entity>/<id>/transitions/<key>`), its MCP tool and its generated client calls. Lowercase segments joined by single underscores, each led by a letter (`^[a-z][a-z0-9]*(_[a-z][a-z0-9]*)*$`), so no two keys turn into one generated name (`mark__paid` and `mark_paid` would both be `MarkPaid`). Unique on the entity, and not a name the entity's own surfaces use: `list`, `get`, `create`, `update`, `delete`, `patch`, `watch`, `batch_create`, `batch_update`, `batch_delete`, `events`, `remove`, `transition`, and the JS resource's own `client`, `table` and `constructor`. The boot error names the surface a reserved key collides with. |
| `Label` | The button text. Empty draws the key. |
| `From` | The values the move starts from; never empty. |
| `To` | The value the move writes. |
| `Stamp` | A Date or Timestamp field the move sets to the server's current UTC date or time. The client never supplies it. |
| `Variant` | The button variant screens draw the move with (`ui.ParseButtonVariant` spellings, e.g. `danger`). Empty is the screen's default. |
| `Permission` | Required on top of the entity's update access, and held by name: a Wildcard grant does not satisfy it. Empty means update access alone. |
| `System` | No route, button or MCP tool. Only Go code calls `RunTransition` for this move. |

Registration (`App.Entity` → `Entity.Validate`) checks every name the
config holds, so a typo fails the app at boot naming the offender:

- `Field` must be a declared Enum, and the primary key, the owner field and
  the tenant column are refused as the state field or a stamp.
- Every `Initial`, `From` and `To` value must be one of the field's
  `Values`; a `Default` must be one of `Initial`.
- A `Stamp` must be a declared Date or Timestamp field, not the state
  field, with no `Default` (a create would set it) and no `auto_generate`
  (the move could not).

## The write rules

Unless `Advisory` is set, the state field and every stamp are guarded
columns: a write changes them only through a move.

- **Create.** The state field may be absent (the `Default`) or hold an
  `Initial` value; a stamp may be absent or null. Anything else is a 422
  `StateError`: the state field names the values a new record starts at,
  a stamp says it is set by a move, not on create.
- **Update.** A guarded column sent with its stored value passes: writing
  it back is not a change. The handler drops it from the body, so a
  full-row `PUT` that round-trips the record still works, and a stale
  round-trip can never clobber a move that ran meanwhile. Any other value
  is a 422 `StateError` naming the moves open from the stored value.

```json
HTTP/1.1 422 Unprocessable Entity
{"error": "status changes only through a move; open from \"draft\": issue, void",
 "success": false,
 "fields": {"status": ["status changes only through a move; open from \"draft\": issue, void"]},
 "moves": ["issue", "void"]}
```

The check runs after the `BeforeCreate`/`BeforeUpdate` hooks, so a hook
cannot set the field either. It covers every write path: the REST create
and update, `_batch` items, cascade writes, `UpsertOne` (an update of the
row the body's key names when that row is visible, a create otherwise; its
`DO UPDATE SET` names a guarded column only when the caller sent it under
a `WithStateOverride` that has its reason and audit log, so an omitted
state field keeps the stored state and the insert arm's `Default` never
lands on conflict), and the in-process `CreateOne` and `UpdateOne`.

A refusal names the stored state and the open moves only to a caller whose
`ReadScope` admits the record. One the scope hides gets the same 422 or 409
with neither, so a caller who may write a record but not read it learns
nothing of its state from the refusal.

`TypedQuery.UpdateAll` refuses a body that names any guarded column
(`crud.ErrBulkStateWrite`), override or not: one value written onto many
rows is a move per record, so run the move per record instead.

## Running a move

`CrudHandler.RunTransition(ctx, id, key)` is the one way an enforced state
field changes. In order:

1. **Who may move.** A System move skips this; no route reaches it. Any
   other move asks the entity's update permission and, when the move sets
   one, its `Permission`. Both are asked about this record, so a `Decider`
   can answer per row. The move's `Permission` is held by name
   (`access.CanResourceExact`): a role granted the Wildcard passes the
   update permission but not the move's own. `WithServerWrites` does not
   skip them.
2. **A read of the row** under tenant, owner-write and soft-delete scope.
   A row the caller cannot see answers 404, so another owner's id reveals
   nothing. A visible row whose value is not in `From` answers a
   `TransitionConflictError` (409) naming the open moves.
3. **`BeforeUpdate` hooks**, with the move's writes as the body and the
   move's key on the context (`crud.TransitionFromContext`). A hook can
   veto; changes it makes to the body are not written.
4. **One conditional `UPDATE`** under the same scope, pinned to `From`:
   `SET field = To, stamp = <the server's UTC date or time>`, and
   `updated_at` when the entity has one. Zero rows means another write
   moved the record first, and the answer is a `TransitionConflictError`.
5. **`AfterUpdate` hooks, the audit row and the `entity.updated` event**,
   in the same transaction.

A hook cannot move the record whose update or move is running it:
`RunTransition` on that record from inside the write answers
`crud.ErrReentrantMove`, and a hook that returns it fails the write with
409, from a `Before` hook as from an `After` one. The nested move would either
break the outer statement's pin on `From`, rolling both back, or leave the
outer write answering a state the record no longer holds. Moving another
record from a hook runs as normal; to chain a move onto this record, run it
after the write commits.

On SQLite, a deferred transaction reads under a snapshot, so two
connections racing to move one record could both read the old state, and
the loser's write would fail with `SQLITE_BUSY` after its hooks ran. A move
takes the write lock before it reads (a statement that writes no row), so
the loser waits for the winner to commit, reads the winner's state, and
answers the usual 409 before any of its hooks run. On Postgres the
statement is a no-op; the conditional `UPDATE` already serializes there.

The REST route is `POST <api>/<entity>/<id>/transitions/<key>`, mounted on
a writable entity whose `States` hold at least one non-system move. It
takes no body and requires the JSON content type, which a cross-site form
cannot satisfy. The response is the moved record in Update's envelope.

| Status | Means |
| --- | --- |
| 200 | The move ran; the body is the moved record. |
| 403 | Missing the update permission or the move's `Permission`. |
| 404 | Unknown key, a System move, or a row the caller cannot see under its scope. |
| 409 | The current value is not in `From`, including the race where another write moved the record first, or a hook tried to move the record whose write is running it. |
| 415 | Not `Content-Type: application/json`. |

When the entity sets `mcp: true`, each non-system move is one MCP tool,
`<entity>_<key>`, taking the record's `id` and posting to the move's
route. The update permission gates it like update. System moves get no
tool.

## Who may move

A non-system move needs the entity's update permission, plus the move's
own `Permission` when it sets one. Both checks are asked about the record
being moved: `access.CanResource` carries `Ref{Type: <entity>, ID: <id>}`,
so a resource-aware `Decider` can allow `pay` on one invoice and refuse it
on another. `WithServerWrites` does not skip them: it lifts owner scoping
for trusted Go, never the permission gates.

Owner, tenant and soft-delete scope apply to both the read and the
conditional UPDATE, so a move on an invisible row answers 404 and a
concurrent delete cannot be raced past.

## Stamps

A move's `Stamp` names a Date or Timestamp field the move itself fills. A
Date stamp gets the server's current UTC date; a Timestamp stamp gets the
current time, bound the way `updated_at` is. The client never supplies the
value: a create may not set it, an update writing it back is dropped like
the stored state, and the boot check refuses a stamp with a `Default` or
`auto_generate`. Reading the stamp is ordinary: it is a normal column in
every response, filter and sort.

## Escape hatches

**`Advisory: true`** releases the state field and the stamps to direct
writes, and keeps the moves as calls and buttons. For an entity whose
status is a label, not a rule.

**`crud.WithStateOverride(ctx, reason)`** lets trusted Go code (seeds,
backfills, repair jobs) write the guarded columns directly, outside a move.
It is set only from Go, never from a request value, and two things are
required or the write is refused with an error:

- a non-empty reason (`crud.ErrStateOverrideNoReason`);
- an audit log on the entity, from `App.WithAuditLog`
  (`crud.ErrStateOverrideUnaudited`), because the override's only trail is
  the audit row.

An update under it, and an `UpsertOne` under it that lands on an existing
row, is audited with the operation `state_override` and the reason in the
audit row's `reason` column (see [audit log](audit-log.md)). `WithServerWrites` does not release the state
field; `WithStateOverride` does, and only it does.
`TypedQuery.UpdateAll` refuses guarded columns even under the override: a
bulk UPDATE runs no hooks, so it would leave no audit row.

### Outside the check

`App.ImportData` restores an `ExportData` archive verbatim: every column
of every row, states and stamps included, with no hooks and no audit rows,
inside one transaction. It is a restore of a database the rule already
governed, not a write path, so it neither checks states nor needs
`WithStateOverride`. Do not use it to load new records; seed those with
`CreateOne` and, for a non-initial state, `WithStateOverride`.

`App.EraseUserData` deletes a user's rows whatever their state; it runs no
moves and no write hooks.

## Generated clients

Every generated client gets one call per non-system move; System moves
appear in none of them. With enforced states (not `Advisory`) the write
shapes also leave out what only a move may change.

| Client | Move call | Write shapes |
|---|---|---|
| OpenAPI | `POST /<entity>/{id}/transitions/<key>`, operation id `<key>_<Schema>` | The create body narrows the state field's enum to the initial values and drops every stamp; the update body drops the state field and the stamps. Responses keep the full enum. |
| MCP | the tool `<entity>_<key>` | The create and update tool schemas follow the OpenAPI request bodies. |
| Go client (`gofastr gen client`) | `client.<Key><Entity>(ctx, id)`, e.g. `PayInvoices` | `<Entity>Input` drops the stamps and keeps the state field (a create may start at an initial value; a PUT writing the stored value back passes). `<Entity>Patch` and `<Entity>BatchPatch` drop both. |
| JS SDK | `client.<table>.<key>(id)`, or `client.<table>.transition(id, key)` | `<Entity>Input` drops the stamps; `<Entity>Patch` is `Partial<Omit<<Entity>Input, "<state>">>`. |
| CLI | `<app> <entity> <key> <id>` | Create and update drop the stamp flags. The state flag stays on both, so a create can start at an initial value; an update that changes the state answers 422. |

A move sends an empty JSON body: the route takes no payload but requires
the JSON content type, and the OpenAPI operation declares that body as a
required empty object. The generators read hand-written declarations that
never passed registration, so they run the same boot check
(`entity.ValidateStates`) and refuse what the app would. Declaration text
that lands in a generated doc comment (an enum value, a table name) has its
control bytes flattened, and `*/` broken in the JS and TS block comments, so
a value can never end the comment. The CLI prints a move's summary in its
help, so state values reach it with control and bidi characters dropped.

A move's Go names join the entity to the key, so two entities can meet in
one name: `orders` with the move `mark_paid` and `paid_orders` with `mark`
both make `client.MarkPaidOrders`, and `orders`/`mark_paid` against
`orders_mark`/`paid` both make the CLI's `runOrdersMarkPaid`. The project,
SDK and CLI generators refuse such a pair and name the identifier; rename
an entity or a key.

## The audit trail

With `App.WithAuditLog` on the entity, every move writes an audit row with
the operation `transition:<key>` inside the move's transaction, and a
state override writes `state_override` with its reason. See
[audit log](audit-log.md) for the row shape and the `reason` column.

## Common mistakes

- **Writing the status in a `BeforeCreate` or `BeforeUpdate` hook.** The
  state check runs after the hooks, so the hook's write is refused the
  same way a client's is. Declare the value as `Initial` or run a move.
- **Expecting a full-row `PUT` to fail.** A guarded column sent with its
  stored value is not a change; the handler drops it and the update
  proceeds. That is the design: only a different value is refused.
- **Bulk-moving with `TypedQuery.UpdateAll`.** Refused with
  `crud.ErrBulkStateWrite`, override or not. One value onto many rows is a
  move per record; call `RunTransition` in a loop.
- **Calling `WithStateOverride` before `App.WithAuditLog`.** The override
  is refused on an entity no audit log records. Register the entities
  first, then `WithAuditLog`, then override.
- **Naming a move `update`, `patch` or `remove`.** Those keys collide
  with a name the entity's own tools, client methods or SDK members already
  use; registration refuses them and the other reserved keys in the `Key`
  row above.
- **A stamp on a column the framework writes.** A stamp or state field on
  the primary key, the owner or tenant column, `deleted_at` under soft
  delete, or an auto-generated column (`created_at`, `updated_at`) is
  refused at boot: those columns already have a writer.
- **Looking for the route of a System move.** It answers 404, like an
  unknown key. System moves exist for Go jobs; write the job that calls
  `RunTransition`.
