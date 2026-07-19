# Optimistic UI — mutations, rollback, and reconciliation

Optimistic UI is the pattern where a click paints the *expected* result
immediately and the server response either confirms it or undoes it. Done
right it makes a server-authoritative app feel instant; done wrong it lies
to the user about state that was never committed. This doc is the
framework's contract for doing it right: one lifecycle, one state set, one
reconciliation model, composed from existing primitives — `OptimisticAction`,
`ToggleAction`, versioned `sortablelist`, `ConfirmAction`, and the generic
RPC wrappers in `core-ui/interactive`.

If you are looking for the wider state boundary (signals vs RPC vs SSE),
start at [UI capability map](ui-capability-map.md). This page assumes that
boundary and zooms in on the mutation half.

---

## The mutation lifecycle

Every optimistic mutation in the framework follows one lifecycle. The
primitives below differ in *what* they paint optimistically (a label, a
row, a column) but they all walk the same five-state machine and reconcile
against the same authoritative source: the HTTP response plus the database,
never a pushed event.

```text
capture previous projection
  → apply optimistic projection
  → send mutation (with id/version if applicable)
  → success (2xx): reconcile authoritative result
  → conflict (409 with version token): refresh authoritative region
  → failure (4xx/5xx/network): rollback and announce
```

- **Capture previous projection.** Before painting, the runtime keeps
  enough state to undo the optimistic change. For `OptimisticAction` that
  is the button's `idle` label span; for versioned `sortablelist` it is a
  snapshot of the dragged column's DOM; for an inline edit it is the
  input's prior value. Without a captured previous state there is nothing
  to roll back to.
- **Apply optimistic projection.** The visible state flips *before* the
  fetch resolves. `OptimisticAction` swaps the label on the next animation
  frame; `ToggleAction` flips idle↔committed; the sortable runtime moves
  the dragged `<li>` into its new position immediately.
- **Send the mutation.** One fetch to one endpoint, carrying whatever the
  handler needs to commit authoritatively: the new value, a stable key, a
  mutation/idempotency key, and (when the resource is versioned) a
  concurrency token.
- **Reconcile.** The HTTP response — not a signal, not an SSE event — is
  the mutation's authority. A 2xx commits the optimistic projection (or
  replaces it with the authoritative result); a 409 with a version token
  triggers server-rendered reconciliation; anything else rolls back.

### The state set

| State | Meaning | Where it lives |
|---|---|---|
| `idle` | Resting; no mutation in flight. SSR ships here. | `data-state="idle"` |
| `pending` | Optimistic projection is painted; the RPC is in flight. Control is `aria-busy="true"` and disabled. | `data-state="pending"` |
| `committed` | The RPC returned 2xx. The projection is now authoritative. | `data-state="committed"` |
| `conflicted` | A versioned 409. The runtime fetches fresh HTML from the conflict endpoint and replaces the affected region. (Sortable-specific today.) | `data-state` on the list, plus an `aria-live` announcement |
| `failed` | The RPC returned non-2xx (or the network threw). The runtime rolls the projection back to `idle` and announces. `OptimisticAction` paints a brief `error` shake first. | `data-state="error"` (OptimisticAction) or direct revert to `idle` (ToggleAction) |

`OptimisticAction` and `ToggleAction` use `data-state` on the trigger
button; `sortablelist` uses it on the `<ol>` wrapper. The runtime reads
the same attribute name across primitives so a single CSS selector
(`[data-state="pending"]`) can style every pending surface consistently.

### Double-click, duplicate submit, and cancellation

The lifecycle is **button-scoped and re-entry-safe**. While a button is in
`pending`:

- `OptimisticAction` ignores further clicks (`if (state === 'committed' ||
  state === 'pending') return;` in `optimisticaction.js`).
- `ToggleAction` does the same (`if (state === 'pending') return;` in
  `toggleaction.js`).
- `sortablelist` ignores new grabs until the active commit settles.

For mutations that must be **globally** idempotent across buttons, tabs,
or sessions (a "pay" button that must never charge twice), do not rely on
the button-scoped guard. Send an `Idempotency-Key` header so the server
deduplicates; see [Idempotency](idempotency.md). The runtime's job is to
keep one button coherent; the server's job is to keep one mutation
coherent.

**Cancellation** has two senses:

1. *User cancels the in-flight click.* There is no abort — the fetch
   completes and the response reconciles. The user can click a sibling
   (e.g. another toggle in the same `Group`) which optimistically revokes
   the first; the first RPC still completes, and the server stays the
   source of truth via a later navigation refresh.
2. *User dismisses a confirm dialog.* `ConfirmAction` and
   `Action.WithConfirm` gate the request *before* it fires. Cancelling the
   dialog never sends the RPC — there is nothing to roll back.

### Out-of-order responses

Each primitive keeps at most one in-flight request per trigger element.
Because the trigger is disabled during `pending`, a second click cannot
start a second fetch and arrive in a different order. The runtime does
**not** coordinate across triggers — if two different buttons POST to the
same handler, their responses reconcile independently against whatever
server state each finds. For ordered mutations across controls, use
version tokens (below) so the second request 409s against the first's
commit.

### Temporary client IDs for optimistic inserts

An *optimistic create* paints a row before the server has assigned it an
ID. The pattern:

1. The client mints a **temporary client ID** (`temp:<uuid>` or
   `temp:<timestamp>-<n>`), stamps it on the optimistic row, and fires
   the create RPC with `temp_id` in the body.
2. The handler commits the row, assigns the authoritative ID, and returns
   the row's authoritative representation (HTML fragment or JSON).
3. On 2xx, the runtime replaces the temp-ID row with the authoritative
   row. On failure, the runtime removes the temp-ID row and announces.

The `sortablelist` runtime already implements the version-aware half of
this pattern (its conflict endpoint returns fresh `<li>` HTML that
reconciles a column, including to zero items). A general-purpose
"optimistic insert" runtime attribute is not yet shipped; today you
compose it from an island RPC that returns the authoritative row HTML and
a signal swap (`data-fui-rpc-signal` with `data-fui-signal-mode="html"`).
The temp-ID discipline is a contract between your handler and your island
code; see [Recipe 3](#recipe-3-optimistic-create-with-a-temporary-row).

### Idempotency and mutation keys

A **mutation key** is a client-chosen value that says "this is logical
mutation N." Send it as the `Idempotency-Key` header on the fetch. The
server records the key with the commit; a replay returns the original
response instead of running the mutation again. This is what makes a
flaky network safe for "ship order," "send invoice," "charge card" — the
user can refresh, the browser can retry, the runtime can re-fire, and the
server deduplicates. See [Idempotency](idempotency.md) for the
server-side contract.

For purely visual optimistic actions (follow, like, save-tag) a mutation
key is optional — the worst case is a double-toggle that the next
navigation refresh corrects. For anything that creates, charges, or
notifies, send one.

### Version and ETag conflict tokens

When a resource can be edited by two actors at once, send a **version
token** (an `ETag`, a `version=N` counter, an `updated_at` cursor) along
with the mutation. The handler compares the request's token to the row's
current token; on mismatch it returns **409 Conflict** with a
problem-detail body. The runtime then reconciles instead of rolling back.

`sortablelist` is the canonical implementation. Each commit POST carries
`version=<token>` (from `Config.Version`); a 409 response triggers
`ConflictRPC` (from `Config.ConflictRPC`), which returns fresh
server-rendered `<li>` HTML that replaces the destination column. The
409 body is read under hard safety bounds — `Content-Type: application/json`,
at most ~4 KB, must parse as `{"error":{"code","message"}}` — and the
`message`, when valid, is surfaced through the polite `aria-live` region.
See [Recipe 5](#recipe-5-sortablekanban-move-with-version-aware-409) and
[Interactive patterns → Sortable List](interactive-patterns.md#sortable-list-single--kanban).

### Rollback vs authoritative refresh

Two distinct reconciliation paths, do not confuse them:

- **Rollback.** The runtime restores the captured previous projection.
  Used when there is no server-side truth to reconcile against — a
  network failure, a 4xx validation error, a non-versioned 5xx. The UI
  returns to exactly what it showed before the click.
- **Authoritative refresh.** The runtime fetches the server's current
  state and replaces the optimistic region with it. Used when the server
  *knows* the projection is stale — a versioned 409, or any path where
  the handler can return fresh HTML. The UI lands on server truth, which
  may differ from both the optimistic projection *and* the prior state.

Rule of thumb: rollback is local and exact; authoritative refresh is
round-tripped and possibly surprising. Prefer authoritative refresh when
the server has a coherent answer; prefer rollback when the server is
unreachable or has no opinion.

### Accessible announcements

Pending, success, and failure must reach assistive tech, not just sighted
users. The primitives handle this differently:

- **`aria-busy`** is set on the trigger during `pending` by both
  `OptimisticAction` and `ToggleAction`, and removed on settlement.
- **`aria-pressed`** mirrors committed state on `ToggleAction` (it is a
  toggle by WAI-ARIA's contract). `OptimisticAction` does not use
  `aria-pressed` — it is one-shot, not a pressed/unpressed control.
- **`prefers-reduced-motion`** is respected: the `OptimisticAction`
  shake animation is disabled under `@media (prefers-reduced-motion:
  reduce)`; the visible label flip is unchanged because it carries
  information, not decoration.
- **`aria-live`** regions announce sortable grab/move/rollback/conflict
  events (the polite region is wired in `sortablelist.js`).

The two button primitives (`OptimisticAction`, `ToggleAction`) do **not**
today emit a spoken "Saved" / "Rolled back" announcement — they rely on
the visible label flip and `aria-busy` toggling. See [Consistency
notes](#consistency-notes) for the gap and the workaround.

### SSE is not the durable ledger

A common mistake is to wait for an SSE event to confirm a mutation. The
runtime never does this and your handler should not either. SSE is
push-only — best-effort delivery for *other* sessions to learn that state
changed. The initiating request's HTTP response is the mutation's
authority; the database row is the durable truth. SSE can drop, lag, or
arrive in any order relative to the response; treating it as the
confirmation channel inverts the contract.

Concretely:

- After a successful follow POST, the `ToggleAction` is committed because
  the response was 2xx — not because a `user.followed` SSE event arrived.
- After a sortable move, the column is authoritative because the move
  POST returned 2xx (or the 409 conflict path reconciled) — not because
  a `board.moved` event was broadcast.
- Other tabs and other users learn about the change through SSE; the
  initiating tab learned through the response.

[UI capability map → Delivery guarantees](ui-capability-map.md#delivery-guarantees)
and [Events and SSE](events.md) cover the wider lane semantics.

---

## The seven recipes

Each recipe names the primitive, the handler contract, the response shape
the runtime expects, and where to see it running in `examples/site`
(`go run ./examples/site` then open the listed route). Demos that are
already complete are linked, not rebuilt.

### Recipe 1 — binary toggle / follow

**Use for:** binary server-backed state the user flips in place — follow /
following, watch / unwatch, subscribe / unsubscribe, default-plan picker.

**Primitive:** `ui.ToggleAction` (`framework/ui/toggleaction.go`). Runtime:
`toggleaction.js`. SSR ships the initial state via `Committed`; the
runtime mirrors it onto `aria-pressed` and flips idle↔committed on click.
A second click reverts when `AllowUntoggle` (or `UntoggleEndpoint`) is
set; without it the button is sticky once committed, matching
`OptimisticAction`.

**Compose:**

```go
ui.ToggleAction(ui.ToggleActionConfig{
    Endpoint:         "/api/follow/42",
    UntoggleEndpoint: "/api/unfollow/42",
    IdleLabel:        "Follow",
    CommittedLabel:   "Following ✓",
    Committed:        alreadyFollowing, // SSR initial state
    AllowUntoggle:    true,
})
```

**Handler contract:** `POST /api/follow/42` → 2xx on success, non-2xx on
failure (runtime reverts to idle). The body is empty — the resource id is
in the URL. Authorize in the handler; never trust the client's optimistic
state.

**Authorization:** the trigger renders for any user who can see the page;
the handler must re-check the session and the follow relationship. The
optimistic flip is presentation only.

**Runnable proof:** [`/components/toggleaction`](../../../examples/site/components.go).
E2E: `TestE2E_ToggleAction_CommitUntoggle`,
`TestE2E_ToggleAction_GroupMutex` in
`examples/site/e2e_new_components_interactions_test.go`.

### Recipe 2 — inline edit with rollback

**Use for:** editing one field of a record in place — rename, retitle,
edit a tag — where the field has a known previous value and the server
validates the new one.

**Primitive:** `ui.OptimisticAction` (`framework/ui/optimisticaction.go`)
as the commit trigger, paired with a text field whose prior value is the
rollback target. Runtime: `optimisticaction.js`. The button flips to its
success label optimistically, fires its endpoint, and on non-2xx shakes
and reverts. The `error` shake animation respects `prefers-reduced-motion`.

> **Honest gap.** `OptimisticAction`'s fetch is fire-and-forget — it does
> not serialize form fields. For a true inline edit where the *new value*
> must reach the handler, wrap the input in a `<form>` and use
> `interactive.OnSubmit`; the runtime submits the form as JSON keyed by
> each control's `name`. The two patterns compose: the form transmits the
> value, an adjacent `OptimisticAction` provides the optimistic visual on
> a value-independent commit (a "freeze" toggle, a "revert to draft"
> action). For a one-form-one-button edit, prefer `OnSubmit` plus
> `interactive.AfterText("Saved ✓")` — see [Forms](form-module.md).

**Compose (the value-independent optimistic half):**

```go
ui.OptimisticAction(ui.OptimisticActionConfig{
    Endpoint:     "/api/rename/validate",
    IdleLabel:    "Save",
    SuccessLabel: "Saved ✓",
})
```

**Handler contract:** `POST /api/rename/validate` → 2xx if the (server-
known) pending value is acceptable, 4xx otherwise. The runtime shakes +
reverts on non-2xx. To transmit the actual edited value, use a sibling
`interactive.OnSubmit` form posting to `/api/rename` whose response
swaps the display region (`data-fui-rpc-signal` with `mode=html`).

**Authorization:** the input and the button render for any viewer; the
handler enforces "can edit this field." A 4xx response is the auth path
the runtime understands — return 4xx (not a redirect) when the user
lacks permission, and the optimistic flip rolls back honestly.

**Runnable proof:** [`/components/optimisticinlineedit`](../../../examples/site/components.go).
The demo pairs a text input with two `OptimisticAction` buttons — one
hits an endpoint that returns 2xx (the optimistic commit sticks), one
hits an endpoint that returns 4xx (the button shakes and reverts). E2E:
`TestE2E_Optimistic_InlineEdit_SuccessAndRollback`.

### Recipe 3 — optimistic create with a temporary row

**Use for:** adding a row to a list — a comment, a task, a tag — where the
user expects the new row to appear instantly and the server assigns the
durable ID.

**Primitive:** `interactive.OnClick` + `data-fui-rpc-signal` (mode
`html`) for the round-trip; the temp-ID discipline is a contract between
your handler and the row template you render. There is no general-purpose
"optimistic insert" runtime attribute today; you compose it.

**Compose (the closest existing-primitive demo):**

```go
// Render: a list region bound to a signal, and an Add button whose
// RPC swaps that region with authoritative HTML on 2xx.
html.Div(html.DivConfig{
    ExtraAttrs: html.Attrs{
        "data-fui-signal":      "opt-create-list",
        "data-fui-signal-mode": "html",
    },
}, renderOptItemList())

interactive.OnClick(
    ui.Button(ui.ButtonConfig{Label: "Add item"}),
    interactive.Post("/__site/optimistic/create").
        OnSuccess(interactive.SetSignal("opt-create-list")),
)
```

**Handler contract:** `POST /__site/optimistic/create` appends a row to
server-side state and responds with the **full, authoritative list HTML**
(not just the new row). The runtime replaces the list region's
`innerHTML` with the response. Because the response is the entire list,
the new row appears with its real server-assigned id; there is no
temp-row-to-reconcile on the wire. On a non-2xx response the runtime
broadcasts the auto-built error object `{ok:false,status,text}` into
the signal; an `html`-mode region **ignores non-string values** (the
optimistic-UI invariant: a failed create leaves the list exactly where
it was), while a sibling `text`-mode status node renders a
human-readable "Error: <status> — <text>" line.

**The temp-row pattern (what "optimistic" adds):** a *true* optimistic
create paints the row before the fetch resolves. That requires either (a)
an island with a small amount of registered JS that mints a `temp:<id>`
row, fires the RPC, and swaps the temp row for the authoritative one on
2xx, or (b) a future runtime attribute in the `data-fui-optimistic-*`
family. The pattern's invariants, whichever path you take:

1. Mint a temp id only the client will see; never let the server persist
   it.
2. Send `temp_id` in the request body so the handler can correlate.
3. The 2xx response carries the authoritative row (with the real id); a
   failure removes the temp row.
4. A temp row must not be editable, draggable, or deletable until the
   authoritative replacement lands — otherwise a user can mutate
   state that does not exist yet.

**Authorization:** the Add button can render for everyone; the handler
enforces "can create." A 4xx on create should leave the list unchanged
(response body empty or an error fragment the runtime writes into the
signal region).

**Runnable proof:** [`/components/optimisticcreate`](../../../examples/site/components.go).
The demo exercises the round-trip shape: click Add → the list region
swaps with fresh server HTML showing the new row. E2E:
`TestE2E_Optimistic_Create_AppendsAndPersists`.

### Recipe 4 — optimistic delete with restore

**Use for:** removing a row — delete comment, remove member, archive task
— where the user expects the row to disappear and the server confirms.
Pair with `ConfirmAction` when the action is destructive enough to need a
second click.

**Primitive:** `ui.ConfirmAction` for both the gate and the round-trip.
Pass `SuccessSignal: "<list-signal>"` and the Confirm button emits
`data-fui-rpc-signal="<list-signal>"`; on 2xx the runtime broadcasts
the response body into every `data-fui-signal="<list-signal>"
data-fui-signal-mode="html"` region, swapping in the authoritative
shorter list. The "restore" falls out of the model: the list is not
swapped until 2xx, so a failure leaves the row exactly where it was.

**Compose:**

```go
// One mounted modal per <trigger, modal> pair (see ui.ConfirmAction).
trigger, modal := ui.ConfirmAction(ui.ConfirmActionConfig{
    Name:          "delete-item-" + item.ID, // unique per row
    TriggerLabel:  "Delete",
    Title:         "Delete this item?",
    Body:          "It will be removed permanently.",
    RPCPath:       "/__site/optimistic/delete?id=" + item.ID,
    SuccessSignal: "item-list", // ← swaps the list region on 2xx
})
// widget.Mount(app.Router(), modal.Build()) — once, at startup

// Render the trigger inline next to its row; the modal mounts globally.
trigger
```

Bind a list region to the same signal name so the runtime has somewhere
to apply the response:

```go
listRegion := html.Div(html.DivConfig{
    ExtraAttrs: html.Attrs{
        "data-fui-signal":      "item-list",
        "data-fui-signal-mode": "html",
    },
}, renderItems())
```

**Handler contract:** `POST /__site/optimistic/delete?id=<id>` removes
the row and responds with the **authoritative list HTML** (the list
without the deleted row). On 2xx the runtime swaps the list region; on
non-2xx the runtime broadcasts the auto-built error object
`{ok:false,status,text}` into the signal, but an `html`-mode region
**ignores non-string values** — the list stays byte-identical and the
row stays visible. Pair `SuccessSignal` with a `text`-mode sibling
region if you want a human-readable failure message; the html region is
reserved for trusted-HTML payloads and is never corrupted by an error
object.

**Restore semantics.** Two flavors:

1. *Implicit restore on failure.* The row never visually disappeared —
   the swap happens on 2xx — so a failed delete leaves the row in place.
   This is what the existing primitives give you for free.
2. *Explicit undo window.* A *truly* optimistic delete removes the row
   instantly and shows an "Undo" affordance for N seconds; if the user
   clicks Undo (or the RPC fails), the row reappears. This pattern needs
   the same temp-row machinery as Recipe 3 and is not directly supported
   by a single runtime attribute today — compose it as an island.

**Authorization:** ConfirmAction's trigger renders for any viewer; the
modal is the UX layer, not the auth layer. The handler must re-check
"can delete this row" and return 4xx (not a redirect) when the user
lacks permission — the runtime leaves the row in place, which is the
honest outcome.

**Runnable proof:** [`/components/optimisticdelete`](../../../examples/site/components.go).
The demo shows a small list where each row's Delete button opens the
ConfirmAction modal; confirming hits the delete endpoint and the list
swaps to the authoritative shorter list on 2xx. A "Delete n1 (will
fail)" affordance below the list confirms into a 422 endpoint so you
can see the failure invariant hold: the list does not change. E2E:
`TestE2E_Optimistic_Delete_RemovesOnConfirm` (success) and
`TestE2E_Optimistic_Delete_Fail_LeavesListUnchanged` (422 → list
byte-identical, row still present).

### Recipe 5 — sortable/Kanban move with version-aware 409

**Use for:** reordering within a list, or moving cards between columns on
a board, where two users can move the same item concurrently.

**Primitive:** `core-ui/patterns/sortablelist` with `Config.Version`
(the concurrency token) and `Config.ConflictRPC` (the reconciliation
endpoint). Runtime: `sortablelist.js`.

**Compose:** one `sortablelist.Render` per column, all sharing the same
`Group` (the board id), each with a unique `Container` (the column id).
`Version` is appended to every commit POST as `version=<token>`. A 409
response triggers `ConflictRPC` (a GET), whose response body replaces the
destination column's `innerHTML`.

```go
patternsSortablelist.Render(patternsSortablelist.Config{
    Label:       col.Title,
    Group:       "board-1",
    Container:   col.ID,
    RPCPath:     "/api/board/move",
    Version:     fmt.Sprintf("v%d", board.Version),
    ConflictRPC: "/api/board/conflict?container=" + col.ID,
    Items:       items,
})
```

**Handler contract.**

- `POST /api/board/move` — body has `order`, `moved`, `container`,
  `version`. If `version` does not match the current board version,
  respond **409 Conflict** with
  `Content-Type: application/json` and a body
  `{"error":{"code":"…","message":"…"}}` (the `message` is surfaced
  through the polite `aria-live` region, capped at ~300 chars). On
  match, apply the move, bump the version, respond 2xx.
- `GET /api/board/conflict?container=<col>` — return fresh `<li>` HTML
  for the named column at its current authoritative state. An empty body
  reconciles the column to zero items.

The runtime reads the 409 body under hard safety bounds
(`Content-Type: application/json`, ≤ ~4 KB, must parse, `message` ≤ ~300
chars). Anything malformed falls back to the generic copy. Without
`Version`, a 409 is treated like any other non-2xx (plain rollback) —
back-compat.

**Authorization:** the rendered board reflects what the viewer can see;
the move handler must re-check that the user can move the named card into
the named column. A 403 (treated as non-2xx) rolls the DOM back; a 409
with version mismatch reconciles. Use 409 only for genuine concurrency
conflicts — never for authorization failures.

**Runnable proof:** [`/components/sortablelist`](../../../examples/site/components.go)
— a 3-column kanban backed by the package-level `kanbanBoard` store, with
`/__site/sortable/move` (version-aware 409) and
`/__site/sortable/conflict` (fresh HTML). E2E:
`TestE2E_SortableKanbanDragPersist` in
`examples/site/e2e_sortable_kanban_test.go`. Full handler source:
`examples/site/main.go` (search `sortable/move`).

### Recipe 6 — grouped mutually-exclusive selection

**Use for:** picking exactly one of N — plan picker, status selector, the
"which list does this belong to" radio-like control rendered as buttons.

**Primitive:** `ui.ToggleAction` with a shared `Group` key. Runtime:
`toggleaction.js`. Committing any button in the group optimistically
reverts the previously-committed sibling (no extra RPC; the server stays
the source of truth and a later navigation refreshes from server state).

**Compose:**

```go
ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
    ui.ToggleAction(ui.ToggleActionConfig{
        Endpoint:       "/api/plan/free",
        IdleLabel:      "Free",
        CommittedLabel: "Free ✓",
        Group:          "plan-picker",
        Committed:      currentPlan == "free",
    }),
    ui.ToggleAction(ui.ToggleActionConfig{
        Endpoint:       "/api/plan/pro",
        IdleLabel:      "Pro",
        CommittedLabel: "Pro ✓",
        Group:          "plan-picker",
        Committed:      currentPlan == "pro",
    }),
)
```

**Handler contract:** each button POSTs its own endpoint on commit. The
`Group` mutex is **purely client-side** — the server sees two independent
mutations. If the user clicks Pro then immediately clicks Free before
either RPC settles, both fire; the server must treat them as the
independent idempotent writes they are (or apply the version-aware 409
pattern from Recipe 5 to make the second reject). On a navigation
refresh, SSR re-reads `currentPlan` from the database and renders exactly
one button as `Committed`, so client and server reconverge.

**Authorization:** each per-plan endpoint enforces its own authorization.
A user who can read "Pro" but not select it gets a 4xx on the Pro POST;
the optimistic flip reverts and the previously-committed sibling stays
committed (the runtime reverts the failed button to idle; it does not
re-commit the sibling — that happens on the next navigation).

**Runnable proof:** the "Free / Pro" cluster on
[`/components/toggleaction`](../../../examples/site/components.go). E2E:
`TestE2E_ToggleAction_GroupMutex`.

### Recipe 7 — slow / network-failure behavior and retry

**Use for:** every optimistic surface — what the user sees when the
network is slow, the server is down, or the response never comes. This
recipe is not a separate component; it is the failure path every other
recipe must survive.

**Primitive:** the `error`/`idle` revert path in `optimisticaction.js`,
the silent revert in `toggleaction.js`, the rollback in `sortablelist.js`,
and `ui.NetworkRetryBanner` for the global "you appear to be offline"
surface.

**What happens on failure:**

- `OptimisticAction` paints the `error` state (shake animation, disabled
  by `prefers-reduced-motion`), then reverts to `idle` after ~600 ms. It
  dispatches `optimistic-action:rolled-back` so app code can hook in.
- `ToggleAction` reverts directly to the prior state (`committed` →
  `idle` on a failed untoggle; `idle` → `idle` on a failed commit). No
  shake; see [Consistency notes](#consistency-notes).
- `sortablelist` restores the destination column from its captured
  snapshot. With `Version` set, a 409 takes the conflict-refresh path
  instead; without it, any non-2xx rolls back.
- `NetworkRetryBanner` shows after a configurable run of failures
  (`FailureThreshold`, default 3). It hides when the Retry button's
  health-check returns 2xx, or when app code calls
  `window.__gofastr.networkStatus.reportRecovery()`. It does **not** wrap
  `window.fetch` — apps wire `reportFailure`/`reportRecovery` into their
  own RPC error handlers.

**Retry.** The primitives do not auto-retry; they roll back and let the
user try again. For mutations that should retry transparently (sync,
upload, batch), use a durable queue with at-least-once delivery
([Job queue](queue.md)) and idempotency keys ([Idempotency](idempotency.md))
so a retry cannot double-apply.

**Compose (a demo that exercises the failure path):**

```go
ui.OptimisticAction(ui.OptimisticActionConfig{
    Endpoint:     "/__site/optimistic/fail", // always 4xx
    IdleLabel:    "Save (will fail)",
    SuccessLabel: "Saving…",
})
```

**Handler contract for the demo:** `POST /__site/optimistic/fail` →
422 (or any non-2xx). `POST /__site/optimistic/slow` → 2xx after a
~2 s delay (exercises the pending window). The runtime shakes + reverts
on the fail endpoint; commits after the delay on the slow endpoint.

**Authorization:** a 401/403 is a non-2xx like any other — the
optimistic projection rolls back. Pair with the global auth redirect at
the page level, not the per-button level; the button should never be
asked to handle a session expiry.

**Runnable proof:** [`/components/optimisticslow`](../../../examples/site/components.go).
The demo pairs a slow endpoint (exercises `pending` then commits) with a
fail endpoint (exercises `error` then rolls back) and renders a
`NetworkRetryBanner`. E2E: `TestE2E_Optimistic_Slow_PendingThenCommit`,
`TestE2E_Optimistic_Fail_RollsBack`.

---

## Consistency notes

This section audits the four primitives that participate in optimistic
mutations — `interactive.OptimisticUpdate`, `ui.OptimisticAction`,
`ui.ToggleAction`, and versioned `sortablelist` — for behavioral
consistency. Applications composing these into the recipes above should
know where they agree and where they diverge.

### Agreed everywhere

- **`pending` disables the trigger.** All three button primitives set
  `disabled=true` and `aria-busy="true"` during `pending`, and clear both
  on settlement. Re-entry during `pending` is a no-op.
- **CSRF forwarding.** All fetches forward `<meta name="csrf-token">` as
  `X-CSRF-Token`. The handler is responsible for verifying the token; the
  runtime just makes it available without per-call-site plumbing.
- **Button-scoped re-entry safety.** Each trigger keeps at most one
  in-flight request. A second click cannot start a second fetch on the
  same element, so responses cannot arrive out-of-order within one
  button.
- **`prefers-reduced-motion`.** The OptimisticAction shake animation is
  disabled under the media query. The visible label flip is unchanged
  because it is informational, not decorative.

### Known inconsistencies

These are real gaps. They are documented here rather than silently
papered over; each is a medium-sized change (runtime + Go + tests) that
should be undertaken deliberately, not as a side effect of a docs pass.

1. **`ToggleAction` has no failure indication.** On a non-2xx response,
   `ToggleAction` silently reverts to the prior state. `OptimisticAction`
   paints an `error` shake first, then reverts. A user who clicks
   `ToggleAction`, sees it flip, and then sees it flip back gets no
   explanation — they have to infer "the RPC failed." A future revision
   should give `ToggleAction` the same `error` state + shake
   (`data-state="error"`, the `ui-optimistic-action-shake` keyframes,
   `prefers-reduced-motion` guard) and announce the rollback.

2. **No spoken announcement of commit / rollback.** `sortablelist`
   announces grab/move/rollback/conflict through a polite `aria-live`
   region. `OptimisticAction` and `ToggleAction` do not — they rely on
   the visible label flip and `aria-busy` toggling. Sightless users
   perceive pending state but not "Saved ✓" or "Rolled back." The
   custom events (`optimistic-action:committed`,
   `optimistic-action:rolled-back`, `toggle-action:commit`,
   `toggle-action:untoggle`) are available for app code to write into an
   `aria-live` span today; a future revision should ship that span
   inside the component so every optimistic surface is announced
   consistently.

3. **`OptimisticAction` is fire-and-forget; `ToggleAction` is too.**
   Neither serializes form data. For mutations that must transmit a
   value, use `interactive.OnSubmit` (which keys the JSON body off each
   control's `name`). This is by design — keeping the optimistic
   primitives payload-free makes them composable — but it is the reason
   Recipe 2 reaches for two patterns side by side. Documented here so
   the next author does not look for a "body" config field that does
   not exist.

### Choosing among the four

- **One-shot commit, no payload, no untoggle** → `ui.OptimisticAction`
  (or `interactive.OptimisticUpdate` for the lower-level wrapper).
- **Binary state, flippable, optionally grouped mutex** →
  `ui.ToggleAction`.
- **Reorder or cross-column move with concurrency** → versioned
  `sortablelist`.
- **Anything that must transmit form data** → `interactive.OnSubmit`,
  optionally paired with one of the above for the optimistic visual.

---

## See also

- [Interactive patterns](interactive-patterns.md) — the full
  `data-fui-*` vocabulary, including the sortable list and ConfirmAction
  reference.
- [UI capability map](ui-capability-map.md) — the wider state boundary
  (signals vs RPC vs SSE) and where optimistic state lives.
- [Runtime contract](runtime-contract.md) — the SSR/hydration/island/SSE
  model and the `data-fui-*` attribute reference.
- [UI composition recipes](ui-composition-recipes.md) — page grammar for
  the surfaces these recipes compose into.
- [Idempotency](idempotency.md) — the server-side `Idempotency-Key`
  contract that makes optimistic mutations safe to retry.
- [Events and SSE](events.md) — why SSE is push-only and is never the
  mutation's confirmation channel.

## Common mistakes

- **Waiting for SSE to confirm a user action.** The HTTP response is the
  authority; SSE tells *other* sessions. Treat the response as the
  commit signal.
- **Rolling back without capturing the previous projection.** If your
  handler returns non-2xx, the runtime needs somewhere to roll back to.
  For `OptimisticAction` that is the idle label; for a custom island,
  you must keep the prior HTML/value yourself.
- **Treating a 409 as a generic failure.** Without `Version` it is; with
  `Version` it triggers server-rendered reconciliation. Use 409 only for
  genuine version conflicts, never for authorization (that is 401/403).
- **Sending a temp id to the database.** A temp id is a client-only
  correlation key. The handler mints the real id; the response carries
  it back; the runtime replaces the temp row.
- **Letting the user edit a temp row.** A temp row is not yet real. Make
  it non-interactive until the authoritative replacement lands.
- **Authorizing at the trigger.** The trigger renders for viewers; the
  handler enforces. A 4xx response is the runtime's honest "rolled back"
  signal — return it instead of redirecting when permission is missing.
- **Adding a second styling surface for the optimistic states.** The
  `[data-state="pending|committed|error"]` selectors are the styling
  surface; compose them, do not invent parallel classes (Hard rule 7).
