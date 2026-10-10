# Admin rebuild status

As of 2026-10-10. The admin rebuild ships as five stacked PRs (gh stack #492).
All five were green at the last check. The top PR (#495) has 68 commits on
top of its pushed head (5b53d2af), plus this status update, that are not on its
branch yet. They live on `wip/admin-shell-remote`, which has every gap from the
last status closed.

The approved design is the "D · Hybrid" prototype
(https://claude.ai/artifact/5xg22eTGdiZbUYaRraijYL). A copy sits at
`docs/admin-prototype.html` for parity work; remove it before the branch
lands on #495.

## PR stack

The PRs merge bottom up. Never rebase them; merge the base in instead.

| PR | Branch scope | State |
| --- | --- | --- |
| #486 | Theming | Open, green, not merged. The bottom of the stack, so it merges first. |
| #491 | P0: entity Display config, views, facets, boot validation | Open, green |
| #493 | States: transitions, audit, overrides | Open, green |
| #494 | entityui: lists, records, forms, bulk, saved views | Open, green |
| #495 | Admin shell (`feat/admin-shell`) | Open, green on pushed head 5b53d2af; 68 commits waiting on `wip/admin-shell-remote` |

At the last check every PR passed its 17 checks, showed 0 unresolved review
threads, and was not behind its base or main. CodeRabbit skipped #494 and #495
and was rate-limited on the others, so those PRs have no bot review.

Review dispositions for #495 are in its issue comment 6044570777.

## Work waiting for #495

Newest first, on top of 5b53d2af (merge commits left out):

- 292baa83 fix(ui): an inline editor's checkbox is not a selected row; toolbar tools wrap
- a6ddb619 feat(admin): entity cards draw their icon in a tile, as the prototype's do
- 741afefb feat(admin): the Jobs filter is a strip of links with counts
- d7e141b5 feat(entityui): a list says how many rows show and offers rows per page
- 36662a51 feat(entityui): a table cell's editor sits in the cell
- b9189ada feat(admin): the dashboard's figures are one strip, and failed jobs need attention
- f7505fc6 feat(ui): a list's bulk bar floats under the rows, as the prototype's does
- d1a8267a docs: list where the admin still looks different from the prototype
- f90ef59b docs: record the full test run in the admin status doc
- 6a92d337 test(backoffice): the supplier field is a searchable picker
- 82a088ac fix(meridian): the blueprint seeds the same payments as the app
- 3a5564cf docs: the admin parity round is done; rewrite the status doc
- dafa6476 feat(admin): the dashboard's primary New button
- b879680f feat(meridian): a Revenue report and seeded payments
- e1a21d37 feat(meridian): a Spanish catalog and a pseudo-locale overflow test
- 00618528 feat(i18nui): a pseudo locale for overflow testing
- d1007e5e feat(admin): the page theme is the account page's Look
- 4715b885 fix(entityui): an upload's prompt is translated
- 69182ad2 fix(ui): chips, short ids and stat-card actions fit a narrow box
- d48d2147 feat(entityui): side panels on a record, and Meridian's Billing panel
- 231f6cd6 feat(ui): DateTimeField, and entity timestamps use it
- d6f30d44 feat(entityui): table cells edit in place
- 1e70356d feat(entityui): a list switches between table and cards
- 17fbef7d feat(admin): a Brutal page theme, picked from the toolbar
- 1c03aee4 docs: note the parity round's progress in the status doc
- 54f15e28 feat(ui): a JSON text area is checked as JSON while it is typed
- 3e629c89 feat(admin): a late dashboard count falls back to a bounded read
- fe0159c6 feat(entityui): the record menu offers Create another and Copy API URL
- aeb5079f feat(admin): / focuses the list search and ? opens the keyboard help
- 62a8b17f feat(entityui): filter rows and column reordering on lists
- 1b5c32a0 fix(crud): a multipart form takes a bool's checkbox pair; the runtime stays in budget
- dae667f5 feat(entityui): Image and File fields upload into storage
- 72fd1918 feat(admin): the Jobs page shows status, updated and last error, and Meridian has one
- 7c3ec12b style(entityui): gofmt the phone-row test
- b7fcd0e0 feat(entityui)!: a relation field is a searchable picker with New
- b8b50b7f feat(ui): Picker picks one record from a server-searched list
- 89fe16f8 feat(ui): a DataTable can be two-line rows on a phone; entity lists use it
- f46c6e3e docs: add the admin prototype for parity work
- d0c3b2c2 docs: record where the admin rebuild stands
- 675300cd feat(admin): the audit log names role, account and bulk changes
- 2fc285b3 feat(meridian): billing and support roles, editable from the admin
- c88168d1 feat(admin)!: the Roles page is a permission grid
- bbe8211b fix(ui): a DataTable card keeps a value of several parts together
- acc6be96 fix(headless): the behaviour gates read the bell module
- 6ec5c8fb feat(headless): the leave guard asks in the kit's dialog
- e6a70495 feat(entityui): the query box's syntax reference is laid out as code
- 45601766 feat(ui): InlineCode draws a short piece of code inside text
- 14c2e17e feat(entityui): the query box says how to write a filter
- dd54ad6e feat(admin): a bulk run reads as what it did in the activity feed
- 04555566 feat(admin): the activity feed names the entity and lists an edit's changes
- dfd359e1 fix(admin): the dashboard names a deleted record by its title
- 31d5d28e fix(ui): an empty data table draws no select-all box
- ff1c45b5 feat(examples): a Meridian payment is named by its invoice and customer
- 23900b81 feat(entityui): a bulk delete's toast offers Undo
- 4c8ce2f7 feat(ui): a server toast carries a button
- e1a982a2 fix(admin): the User roles page drops its count line
- 0ad8c035 feat(ui): ShortID shows a long identifier and copies all of it
- 5024f499 feat(admin): the Jobs page pages through every job
- ec7aad55 feat(admin): the User roles page pages through every account
- d68415b5 feat(admin): the audit log pages back through older rows
- 5ab6ecc0 fix(ui): an empty data table keeps its empty state in view
- ddaeeed2 fix(entityui): a restore or purge shows one toast
- 71564abf feat(admin): a related record stacks as a drawer
- e4bfafd2 feat(entityui): a soft delete's toast offers Undo
- c4a1b19e feat(headless): a success toast carries one action button
- c7834c08 feat(entityui): a record drawer steps through the list under it
- aac3d3d1 feat(ui): DrawerBar steps to the previous and next record
- 7edda1cd feat(core-ui): a swap link renders its target in the top intercept layer

## Feature inventory

All 23 feature groups in the plan are done.

Done this round: lists become two-line rows on a phone; a searchable
relation picker with New, whose open button stacks the related record as a
drawer (the prototype's peek); Image and File fields upload into storage;
timestamps use `ui.DateTimeField`; JSON is checked in the browser; the `/`
key and the shortcut sheet; a late dashboard count falls back to a bounded
read; record side panels (`Extension.Side`); a pseudo locale with an overflow
test; and in Meridian a Revenue report, seeded payments behind the invoice's
Payments tab, a Spanish catalog and the Jobs page.

Done before: entity Display config, views and facets; States and
transitions; form layouts, sections and read-only fields; lists (search,
filters, sort, paging, empty states); record pages (related, activity, API,
duplicate, trash); bulk actions with every-match caps, jobs and undo; saved
views with owner and tenant isolation; the shell, sidebar, breadcrumbs,
palette, theme and account; the audit log and Roles pages; entity tools and
count endpoints; the security guards (authz, scope, masking, cross-site, body
caps, no-store); generator, SDK and LLM metadata; removal of the old
`/admin/e/` pages.

Deferred on purpose, not counted as gaps: version history and drafts, edit
locks, preview, translated field values, locale formatting, RTL, single-record
screens, trees, media library, manual ordering, board, calendar and grouped
rows, MCP action tooling.

## Prototype comparison

Features checked on 2026-10-09 against the prototype's source and
Meridian screenshots (light, dark, 375px phone). A pixel comparison against
the rendered prototype on 2026-10-10 found seven visual differences. The
same day closed them; "Looks different" below says how, and names what
is left.

| Surface | Matches the prototype | Differs |
| --- | --- | --- |
| Shell and navigation | Sidebar groups and counts, collapsible nav, breadcrumbs, ⌘K palette, theme toggle, account menu, `/` and `?` keys | The look (Default or Brutal) is on the account page, not in the account menu: a menu cannot hold a radio group. |
| Dashboard | Metrics as one strip, entity cards with New, a page-level New Customer, attention, recent activity, failed jobs, polling | — |
| Entity lists | Search, sort, paging, saved views, counts, columns (hide and reorder), filter rows, table and cards switch, inline editing, bulk actions, CSV, phone rows | — |
| Record drawer and forms | Drawer, previous and next, related, activity and API tabs, leave guard, delete with undo, duplicate, Create another, Copy API URL, relation picker with New, related record stacked as a drawer, side panels, Open in panel from the full page back to the drawer | The prototype's side column is sticky; ours scrolls with the form. |
| Operations | Jobs with status filters, updated and last-error columns, replay; audit filters and diffs; Roles grid; User roles; account and password | — |
| Theme | Light, Auto and Dark; Default and Brutal | — |

## Looks different

The seven differences the 2026-10-10 pixel comparison found, and where
each went. All screens re-shot in light, dark and at 375px.

1. Bulk actions: done (f7505fc6). Entity tables float an inverse bar at
   the bottom under the rows, "{n} selected", the action, Apply and a
   clear button (`ui.SelectionConfig.Floating`, `headless.Selection`).
2. Dashboard: done (b9189ada). The figures are one `ui.StatStrip` whose
   fourth figure is Failed jobs ("Needs a replay", red). Failed jobs
   moved into Needs attention, whose rows sit flat in the card.
3. Inline editing: done (36662a51). The editor lies over the cell (the
   field and Save on one line, no label, no pencil). It still opens on a
   click, not a double-click: a double-click has no keyboard twin.
4. List toolbar and footer: done (d7e141b5). Table / Cards is labelled and
   after Columns; the footer shows "1–25 of 40", rows per page and the
   pager.
5. Density: no change needed. Rows are 44.5px under a mouse
   (`ContentRow Dense` keys on `pointer: fine`); the 53px came from the
   headless screenshot browser, which reports a coarse pointer. The
   screenshot helper now takes `SHOT_POINTER=fine`.
6. Queue filters: done (741afefb). A strip of links, "Failed 2", no Apply.
7. Small things:
   - Entity cards put the icon in a tinted tile: done (a6ddb619,
     `StatCardConfig.Tile`).
   - The active sidebar item is a grey fill, not a white pill with a
     border. Left as is: the pill comes from the prototype's theme (a
     white sidebar on a warm page); Meridian's tokens draw the kit's
     active state, and a pill would be a theme option, not a fix.
   - The avatar shows one initial. Left as is: the seeded admin has no
     name (battery/auth users carry an email, not a display name), so
     the avatar takes the email's first letter. A named account shows
     two. Giving auth users a profile name is its own change.

## Known test failures

`./scripts/test-all.sh` ran on 2026-10-09. Two failures came from this
round and are fixed (82a088ac, 6a92d337). The rest fail on the round's
starting commit too, or come from the container:

- `framework` TestContractsFixAdmitsPartialWritesOnFailure,
  `cmd/gofastr` TestVerifyJSONFixFailureCarriesPartialWrites and
  TestVerifyFixReportsPartialWritesOnFailure, `framework/migrate`
  TestGenFileRerunAfterSnapshotFail and `core/upload`
  TestDeleteExistsLeakNoAbsPath: each counts on a write being refused,
  and the container runs as root.
- `core/webbotauth` (four tests): a fetch to a made-up HTTPS host meets
  the container's egress proxy.
- `framework/ui` TestContentRowDenseOnFinePointer: the headless browser
  here does not report a fine pointer.
- `examples/webmcp-remote-assist` TestRemoteAssistFlow: the known Chrome
  WebMCP `executeTool` change.
- `internal/upgrade` and its `scan` packages failed only under
  `GOTOOLCHAIN=go1.27.2`, which leaves the type-checker reading export
  data from a newer compiler. They pass on the default toolchain.
- `core-ui/runtime` TestTransitionPickedByDestination timed out alone
  here and on the starting commit; it passed in the full run.

## Next steps

Before #495 is ready:

- [ ] Remove `docs/admin-prototype.html`.
- [ ] Bring `wip/admin-shell-remote` onto `feat/admin-shell` (merge, never rebase).
- [ ] Base check: `git fetch origin`, `git rev-list --count HEAD..origin/<base>`, merge the base in if behind.
- [ ] Push `feat/admin-shell` with the full hook (never `--no-verify`).
- [ ] Rewrite the #495 body from the commit list above, then `gh pr edit 495 --body-file`.
- [ ] `./scripts/pr-review-findings.sh 495 --gate`, triage every thread, then watch CI.
- [ ] Remove the stale `wip/drawer-steps` branch and worktree.

## Where things live

- Code: `wip/admin-shell-remote`, to land on `feat/admin-shell`.
- Demo: `examples/meridian`, with its own SQLite database and the admin at
  `/admin`. Sign in with the account seeded from `ADMIN_SEED_PASSWORD`.
  `MERIDIAN_PSEUDO_LOCALE=1` adds the en-XA pseudo locale; a Spanish browser
  gets the Spanish catalog.
- Prototype: https://claude.ai/artifact/5xg22eTGdiZbUYaRraijYL ("D · Hybrid").
