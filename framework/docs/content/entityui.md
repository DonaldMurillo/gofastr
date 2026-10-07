# Entity screens (entityui)

`framework/entityui` draws an entity's list, record and create screens from
its schema, its `Display` config and its `States`. Generated apps draw
through it, the admin battery composes it, and a hand-written app page can
place the same builders anywhere a component goes. It replaced
`framework/ui/resource` (deleted): the old engine mounted island routes
beside each screen. entityui draws no islands, and the only routes it
adds are two JSON endpoints per entity, mounted by `App.EntityUI` beside
the CRUD routes (see "Bulk actions and export").

One `*entityui.UI` serves one app. Build it once, after the entities its
`Extensions` name are registered (an entity registered later still gets
its bulk and export routes); a second `App.EntityUI` call panics, so the
admin battery and the app's screens share the one the app built:

```go
<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/entityui"
var fwApp *framework.App
var ctx = context.Context(nil)
-->
appUI := fwApp.EntityUI(entityui.Extensions{})
```

`App.EntityUI` checks every name the extensions use against the registered
entities and panics at boot on a bad one, naming it, the way `App.Entity`
refuses a bad declaration. An entity registered after `App.EntityUI` gets
the same checks at `App.Entity` (`UI.CheckEntity`): an input naming no kind
panics there, and so does a view with no `Where`, since
`Extensions.Entities` could not name the entity to register its filter.
Builders are components: return them from a
screen, or call `RenderCtx(ctx)` to place one inside another component.

## What it draws

- **A list** (`appUI.List("invoices")`): a `ui.DataTable` (or cards),
  its columns from `Display.Columns`, a title cell that links to the
  record, view tabs, facets, a search box over `SearchFields`, filter
  chips, sort headers and a pager. The row menu offers Open, Duplicate
  and Delete where the builder turns them on. Five more controls are
  off by default on app pages and on in the admin: the query box
  (`.QueryBox()`), the columns menu (`.ColumnsMenu()`), the trash view
  (`.Deleted()`), saved views (`.SavedViews()`) and tab counts
  (`.TabCounts()`) — "Query box, columns menu, trash view, saved
  views, tab counts" covers them.
- **A record** (`appUI.Record("invoices", id)`): a page header with the
  record's title, its state as the badge, and "Created … · Updated …"
  from its timestamps; a button per state move whose `From` holds the
  stored value; one icon-only menu named for the record (Copy link, and
  Duplicate and Delete where turned on); and on the Edit tab, Save. Save
  submits the form from the header, answers Mod+S (⌘S, Ctrl+S), and
  reads as idle until the form has edits; beside it a move or app action
  declared primary draws as secondary, so Save is the header's one
  primary action. Tabs: Edit (the form from `Display.Form`), Related
  (the related entities the page names), any extension tabs, and
  Activity (the audit trail) and API (the record as the API returns it)
  where turned on. Related draws each related entity as an embedded
  list (see below) whose Add opens the create form with this record
  prefilled, and shows how many rows its lists hold, each
  entity counted through its own read gate and scope, and Activity how
  many trail entries it draws (50 at most, "50+" past that). Activity is
  a timeline, newest first: each entry is a sentence with the actor in
  bold ("**ada@example.com** made changes", "… created this invoice",
  "… ran Send", "… overrode the status") and how long ago, and an edit
  lists the fields it changed as a `ui.ChangeList`, each value drawn the
  way the list's cell draws it (a badge, a money figure, a related
  record's title). An update that changed no field the caller may see
  reads "… saved this invoice". Masked (`NoQuery`) and `Hidden` fields
  never appear, and an override's reason shows under it. The form's
  side column holds Details: the id with a copy button, the created and
  updated times, and each move's stamp; the state field and its stamps
  are not form fields. Opened as a drawer, the record wears
  `ui.DrawerBar` (close, its path, copy link, open as page) and its menu drops Copy
  link. With `WithRecordPath`, a relation select holding a value draws
  an open button beside it, linking to that record, when the caller's
  own read of the related entity returns the row.
- **A create screen** (`appUI.Create("invoices")`): the same form,
  starting at each field's `Default`, posting a create to the entity's
  REST base. `?duplicate=<id>` prefills
  from that record minus what a create may not set and what the copy
  would collide on: a `unique` field and the fields of a unique index
  start blank (a relation in a mixed index keeps its value, so an
  invoice copies under its customer with a blank number);
  `?prefill_<field>=<value>` prefills one field — the convention a
  `Where`-pinned list's New link uses, the Related tab's among them. A
  bare `?<field>=` is some other param and prefills nothing.
- **Stats and charts**: `appUI.StatValue` (agg `count` or empty, or
  `sum` of an int, float or decimal field; `where` in the query DSL),
  `GroupBars`, `GroupSlices` and `LineChart` (rows per value of a field,
  in value order, an enum's in its declared order). The database computes
  each over every match (`crud.SumAll`, `GroupCountAll`), so a sum is
  whole and rounded once, from the database's total. Any other agg
  prints "—". On an entity with `AfterList` hooks the stat totals the
  masked rows instead, and past 100,000 rows prints "—" rather than part
  of them; a field with more than 100 values draws no chart. Each logs
  why. A dashboard block reads an entity without a screen of its own.
  `appUI.Count(ctx, entity, where)` is the count alone, formatted, and
  reports false where `StatValue` would print "—", so a nav row or a
  badge draws nothing instead. `appUI.CheckStat(entity, agg, field,
  where, format)` returns why `StatValue` could never compute a spec,
  so a host can refuse a configured stat at boot.
  `appUI.LastUpdated` is when the newest record the caller can read was
  written (the greatest `updated_at` in scope); it reports false on an
  entity without timestamps, a refused read or no rows.

An `Image` field draws a `ui.Thumbnail`: a small one in a list cell, a
large one above the URL input on the record. A URL that
`urlsafe.ImageSource` refuses (a `javascript:` or SVG data URI) draws
no image.

## How it reads the entity

`Display` (`entity.DisplayConfig`, spelled `display:` in a blueprint) is
plain data on the entity; nil means every default falls back to the schema
itself. It names the record (`Singular`, `Plural`, `TitleField`,
`Description`), the list (`Columns`, `Views`, `Facets`, `PageSizes`,
`Card`, `NoDuplicate`, `NoBulk`), the sidebar (`Nav`), the form layout
(`Form`, with rows, sections and a side rail) and per-field hints
(`Fields`: `Label`, `Help`, `Placeholder`, `Locked`, `Omit`, `ShowWhen`,
`Input`). A hint changes how a screen draws a field, never what the API
accepts; the field's own `Hidden`, `ReadOnly` and `NoQuery` keep their
meaning and every screen honours them first.

A form input carries the field's own validators, so the browser refuses a
bad value before the round trip: `Required` sets `required`; a string's
`Min` and `Max` set `minlength` and `maxlength` (a fractional minimum
rounds up) and a number's set `min` and `max`; `Pattern` sets
`pattern`, wrapped as `[\s\S]*(?:<pattern>)[\s\S]*` because the
server's check is an unanchored match and the browser anchors the
attribute. A pattern using Go-only syntax (inline flags such as `(?i)`)
stays off the input and the server alone checks it. The server still
validates every write; the attributes only move the first refusal
earlier.

`States` (`entity.StatesConfig`) gives the record its moves. The state
field and every stamp render read-only on every screen; a button per
transition posts the entity's transition route
(`POST <api>/<entity>/<id>/transitions/<key>`), whose success re-fetches
the page. A `System: true` move draws no button — only Go code calls it.
A move's `Permission` gates its button with the same exact resource check
the route runs: the caller's roles must grant it by name, and a
`Wildcard` grant does not, so no button is drawn that the route would
refuse.

Every chrome string (headings, buttons, tab names, notices) resolves
through `framework/i18nui` keys, so a catalog entry translates the screens
without touching the entity. The list's count reads as a sentence ("11
customers"): `i18nui.EntityNoun` lowercases the English name, keeps an
acronym ("API keys"), and uses a catalog's `entity.<entity>.plural` as
written.

## Building a screen

```go
<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/entityui"
var fwApp *framework.App
var ctx = context.Context(nil)
-->
appUI := fwApp.EntityUI(entityui.Extensions{})
list := appUI.List("invoices").
	Columns("number", "amount").   // Display.Columns when unset
	PageSize(20).                  // capped by Pagination.MaxListLimit
	RenderCtx(ctx)                 // a component; place it anywhere
record := appUI.Record("invoices", "inv-42").Delete().RenderCtx(ctx)
_, _ = list, record
```

The list builder also takes `Key` (namespaces its query params when two
lists share a page), `View` (the view that opens when the URL names
none), `As("cards")`, `Where(field, value)` (pins a term inside the
caller's scope — a tab listing one invoice's payments; the pinned field leaves the default columns and the facets, and New carries it as `?prefill_<field>=`), `Base` (where
record links hang off), `Heading(text, level)` (it also names the table,
as a hidden caption, so two lists on one page are two named regions) and
`Empty(text)`,
`NoCreate`, `NoLinks` (rows with no record links, no row menu and no New,
for an entity with no screen of its own), `Delete`, `Duplicate`, `Bulk`,
`QueryBox`, `ColumnsMenu`, `Deleted` and `SavedViews` (the four list
controls below), and `Actions` for header buttons beside New.

`Empty(text)` replaces the description of an empty list's state, which
offers New. A list narrowed to nothing draws its own state instead,
without New: a search, typed filter, facet or saved view that matches
no row reads "No {entity} match" with a "Clear search and filters"
link (the view and columns stay, the sort resets), and a view with no
rows reads "No {entity} in this view".

The record builder takes `Base`, `Form` (replaces `Display.Form` on this
page), `Omit(fields...)`, `Tab(key, build)` for a page-local tab,
`Related(entities...)` for the Related tab's lists, `RelatedAt(entity,
base)` for one whose screens live elsewhere (an empty base draws it with
`NoLinks`), `Activity()`, `API()`, `Override()`, `Delete()`, `Duplicate()`
and `Prefill(values)`.
A related list's heading sits one level below the record's title, so the
page keeps one `<h1>`.

A builder name that is wrong — an unknown entity, a bad `As`, an unknown
column — fails that slot with a generic message and a log line, never a
failed page.

## Naming records and pointing writes elsewhere

`appUI.RecordTitle(ctx, entity, id)` answers the name a record's heading
shows, for a breadcrumb or a link drawn outside its screen.
`appUI.SearchRecords(ctx, entity, q, limit)` answers up to `limit`
records (at most 20) whose `SearchFields` match `q`, the way the list's
search box matches, as `entityui.RecordMatch{ID, Title}`, in primary-key
order. The limit counts records the caller may open: a row a Decider
refuses does not use up a place, and the read pages past refused rows for
at most five pages of `limit` rows. Both read behind the same gates as
the screens (scope, sign-in, RBAC, a Decider's per-row answer, the read
hooks) and answer nothing for a record or entity the caller may not see.

`appUI.SnapshotTitle(ctx, entity, row)` names a record from a stored copy
of its values (an audit row's old or new side) the way `RecordTitle`
names a live one, reading nothing, so a deleted record keeps its name; a
masked title field reads as the entity's singular name.
`appUI.Changes(ctx, entity, before, after)` draws what one edit changed
as the Activity tab's change list, "" when no field the caller may see
differs; `before` and `after` are keyed by the API's wire names, the way
the audit log stores them. `appUI.WithActorName(name)` returns a UI whose
Activity tab names each actor by `name(ctx, id)` (the admin passes the
account's email), the id where it answers "" or panics.

`appUI.WithAPIPath(path)` returns a UI with the same screens and
Extensions whose writes (save, delete, moves, bulk, export) post to
`path(e)` instead of the entity's REST routes; `path` answering false
draws that entity read-only. A back office uses it to send writes through
routes it gates itself: `battery/admin` mounts the CRUD handler's write
routes under `/admin/api/<entity>` this way. Reads are unchanged.

`appUI.WithRecordPath(path)` returns a UI whose list cells draw a
relation's title as a chip linking to `path(e) + "/" + id`, the related
record's screen. A link is drawn only for a title the caller's own read
of the related entity returned; a refused relation stays muted, an id
the read did not return stays text, and `path` answering false leaves
that entity's titles as text. The admin points it at
`/admin/entities/<entity>` for the entities it exposes. An enum cell
draws its badge with a dot.

## Extensions

`Extensions` is the code an app registers next to its screens: the entity
says what to show, extensions draw or act.

Extension code runs as the caller. A back office that elevates its own
reads and writes (`crud.WithElevation`, as battery/admin does) does not
vouch for an action's `Run`, a tab's `Build`, a view func, a field kind
or a replaced list or record body: each receives the caller's context
with the elevation removed (`crud.WithoutElevation`), and the component
it returns draws with that context too, so it passes only
the read and write gates the caller's own roles pass.

```go
<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core/render"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/entityui"
import "github.com/DonaldMurillo/gofastr/framework/filter"
var fwApp *framework.App
-->
appUI := fwApp.EntityUI(entityui.Extensions{
	Kinds: map[string]entityui.Kind{
		"slug": {Input: func(ic entityui.InputContext) render.HTML {
			return render.HTML("") // draw the input for ic.Field
		}},
	},
	Entities: map[string]entityui.Extension{
		"invoices": {
			Views: map[string]entityui.ViewFunc{
				"overdue": {Filter: func(context.Context) (*filter.Predicate, error) {
					return &filter.Predicate{Field: "status", Op: filter.OpEq, Value: "past_due"}, nil
				}},
			},
		},
	},
})
```

- **Kinds** draw one field kind: `Input` on forms, `Cell` in list cells
  and cards, `Detail` read-only (`Cell` when nil). `Display.Fields[f].Input`
  picks one by name. `email`, `url`, `color`, `markdown` and `code` are
  built in, and so is `money` for an Int, Float or Decimal: a number input
  behind the currency symbol, and the value printed as an amount (`$1,234.50`,
  `-$5.00`) in cells and read-only. The symbol is the
  `ui.entity.currency` catalog entry (`$` by default), which the
  `format: money` stat reads too. A built-in kind on a field type it does
  not fit (`money` on a String) fails registration. An app kind of the
  same name replaces a built-in one, and its fit is the app's to judge. A
  locked field drawn by a kind keeps its label above the kind's `Detail`.
  `Cell` and `Detail` get the row after the read hooks, so a column a hook
  masks stays masked in them, and a relation whose target the caller may
  not read draws muted without calling them. Only `Input` gets the stored
  value, since a form prefills from it. `InputContext.Label` and `Help`
  carry the words the form resolved (catalog entry, else the Display
  hint, else the humanized name), so a kind labels its field the way a
  built-in field would.
- **Views** bind a func to a `Display.Views` key, for a filter that
  depends on who is looking, the tenant or the clock. The URL carries the
  key (`?view=overdue`), never the predicate. The func's predicate passes
  `filter.ValidatePredicate` before it reaches SQL, every time — field
  names are spliced into WHERE clauses, so a func's own tree gets the
  same validation URL input gets. `Show` hides the view from callers it
  does not apply to; a hidden default view falls back to All. When a list
  falls to a view with no `?view=` (a `Default` view, or the builder's
  `View`), its All tab links `?view=all`, the reserved key. A view with
  neither a `Where` nor a registered func fails at boot, and so does a
  func registered for a key no view declares.
- **Tabs** add a record tab after the built-in ones. `Build` runs inside
  a recover: a panicking tab fails that tab alone.
- **Actions** add record header buttons and, with `Bulk`, list bulk
  actions. A record button posts to the entity's `_bulk` route with scope
  `record` and the one id, so it runs through the same re-read, gates and
  audit row as a bulk run; it answers 200 when the action ran, 403 when
  the record's gates skipped it and 500 when `Run` failed. It shows only
  to a caller who may run it on that record, in its `Variant`
  (`ui.ButtonSecondary` when empty; `New` refuses a variant no Button
  knows). `Permission`, when set, is checked against the caller's own
  roles on top of the entity's update access; a `Wildcard` grant does not
  satisfy it. `Run` receives the resolved selection and a CRUD handle
  scoped to the caller. Up to `InRequestCap` (100) records run inside the
  request; a larger selection needs `Extensions.Jobs` (the admin backs it
  with `battery/queue`), and without one it is refused naming the cap.
  "Every match" resolves at most `EveryMatchCap` (10,000) records.
- **List and Record** replace an entity's list or record body with the
  app's own component, under the same read gates and route.

## The read gates

Route middleware never runs for a component render, so the checks the
JSON API applies live in the builders themselves:

- The list runs `CanReadScoped` before any read; the record also runs
  `CanReadRecordScoped`, so a resource-aware Decider that allows the
  listing and denies one row answers not-found, byte-identical to a
  missing id.
- Every read of a DIFFERENT entity — relation cell labels (one IN read
  per relation column), the create form's relation picker, a relation
  facet's options, the Related tab's lists, the stats and charts — passes
  that entity's own gate first.
- A relation the caller may not read renders muted (an em dash), never
  the related record's name and never the raw foreign key. A refused
  relation facet draws no options, so the facet is absent. A refused
  related list draws its notice, never its rows. A refused stat prints
  the em dash: an aggregate never announces an entity it cannot read.

A builder never draws a write the API would refuse the caller. New,
Duplicate and the create screen need the entity's create access
(`CanCreateScoped`); the edit form and the state moves need update
access to the record (`CanUpdateRecordScoped`), and a move also its own
`Permission` by name; Delete needs delete access to the record
(`CanDeleteRecordScoped`). A caller who lacks one sees the record's
values without that control, and the create screen draws the
not-available notice. An entity with no REST write routes renders
read-only for everyone: values, moves and deletes gone, nothing
submittable.

## State in the URL, writes as form RPCs

A list keeps its state in the page's own query string: `sort`, `dir`,
`page`, `q`, `filter` (DSL text), `view`, `cols` (the shown columns, in
order), `saved` (an open saved view's id) and `f_<field>` facets, each
prefixed by the list's key when it has one (`due_sort` for
`.Key("due")`). Sort headers, pager links and view tabs are plain
anchors the client router intercepts; the toolbar is one GET form whose
hidden inputs round-trip the state it does not own. There is no island
to mount and no second route onto the rows: every request passes the
page's own route gate. A page out of range lands on the last page; a
failed count leaves the reader on the page asked for with no pager.

The record's tabs are the same shape (`?tab=related`). Writes — save,
delete, a state move — are form RPCs (`data-cui-rpc`) to the entity's
REST routes whose answer re-fetches the page
(`data-cui-rpc-navigate`), carrying a toast and, on a refusal, the
runtime's error toast. The edit form's inputs prefill from the unhooked
read so they round-trip; read-only values show what an `AfterGet`
redaction shows.

## Query box, columns menu, trash view, saved views

Four list controls are off by default on app pages and on in the admin.
Each is one builder method, and each keeps its state in the page's own
query string like the rest of the list. The list draws one toolbar
form: the search, a Filters dropdown (the facets, the query box and
the one Apply/Reset pair, with a badge counting the filters set) and
the Columns menu at the row's end. Saved views are tabs beside the
declared views, and the "Save view" dropdown sits at the tab row's
end. Active filters show as chips under the toolbar, each linking to
the list without it, plus a "Clear all".

```go
<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/entityui"
var fwApp *framework.App
var ctx = context.Context(nil)
-->
list := fwApp.EntityUI(entityui.Extensions{}).
	List("invoices").
	QueryBox().      // the filter typed by hand
	ColumnsMenu().   // show, hide, reset
	Deleted().       // the trash view, ?view=deleted
	SavedViews().    // the caller's named views, ?saved=<id>
	TabCounts()      // a row count on each view tab
_ = list.RenderCtx(ctx)
```

- **An embedded list** (`.Embedded()`) is one section of another
  screen, the way a record's Related tab draws its lists: a compact
  header holding the heading, the row count and a small "Add
  <singular>" button, then the rows with no view tabs, search, filters
  or bulk selection, and a one-line empty state when there are none.
  Sort and pager stay, keyed as ever.

- **A preview** (`.Top(n)`) shows the first `n` rows in the view's own
  order, with no pager, sort controls or row menu (each row still links
  to its record), ignoring the URL's page and sort params. It is for a
  list whose full form lives on another screen: the admin's Needs
  attention panel draws `.Embedded().Top(5)` lists linking to theirs.

- **The query box** (`.QueryBox()`) is where the reader types the
  filter: a labelled text field named the list's `filter` param,
  prefilled with the active filter text, helped by the entity's
  queryable field names, inside the Filters dropdown of the toolbar
  form, which round-trips the state it does not own. The server parses the text with the same parser the
  chips use, so both stay in sync; text that fails to parse, or names a
  Hidden, `NoQuery` or unknown field, keeps the filter-did-not-apply
  warning and lists without it — never an error page, never SQL.
- **The columns menu** (`.ColumnsMenu()`) is a menu of checkbox rows,
  one per available field, each a link that toggles that field in the
  `cols` param and keeps the filter and the sort, plus a Reset row that
  drops it. The title row is checked and disabled. A `cols` param
  typed by hand still sets the order. Every
  `cols` name must be a visible, non-omitted field; an unknown,
  Hidden, omitted or duplicate name makes the whole param ignored —
  the list's resolved columns stand. The title column carries the
  record link, so it may not be hidden: a `cols` that leaves it out
  gets it back, first. Columns change what a row shows, not which rows
  match, so the page stays; the read asks only for the shown columns.
- **The trash view** (`.Deleted()`, an entity with `Scope.SoftDelete`
  only) adds a Deleted tab beside the views: `?view=deleted` lists only
  soft-deleted rows, under the same owner, tenant and read scope as the
  live list, with no bulk bar, no New, no row menu and no record link
  (the record screens read live rows only). Each row offers Restore
  and Delete permanently, behind a confirm. The two post to the host's
  write base for the entity — `POST <write base>/<id>/_restore` and
  `POST <write base>/<id>/_purge` — served by
  `appUI.RestoreHandler(entity)` and `appUI.PurgeHandler(entity)`,
  which a host mounts the way it mounts `appUI.BulkHandler(entity)`.
  The handlers run the CRUD handler's `RestoreOne` / `PurgeOne` under
  the caller's own context, so permission, the Decider and owner and
  tenant scope are the write route's own gates; a purge of a live row
  answers 409 and touches nothing. A form-RPC caller gets a status and
  a toast; a plain form post gets a 303 back to the Deleted view, along
  a same-origin relative return path the form carried — never an
  absolute URL. Cross-site posts are refused, the body is capped and
  nothing is stored cacheable.
- **Tab counts** (`.TabCounts()`) put a row count on each view tab:
  the rows that tab's link would list, under the page's search, facets,
  filter, `Where` pins and the caller's read scope (owner, tenant,
  read hooks). A saved view's tab counts its own filter, a Deleted tab
  the trashed rows. The open tab reuses the page's own count; every
  other tab is one COUNT. A refused count leaves its tab bare. With
  the counts on the tabs, the page header shows the entity's
  `Display.Description` in place of the "N invoices" line, when it has
  one.
- **Saved views** (`.SavedViews()`, when the UI carries a store —
  `appUI.WithSavedViews(store)`) keep a named filter-and-columns state
  per caller, in a `SavedViewStore` the host backs with a table
  (`battery/admin` provides it). `?saved=<id>` opens one: its filter
  and columns apply as if they were the `filter` and `cols` params,
  with an explicit param in the URL winning, and both are re-parsed
  and re-checked on every open — a view that no longer applies (or an
  unknown or foreign id) draws a callout and lists the All view,
  revealing nothing. The save form ("Save view", a name; shown once the
  filter or the columns differ from the open view) and the open view's
  delete form post to `POST <write base>/_views` and
  `POST <write base>/_views/_delete/{id}`, served by
  `appUI.SavedViewsHandler(entity)`. The handler re-checks the filter
  and columns against the entity's fields before storing anything;
  owner and tenant come only from the caller's context, which the
  store enforces. A list with a saved view open draws no Export link
  and offers no "every match" bulk scope: the saved narrowing is not
  in the query those routes read, the same rule a `Where`-pinned list
  follows.

A saved view's own columns apply even when the columns menu is off:
they are the saved-view feature's state, not the menu's.

## API tab and status override

`Record("invoices", id).API()` adds the API tab: how to reach this
record from code. It shows the record as JSON, as the REST read under
the caller's context returns it (Hidden columns never appear, an
`AfterGet` mask shows the mask); the entity's own REST path and the
methods its `Exposure` allows, even on a back office whose
`UI.WithAPIPath` moved the writes; the MCP tool names while
`Exposure.MCP` is on (`crud.MCPToolNames`); and a link to `/api/llm.md`.
The methods come from the declaration, so a read-only `App.View` mount
still lists the write methods. The tab reads nothing the record screen
could not.

`Record("invoices", id).Override()` adds a status override: a form in a
disclosure below the tabs, for setting the state field outside the
declared moves (a bad import, a support case). It is drawn only for an
entity with enforced `States` on an app that keeps an audit log, and
only for a caller who may update the record and also holds
`<entity>:override_state`, checked with `access.CanResourceExact`: a
`Wildcard` grant does not satisfy it, and neither does
`crud.WithElevation`. The capability adds to the update permission; it
does not replace it. The form takes a state and a
required reason (at most 500 characters) and asks before it posts.

It posts to `<write base>/{id}/_override`, served by
`UI.OverrideHandler(entity)`, which the host mounts beside its other
write routes. The handler takes POST only, refuses cross-site posts,
caps the body, checks the capability before reading anything, reads the
record under the caller's context and the entity's read permission
(another owner's id answers 404), refuses a caller the update
permission refuses (403), validates the state and the reason, and
writes with `crud.UpdateOne` under `crud.WithStateOverride`. The audit
row (`state_override`, with the reason) comes from crud. An entity no
audit log records answers 409; the write never falls back to an
unaudited one. A write crud refuses answers what the JSON API would: 403,
404, 422 for invalid values, 400 for a hook's refusal. A form RPC gets a
status and a toast; a plain form post gets a 303 to the form's `back`
path, which must be a same-origin relative path.

## Bulk actions and export

`.Bulk()` on a list (on in the admin) adds a select column, a bulk bar and
an Export CSV link. `Display.NoBulk` turns all three off for the entity,
and so does an entity with no REST write routes.

- **The bar** is one form RPC to `POST <api>/<entity>/_bulk`. It names an
  action (Delete, set an enum or bool field, a state move, or an app
  `Actions` entry with `Bulk`) and a scope: the checked rows, the page's
  rows, or every match of the list's query. The bar offers only the
  actions the caller's collection gates allow (a move or an app action
  also needs its `Permission` by name), and asks before it posts. The
  route takes JSON only and answers 415 to anything else, so a
  cross-site form cannot post it.
  Cards have no checkboxes, so they offer no "selected" scope; "every
  match" appears only when the count is known, the list is not pinned by
  `Where`, and more rows match than the page shows.
- **The server resolves the selection itself.** Posted ids are re-read
  through the scoped CRUD handler under the caller's context, so an id
  from another owner or tenant drops out. "Every match" rebuilds the
  list's view (a builder's `View` included), search, filter and facets
  from the posted query, up to `EveryMatchCap`, and the bar posts a
  digest of the ids it offered (`match`): any other set is refused with
  409, even one of the same size, so a row that entered the list after
  it was drawn is never touched. A list past the cap is not offered
  every match. Each record then passes its own update or delete
  gate before the write; a refused record counts as skipped.
- **Every run writes one audit row** (`op: "bulk"`) with the action, the
  count and the done, skipped and failed tallies, when the app has
  `WithAuditLog`. The actor is the audit log's actor.
- **Past `InRequestCap`** the run needs `Extensions.Jobs`. `App.EntityUI`
  then requires `App.DB` and creates two snapshot tables,
  `gofastr_bulk_jobs` and `gofastr_bulk_items`: the job records the
  resolved ids at the moment of the request, and `RunBulkJob` walks them
  in order. A retried job resumes at the first record with no outcome,
  and the first outcome recorded for a record is the one kept. The
  summary row counts every outcome the store holds (`BulkStore.Tally`),
  so a resumed run reports the whole job, and a queued run writes that
  row once, when it finishes.
- **Queued runs are leased.** `RunBulkJob` claims the job for five
  minutes before each chunk, and every outcome and the finish write are
  fenced on that claim: a second worker handed the same job runs nothing
  while the lease is live (`ErrBulkJobBusy` asks its queue to retry), and
  a worker whose lease lapsed cannot write (`ErrBulkLeaseLost`). A worker
  that dies holding the lease blocks the job until it expires. Delivery
  is at least once, so an app `Actions` callback reads
  `ActionContext.Run`, the job id, to make its own side effects
  idempotent.
- **A double submit answers the job already queued.** The job is keyed on
  the action, its input and the resolved ids; while one with that key is
  queued, the second confirm gets its id and count and enqueues nothing.
- **App start resumes and prunes.** A job written but never handed to
  the runner (the process died in between, or `Enqueue` failed) is
  handed over again at `App.Start` once it is a minute old
  (`UI.ResumeBulkJobs`); a refused `Enqueue` marks the job stopped
  instead. Finished jobs older than `entityui.BulkRetention` (30 days)
  are deleted then too (`UI.PruneBulkJobs`). An app that runs for weeks
  schedules both, with cron or its queue.
- **Export** is `GET <api>/<entity>/_export.csv` with the list's
  narrowing (a builder's `View` included) and `_list=<key>`: the file
  holds what the list narrowed to, up
  to `EveryMatchCap`, read through the same scoped handler and read hooks
  the list uses. `NoQuery`, omitted and JSON fields are left out, and a
  cell a spreadsheet would run as a formula is prefixed with a quote. A
  list pinned with `Where` draws no Export link: the route reads the
  query, and a pin is not in it.

Both routes mount on the router the entity's CRUD routes went on, so an
entity registered with `App.GroupEntity` keeps its group's prefix and
middleware, and its screens post to the group's path. Both answer 404 for
an entity with bulk off, except a record action, which `_bulk` still
runs. The router serves the static `_bulk` and
`_export.csv` segments ahead of `/{id}`, so no record id can shadow them.

The screens look an entity up by name (`Registry.Get`), so they draw the
version a name resolves to: the unversioned entity, else the sole
version. Only that entity gets the two routes. A group version that
shares its name with an unversioned entity gets none, and when several
versions share a name and none is unversioned, the name is ambiguous and
no version gets them. A version that owned its name stops answering
(404) once a later `App.Entity` takes the name over. The app's OpenAPI document lists both routes for each entity EntityUI
mounted them on; `openapi.EntityOpenAPIWithBulk` builds that document
outside the app.

## Common mistakes

- **Expecting `Display` to change access.** `Nav.Hide` drops an entity
  from the sidebar, `Omit` drops a field from screens; neither gates
  anything. Exposure, scope and RBAC decide who reads what, and the
  builders enforce them whatever `Display` says.
- **Registering a view func under a key no view declares** (or declaring
  a `Where`-less view with no func). `App.EntityUI` refuses at boot,
  naming the entity and the key.
- **Trusting a view func's predicate.** It passes `ValidatePredicate` on
  every render; a func naming a `Hidden` or unknown field fails its slot
  with the generic message, and the log line carries the detail.
- **Calling `App.EntityUI` twice.** The second call panics: build the
  one `*entityui.UI` at boot and pass it to whatever else draws entity
  screens, the admin included.
- **Two lists on one page with no `Key`.** The second list with a taken
  key fails its slot. Give each list its own key.
- **Expecting `Locked` to protect the column.** `Locked` keeps a field
  out of the screen's submit; the JSON API still writes it. A field the
  API must also refuse belongs in `States`, which the CRUD handler
  enforces.
