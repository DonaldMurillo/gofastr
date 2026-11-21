# States and transitions

Declare a finite state machine on an entity when status changes should happen through named, guarded transitions instead of arbitrary `PATCH` writes.

## Declaring states

Attach `States` to `framework.EntityConfig`:

```go
app.Entity("invoices", framework.EntityConfig{
	Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "status", Type: schema.Enum, Values: []string{"draft", "sent", "paid", "void"}, Default: "draft"},
		{Name: "sent_at", Type: schema.Timestamp},
		{Name: "paid_at", Type: schema.Timestamp},
	},
	States: &entity.StatesConfig{
		Field:   "status",
		Initial: []string{"draft"},
		Transitions: []entity.Transition{
			{Key: "send", Label: "Send", From: []string{"draft"}, To: "sent", Stamp: "sent_at"},
			{Key: "pay", Label: "Mark paid", From: []string{"sent"}, To: "paid", Stamp: "paid_at", Permission: "invoices:pay"},
			{Key: "void", Label: "Void", From: []string{"draft", "sent"}, To: "void", Variant: "danger", Permission: "invoices:void"},
		},
	},
})
```

What GoFastr generates and enforces automatically:
- **Transition route:** `POST /<table>/{id}/transitions/{key}` executes the named move inside a transaction, checks that the current value is in `From`, verifies `Permission` (if set), stamps `Stamp` with the current UTC time (if set), and runs update hooks + emits `entity.updated`.
- **Direct edits blocked:** once `States` owns a column (unless `Advisory: true`), `PUT`/`PATCH` cannot overwrite that column directly, and codegen removes the state field from `<Entity>Patch` ([entity declarations](entity-declarations.md#the-typed-repository)).
- **UI integration:** `framework/entityui` and `battery/admin` render non-danger transitions applicable to the current state as header buttons, move `Variant: "danger"` transitions behind a confirm flow, and show transition timestamps in the Details sidebar.
- **MCP tools:** when `Exposure.MCP` is enabled, non-system transitions are exposed to agents alongside CRUD tools.

## System transitions and overrides

- **System-only transitions (`System: true`):** callable from Go code (such as webhook handlers or background jobs) without exposing an HTTP transition button or endpoint. If every transition on an entity is `System: true`, no `/transitions/{key}` route is mounted.
- **Admin overrides:** when configured with an override permission, authorized operators can force a state change with a required reason, recorded in the audit trail and shown in the record's Activity timeline.

## Hooks and error codes

- Moving from a state not listed in `From` returns `409 Conflict`.
- Missing `Permission` returns `403 Forbidden`.
- Missing or out-of-scope row (`OwnerField` / `MultiTenant` / `SoftDelete`) returns `404 Not Found`.
- Transitions run the entity's normal `BeforeUpdate` and `AfterUpdate` hooks—call `crud.TransitionFromContext(ctx)` inside those hooks to inspect which transition is running, run domain checks (like verifying line items exist before sending), or enqueue post-commit side effects.

## Common mistakes

- **Trying to update a state-managed field via `PATCH /invoices/{id}`:** direct writes to the state column are rejected. Call `POST /invoices/{id}/transitions/{key}` (or the typed transition helper in Go).
- **Forgetting to list the `Stamp` column in `Fields`:** every `Stamp` target must be a declared `schema.Timestamp` or `schema.Date` field on the entity.
- **Putting side effects (sending email, charging a card) inside `BeforeUpdate`:** `BeforeUpdate` runs inside the DB transaction. Enqueue jobs or call external APIs in `AfterUpdate` (or write to an outbox in the transaction).
- **Omitting `Variant: "danger"` on irreversible moves like `void` or `cancel`:** marking destructive moves `Variant: "danger"` keeps them styled as destructive actions with confirmation in entityui.
