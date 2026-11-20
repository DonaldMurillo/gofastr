# Optimistic UI: mutations, rollback, and reconciliation

Optimistic UI paints the expected result immediately on click, then lets the HTTP response confirm it, run conflict reconciliation, or roll it back. In GoFastr, optimistic updates are composed from `ui.OptimisticAction`, `ui.ToggleAction`, versioned `ui.SortableList`, `ui.ConfirmAction`, and `core-ui/interactive` RPC actions.

## Rollback and conflict reconciliation

Every optimistic primitive follows the same five-state machine (`data-state` on the trigger or sortable `<ol>`):

```text
capture previous projection
  → apply optimistic projection (data-state="pending", aria-busy="true")
  → send HTTP mutation (with Idempotency-Key / version token as needed)
  → 2xx: committed (reconcile authoritative result)
  → 409 (with version token): conflicted → conflict reconciliation (fetch authoritative HTML)
  → non-2xx / network error: failed → rollback after ~600ms and announce
```

- **Re-entry safe per button:** During `pending`, controls set `aria-busy="true"` (never `disabled`, so keyboard focus doesn't jump to `<body>`) and ignore duplicate clicks. For cross-tab or financial idempotency ("charge once"), send an `Idempotency-Key` header (see [idempotency.md](idempotency.md)).
- **HTTP response is the authority—never SSE:** The initiating tab commits, reconciles, or rolls back from its HTTP response. SSE is push-only for *other* sessions; never wait on an SSE event to confirm your own write.
- **Rollback vs. reconciliation:** Roll back locally when the server couldn't accept or wasn't reached (network error, 4xx, 5xx). Use authoritative reconciliation (refetch fresh HTML) when a versioned `409 Conflict` means server state moved underneath the client.

## Recipes

### 1. Binary toggle / follow (`ui.ToggleAction`)

Use for binary server-backed state (follow, watch, subscribe). SSR renders `Committed` (`aria-pressed` mirrors it); clicking flips optimistically and rolls back on non-2xx. Set `AllowUntoggle: true` (plus optional `UntoggleEndpoint`) to allow toggling back:

```go
ui.ToggleAction(ui.ToggleActionConfig{
    Endpoint:         "/api/follow/42",
    UntoggleEndpoint: "/api/unfollow/42",
    IdleLabel:        "Follow",
    CommittedLabel:   "Following ✓",
    Committed:        alreadyFollowing,
    AllowUntoggle:    true,
})
```

### 2. Grouped mutually-exclusive buttons (`ui.ToggleAction` + `Group`)

Share a `Group` name across sibling `ToggleAction` buttons (e.g., plan pickers) for a client-side mutex. Committing one optimistically reverts the previously committed sibling; if the new POST returns 4xx, the previous sibling restores automatically.

### 3. One-shot optimistic commit (`ui.OptimisticAction`)

Use for payload-free commits (approve, archive, freeze). Flips to `SuccessLabel` immediately; shakes and reverts on non-2xx (respecting `prefers-reduced-motion`):

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/ui"
-->
```go
ui.OptimisticAction(ui.OptimisticActionConfig{
    Endpoint:     "/api/rename/validate",
    IdleLabel:    "Save",
    SuccessLabel: "Saved ✓",
})
```

Note: `OptimisticAction` and `ToggleAction` are fire-and-forget and do not serialize form fields. To transmit edited field values, use `interactive.OnSubmit` (with `interactive.AfterText("Saved ✓")` or a signal swap).

### 4. Optimistic create / list append (`interactive.OnClick` + HTML signal)

Bind a list container with `data-cui-signal="opt-create-list"` and `data-cui-signal-mode="html"`, and return the authoritative list HTML on 2xx. On non-2xx, the runtime broadcasts `{ok:false, status, text}`—`html`-mode regions ignore non-string error objects so the list stays intact while an optional sibling `text`-mode status node displays the error.

For true pre-fetch row insertion, mint a client-only `temp:<id>` placeholder in a custom island (keep temp rows non-interactive, never persist `temp_id` as a real ID, and replace or remove it on HTTP settlement).

### 5. Delete with confirmation & implicit restore (`ui.ConfirmAction`)

Pass `SuccessSignal: "item-list"` to `ui.ConfirmAction` and return the updated list HTML on 2xx. Because the list only swaps on 2xx, any 4xx/5xx leaves the row untouched; for an explicit Undo button on soft-deletes, pair `interactive.Delete(...).OnSuccessToastAction("Undo", ...)` (see [interactive-patterns.md](interactive-patterns.md)).

### 6. Version-aware Kanban / sortable move (`ui.SortableList`)

Pass `Version` and `ConflictRPC` so concurrent board edits reconcile instead of silently overwriting:

<!-- gofastr:compile
import "fmt"
import "github.com/DonaldMurillo/gofastr/framework/ui"
type w4Col struct{ Title, ID string }
type w4Board struct{ Version int }
var col = w4Col{Title: "todo", ID: "col-1"}
var board = w4Board{Version: 2}
var items []ui.SortableItem
-->
```go
ui.SortableList(ui.SortableListConfig{
    Label:       col.Title,
    Group:       "board-1",
    Container:   col.ID,
    RPCPath:     "/api/board/move",
    Version:     fmt.Sprintf("v%d", board.Version),
    ConflictRPC: "/api/board/conflict?container=" + col.ID,
    Items:       items,
})
```

- On 2xx move: bump version and call `ui.RenewSortableVersion(w, nextVersion)` so every column in the `Group` holding the previous token advances together.
- On version mismatch: return `409 Conflict` via `handler.WriteError` (`{"error":"..."}`). The runtime announces the message, GETs `ConflictRPC` (`ui.SortableListItems` + `ui.RenewSortableVersion`), and replaces the destination column.
- Use `409` only for concurrency conflicts; return `403` for authorization failures (rolls back instead of reconciling).

### 7. Slow networks & offline state (`ui.NetworkRetryBanner`)

Optimistic primitives never auto-retry—they roll back and let the user retry (use [battery/queue](queue.md) + [idempotency](idempotency.md) for automatic background retries). `ui.NetworkRetryBanner` tracks `window.__gofastr.sseStatus` (and custom `window.__gofastr.networkStatus.reportFailure()` / `reportRecovery()` calls) to announce offline state and probe a health endpoint.

## Choosing the right primitive

| Need | Primitive |
|---|---|
| One-shot commit, no form payload | `ui.OptimisticAction` |
| Binary toggle or mutually-exclusive group | `ui.ToggleAction` |
| Drag/keyboard reorder with concurrency | Versioned `ui.SortableList` |
| Form payload submission | `interactive.OnSubmit` (+ signal swap or `AfterText`) |

All button primitives emit `action:start`, `action:committed`, `action:rolled-back`, and `action:untoggle` events if you want to hook custom `aria-live` announcements (`SortableList` announces automatically via `aria-live="polite"`, and `framework/headless` action buttons include a failure status span). Note that a failed `ToggleAction` untoggle stays `committed` silently (nothing to roll back to).

## Common mistakes

- **Waiting for SSE to confirm a user action.** Treat the HTTP response as the commit signal; SSE updates *other* viewers.
- **Returning 409 for authorization failures.** Return 401/403 so the client rolls back; reserve 409 for version conflicts.
- **Forgetting `ui.RenewSortableVersion` on 2xx moves.** Without renewing the token header, the very next drag sends a stale version and conflicts.
- **Persisting a client `temp_id` or letting users edit temp rows.** Temp IDs are client-only correlation keys until the server returns the real ID.
- **Authorizing only at the UI trigger.** Always enforce permissions in the HTTP handler and return 4xx (not a redirect) on denial so the optimistic state rolls back cleanly.
- **Inventing parallel CSS classes for optimistic states.** Style `[data-state="pending|committed|error"]` instead.
