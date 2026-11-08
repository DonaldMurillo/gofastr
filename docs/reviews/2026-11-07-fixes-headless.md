# Fixes for C19 and C20 — November 7, 2026

Base: `7d8cd9eea` (main), shared checkout. Uncommitted working-tree changes;
no commit, branch or PR.

| ID | Status | Production change | Regression test |
| --- | --- | --- | --- |
| C19 | Fixed | `framework/headless/menu.js` | `framework/headless/menu_tab_e2e_test.go` |
| C20 | Fixed | `framework/headless/sortablelist.{js,go}`, `framework/ui/sortablelist.go`, `examples/site/main.go` | `framework/headless/sortablelist_version_e2e_test.go`, `examples/site/sortable_version_test.go`, `examples/site/e2e_sortable_kanban_test.go` |

## C19: Tab from a native summary closes the menu

The keydown Tab branch closed a menu only when focus sat inside a
`[data-hui-menu-trigger]` wrapper (caller-owned trigger) or on a menuitem.
A native top-level summary is neither, and with every row disabled the
focus-on-open lookup finds no row, so focus stays on the summary. The
branch now also resolves `details[data-hui-menu] > summary` to its parent
`<details>` and runs the same `closeChain`. Tab is still not prevented, so
focus moves on natively. A submenu parent summary also matches; closing its
own details first is harmless, because the menuitem branch then closes the
whole chain as before.

Unchanged: the Space and type-ahead branch. Space during an active 800 ms
type-ahead buffer still does not click a link (the rejected candidate in
the review); `TestMenuSpaceActivatesLink` passes.

`TestMenuTabClosesFromEveryTrigger` is a real-browser matrix: native summary
vs caller-owned button, rows all disabled vs enabled, Tab vs Shift+Tab (8
subtests). Keys are trusted chromedp key events: Enter runs the native
toggle, Tab runs the browser's own sequential focus navigation. Each case
asserts the menu opened, where focus sat after Enter (summary, button or
the first row), and that the menu is closed after the key. On the summary
path it also asserts where focus landed: After / Before for the disabled
rows; After / the closed summary for the enabled rows.

Guard proof: with the pre-fix `menu.js` (HEAD) restored, exactly the two
`summary, rows disabled` subtests fail and the six controls pass.

Limit found while writing it, not fixed and not validated as a defect: on
the caller-owned trigger path the `<details>` has no `<summary>`, so the
browser supplies its own, and in the headless test page (no kit CSS) that
implicit summary is a sequential focus stop. Tab from the button, or
Shift+Tab from a row, lands there. The menu still closes. The test asserts
focus targets on the summary path only. Whether the kit's sheet hides that
stop on a real page was not checked.

## C20: versioned sortable lists renew their token

Contract (documented in `optimistic-ui.md` recipe 5, `interactive-patterns.md`,
`runtime-contract.md` and `core-ui/ARCHITECTURE.md`):

- The server renews a versioned list's token through the
  `X-Gofastr-Sortable-Version` response header, percent-encoded like
  `X-Gofastr-Title`. Go: `headless.RenewSortableVersion(w, version)` and the
  `ui.RenewSortableVersion` wrapper. An empty version sets nothing.
- On a 2xx move, every list of the move's Group (the list alone when
  ungrouped) whose token equals the token the move sent takes the new one.
  Those lists showed the state the server accepted the move against.
- On the conflict refresh, the refreshed list takes the header's version
  together with its rows.
- A list holding any other token keeps it, so a column whose rows may be
  older than the server's still conflicts and refreshes on its next move.
- A 409 never renews, a value that does not decode is ignored, and a
  response with no header changes nothing (a server that never calls the
  helper behaves exactly as before).
- Linked lists share one token, the board's (as the flagship and the
  recipe already did).

`sortablelist.js` reads the token once per commit (`was`), sends it, uses it
for the 409-is-versioned check, and matches it on renewal. `peers(list)` is
the group walk now shared by renewal, `canCross` and the ArrowLeft/Right
column lookup; an ungrouped list is its own only peer.

The flagship's move handler calls `ui.RenewSortableVersion` before its 204,
and its conflict endpoint calls it with the version the rows were read at.
`framework/gallery/catalog.go`'s sortable snippet shows the call.

### Tests

- `TestSortableVersionRenews` (headless, real browser + real HTTP, public
  renderers, `handler.WriteError` 409): three sequential keyboard moves on
  `a,b,c`, each waited out (server log grew, every list's in-flight counter
  back to zero). Versioned and renewed: `204 v1, 204 v2, 204 v3`, list ends
  at the server's order with `v4`. Unversioned control: three 204s, no
  version sent. Never-renewed control (the server does not call the
  helper): `204 v1, 409 v1, 409 v1`, the C20 shape.
- `TestSortableConflictRenewsToken` (headless): two linked columns. A saved
  move in column a renews column b to `v2`; a crossing and a reorder in b
  save. Another writer then bumps the board; the next move in a conflicts,
  and the refresh brings a's server rows with `v5` while b keeps `v4`.
  The next move in a saves; b's next move conflicts and refreshes it, and
  the one after saves. Expected log
  `204 v1, 204 v2, 204 v3, 409 v4, 204 v5, 409 v4, 204 v6`; both columns end
  at the server's rows and token.
- `TestSortableMoveRenewsVersion` (examples/site, handler level): three
  moves return 204 with `v2`, `v3`, `v4`; a stale `v1` move is 409 with no
  header; the conflict GET returns `v4` and the saved order.
- `TestE2E_SortableKanbanThreeMoves` (examples/site, real browser on the
  flagship page): reorder, crossing, reorder, each settled at `v2`, `v3`,
  `v4` on every column; the board and a reload show
  `todo:k2 doing:k1,k3 done:k4`.

Guard proofs, each restored afterwards:

- Success renewal removed: `TestSortableVersionRenews/versioned,_renewed`
  answers `204 v1, 409 v1, 204 v2`, and the linked test fails on column b
  still at `v1`.
- Refresh renewal removed: the linked test fails with column a at
  `a3,a2@v4` after the refresh.
- Renewing every peer regardless of its token: the linked test's log
  shows column b's stale move accepted (`204 v6` where `409 v4` belongs).
- `RenewSortableVersion` removed from the site's conflict endpoint:
  `TestSortableMoveRenewsVersion` fails on the empty header. Removed from
  the move handler: `TestE2E_SortableKanbanThreeMoves` times out waiting
  for `v2`.

### Budget

`sortablelist.js` measured 3076 gzip bytes at HEAD and 3189 with the fix
(+113: the header read and decode, the peer walk; folding two group lookups
into `peers` paid part of it back). Both rows moved to 3191 (measured + 2)
with the reason beside them: `framework/headless/budget_test.go` and
`core-ui/runtime/internal/treegate/budget_test.go`. `menu.js` went from 2205
to 2223, under the 3072 goal.

### Not covered

- The delayed-refresh race in the ledger's unvalidated reserves
  (business-rule rejection, newer accepted move, late refresh) is a
  different scenario and was not reproduced or changed. Two commits in
  flight together still send the same token and the second conflicts, as
  before; the token match keeps a late 2xx from overwriting a newer token.
- Pointer drag was not driven for C20; it shares `commit` with the keyboard
  path.
- No screenshot: no markup or styling changed.

## Commands run

Coordinator validation on the combined checkout also passed the verbose
menu Tab matrix, both version-renewal browser regressions, behavior-module
budgets, the flagship three-move browser persistence test, its handler
header test, and treegate budgets. Repository vettool passed headless, UI,
gallery and site packages. Full docs and changelog gates passed after
coordinator edits. This is not a full site-suite or pointer-drag claim.

All with `-p 2 -count=1` in this checkout:

```sh
go test -run 'TestMenuTabClosesFromEveryTrigger' ./framework/headless/
go test -run 'TestSortableVersionRenews|TestSortableConflictRenewsToken' ./framework/headless/
go test ./framework/headless/                      # whole package incl. browser tests, 201s
go test -run 'Sortable|Internal|Kit|Gallery|Catalog' ./framework/ui/ ./framework/gallery/
go test -run 'TestSortableMoveRenewsVersion|TestSortableMoveDedupsCards' ./examples/site/
go test -run 'TestE2E_SortableKanban' ./examples/site/
go test -short -run 'Sortable|Kanban|Component|DocsCatalog|Embedded' ./examples/site/
go test ./core-ui/runtime/internal/treegate/ ./core-ui/check/ ./framework/docs/...
go vet -vettool=<built ./cmd/vettool> ./framework/headless/ ./framework/ui/ ./examples/site/ ./framework/gallery/
```

All passed after the final edits, with one exception outside this change:
the last `core-ui/check` run fails `TestLintRegistryOwnProps_RepoIsClean`
on `core-ui/runtime/frag/nav.js:969`, another session's uncommitted edit in
this shared checkout; the same package passed earlier in this session
before that edit landed. One `framework/docs` run failed while other
sessions were editing docs and passed on the rerun. The full
`examples/site` suite and `make test` were not run.
