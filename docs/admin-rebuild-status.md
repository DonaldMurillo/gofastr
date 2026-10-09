# Admin rebuild status

As of 2026-10-09. The admin rebuild ships as five stacked PRs (gh stack #492).
All five are green. The top PR (#495) has 29 local commits that have not been
pushed. The admin still lacks phone list cards, a searchable relation picker,
file upload, and a Queue page in Meridian.

The approved design is the "D · Hybrid" prototype
(https://claude.ai/artifact/5xg22eTGdiZbUYaRraijYL).

## PR stack

The PRs merge bottom up. Never rebase them; merge the base in instead.

| PR | Branch scope | State |
| --- | --- | --- |
| #486 | Theming | Open, green, not merged. The bottom of the stack, so it merges first. |
| #491 | P0: entity Display config, views, facets, boot validation | Open, green |
| #493 | States: transitions, audit, overrides | Open, green |
| #494 | entityui: lists, records, forms, bulk, saved views | Open, green |
| #495 | Admin shell (`feat/admin-shell`) | Open, green on pushed head 5b53d2af; 29 commits unpushed |

At the last check every PR passed its 17 checks, showed 0 unresolved review
threads, and was not behind its base or main. CodeRabbit skipped #494 and #495
and was rate-limited on the others, so those PRs have no bot review.

Review dispositions for #495 are in its issue comment 6044570777.

## Unpushed work on #495

Newest first, on top of 5b53d2af:

- 675300cd feat(admin): the audit log names role, account and bulk changes
- 2fc285b3 feat(meridian): billing and support roles, editable from the admin
- c88168d1 feat(admin)!: the Roles page is a permission grid (renames `_revoke` to `_permissions`)
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

Of 23 feature groups in the plan, 14 are done, 7 are partly done and 2 are
missing.

| Feature | State | Gap |
| --- | --- | --- |
| Relation picker | Missing | A plain select of the first 100 records (`framework/entityui/record_form.go` `relationSelect`). No search, no "New", no warning when the list is cut off. |
| Image and file fields | Missing | A preview above an editable URL text box. No upload into storage. |
| Date and time inputs | Partial | Timestamps use the browser's `datetime-local` input, not `ui.TimePicker`. |
| JSON fields | Partial | Validated by the server on save; nothing checks them in the browser first. |
| Keyboard shortcuts | Partial | ⌘K, Escape and ⌘S work. The `/` search-focus key and the shortcut help sheet are not wired. |
| Dashboard counts | Partial | Polling, hidden-tab pause and `10k+` work. A failed or late count shows "—" instead of an estimate. |
| Extensions | Partial | No hook for a side panel on a record page. |
| Translations | Partial | Admin and entity keys exist. No pseudo-locale test catches text that overflows the layout. |
| Meridian dogfood | Partial | No revenue report, no Payments tab, no Spanish catalog. |

Done: entity Display config, views and facets; States and transitions; form
layouts, sections and read-only fields; lists (search, filters, sort, paging,
empty states, mobile); record pages (related, activity, API, duplicate, trash);
bulk actions with every-match caps, jobs and undo; saved views with owner and
tenant isolation; the shell, sidebar, breadcrumbs, palette, theme and account;
the queue, replay, audit log and Roles pages; entity tools and count endpoints;
the security guards (authz, scope, masking, cross-site, body caps, no-store);
generator, SDK and LLM metadata; removal of the old `/admin/e/` pages.

Deferred on purpose, not counted as gaps: version history and drafts, edit
locks, preview, translated field values, locale formatting, RTL, single-record
screens, trees, media library, manual ordering, board, calendar and grouped
rows, MCP action tooling.

## Prototype comparison

This compares the source against the prototype. It is not a side-by-side
screenshot check, which is still owed for every screen.

| Surface | Matches the prototype | Differs |
| --- | --- | --- |
| Shell and navigation | Sidebar groups and counts, collapsible nav, breadcrumbs, ⌘K palette, theme toggle, account menu | — |
| Dashboard | Metrics, entity cards, attention, recent activity, polling, per-card New | No page-level "New customer" button. The Queue card is absent in Meridian. |
| Entity lists | Search, server-side sort and paging, saved views, counts, column visibility, bulk actions, CSV, New | Tables scroll sideways on a phone instead of becoming cards. No layout switch, no structured filter rows, no column reordering, no inline editing. |
| Record drawer and forms | Drawer, previous and next stepping, related, activity and API tabs, leave guard, delete with undo, duplicate | No relation preview (PeekView). No "Create another" or "Copy API URL". |
| Operations | Audit filters and diffs, Roles grid, User roles, account and password, confirmations, toasts, empty states | Queue not wired in Meridian. The queue page lacks Done and Running filters and Status, Updated and Last-error columns. |
| Theme | Light, Auto and Dark | No Brutal theme; the admin uses `ui.ThemeToggle`, not `ui.ThemePicker`. |

## Gaps ranked

Worst for day-to-day usefulness first:

1. Entity lists scroll sideways on a phone. `framework/entityui/list_table.go`
   sets `Responsive: ui.ResponsiveScroll`. The card layout works (fixed in
   bbe8211b), so this is a switch plus screenshots.
2. The relation picker is a capped dropdown with no search, "New" or preview.
3. Image and file fields have no upload.
4. Meridian does not set `Queue` on `admin.Config`
   (`examples/meridian/main.go`), so the demo has no Queue page; the queue page
   also lacks the prototype's filters and columns.
5. Filters are one query box, not field, operator and value rows. Columns can
   be hidden but not reordered.
6. Smaller items: the `/` key and shortcut help sheet; "Create another" and
   "Copy API URL" on records; a dashboard count estimate on timeout; JSON
   checked in the browser; the Brutal theme.
7. Not built: inline table editing and a table/cards switch on lists.

## Next steps

Options for the next round:

- Fix gaps 1, 2 and 4, then push the batch (recommended). These are the first
  things a person notices in the demo.
- Push the 29 commits now and take the gaps as a follow-up round.
- Work through the whole list before pushing. The largest option, and the stack
  is more likely to fall behind main.

Before #495 is ready, run once at the end of the batch:

- [ ] Full suites: `./scripts/test-all.sh`.
- [ ] Base check: `git fetch origin`, `git rev-list --count HEAD..origin/<base>`, merge the base in if behind.
- [ ] Push `feat/admin-shell` with the full hook (never `--no-verify`).
- [ ] Rewrite the #495 body from the commit list above, then `gh pr edit 495 --body-file`.
- [ ] `./scripts/pr-review-findings.sh 495 --gate`, triage every thread, then watch CI.
- [ ] Remove the stale `wip/drawer-steps` branch and worktree.
- [ ] Screenshot every admin screen beside the prototype: light, dark and phone.

## Where things live

- Code: branch `feat/admin-shell`. Find its checkout with `git worktree list`.
- Demo: `examples/meridian`, built from that branch with its own SQLite
  database and the admin at `/admin`. Sign in with the account seeded from
  `ADMIN_SEED_PASSWORD`.
- Prototype: https://claude.ai/artifact/5xg22eTGdiZbUYaRraijYL ("D · Hybrid").
