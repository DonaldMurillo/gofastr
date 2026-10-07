# Screens, layouts, outlets, and the layout chain

A screen is one route's content. A layout is the chrome around it — the
app shell with the sidebar and header, the marketing site's header and
footer, the docs page's nav column and table of contents. This page
covers the layout primitive (`app.NewLayout`, outlets, fills, route
areas), the route tree that nests layouts (`ScreenGroup`), what the
server renders for each level, and how the client swaps only the part of
the page that changes on navigation.

A layout is an ordinary component tree with three kinds of area:

- **static** — built once per render, never changes while the layout
  stays on screen (the header, the footer, the sidebar column);
- **route area** — stays in place, re-rendered by the server on every
  navigation the layout survives (breadcrumbs, a help-nav's
  current-article mark);
- **outlet** — a named slot a screen or screen group fills with its own
  content (the toolbar actions, the right-hand context panel, the docs
  page's table of contents).

There is no layout constructor in `framework/ui`. Layouts are declared
in `core-ui/app` with the primitive below, composed from the `framework/ui`
components (Sidebar, ContentRow, Container, Breadcrumbs) and the app's
own packages for its header, footer and docs page; the ready-made page
shapes live as examples, not exports. See
[UI composition recipes](ui-composition-recipes.md) for the four shapes
built end to end.

## The primitive: NewLayout

**Use it when** any page shares chrome with other pages — a sidebar app,
a site with a header and footer, a section with its own sub-navigation.
`NewLayout(name, spec, build)` takes a build function that receives
the request context and a `*LayoutTree` addressing the placement points,
and returns ordinary component HTML. Everything in the returned tree is
static chrome except the `l.Primary()`, `l.Place(...)`, and
`l.RouteArea(...)` cells.

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
type HomeScreen struct{}
func (h *HomeScreen) Render() render.HTML { return render.Text("Home") }
func buildSite(ctx context.Context, l *app.LayoutTree) render.HTML {
	return render.Text("chrome") // replaced below
}
-->
```go
site := app.NewApp("myapp")

shell := app.NewLayout("shell", app.LayoutSpec{}, buildSite)
site.SetDefaultLayout(shell)

site.RegisterScreen(app.NewScreen("/", &HomeScreen{}).WithTitle("Home"), nil)
```

The build function's `_ context.Context` parameter is the live request
context (auth-aware chrome reads it); the layout keeps its name, layer
key, and landmark duty. A screen's route content renders where the
build calls `l.Primary()` — layer 0's primary is the page's single
`<main id="main-content">`; an inner layer's primary is the swap target
inside its parent.

**The mistake it prevents:** hand-rolling a shell `<div>` per screen and
copying the header into every page. The layout is declared once; every
screen under it inherits the chrome, and navigation between siblings
never re-renders it.

The real shape of a build function, from `examples/tracker/main.go`
(`buildShell`): static header and sidebar, a crumbs route area, a
toolbar outlet, the primary, an aside outlet:

```go
func buildShell(ctx context.Context, l *app.LayoutTree) render.HTML {
	return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		html.Header(html.HeaderConfig{Banner: true}, topBar),
		ui.ContentRow(ui.ContentRowConfig{
			Viewport: true,
			Sidebar:  nav,
			Toolbar: ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween},
				l.RouteArea("crumbs", crumbsArea), l.Place(toolbar)),
			Aside: l.Place(aside), AsideLabel: "Context",
		}, l.Primary()))
}
```

Read route state only inside a `RouteArea` closure (it receives the
match); the rest of the build is static chrome a partial never
refreshes, and the `layoutfunc` lint flags a `MatchFromContext` read
outside an area (see [Validation and the lint](#validation-containment-and-the-layoutfunc-lint)
below).

## Which slot to use

A build function places four kinds of cell, and a pane that must
survive navigation takes a fifth tool. Pick by who chooses the content:

| You want | Use | Who chooses the content | Re-rendered | Tracker example |
|---|---|---|---|---|
| The page's main content | `l.Primary()` | the screen | every navigation | the issue |
| A region each route may fill differently | an outlet: `l.Place(o)`, filled by `Screen.Fill`, `ScreenGroup.Fill`, or the outlet's `Default` | the screen, else its groups, else the outlet | every navigation | toolbar, activity aside |
| A region derived from the URL, the same code for every route | `l.RouteArea(name, fn)` | the layout, from the match | every navigation | breadcrumbs |
| Chrome that stays put | plain components in the build | the layout | never, while the layer is kept | top bar, sidebar |
| A pane that keeps its DOM and scroll across sibling routes | a group layer: `ScreenGroup` with its own layout | the group's layout | when the group's resolved prefix changes | the project's issue list |

The tracker's toolbar row shows the outlet and the route area side by
side. The crumbs are the same function on every page, so they are a
route area. The buttons belong to the page: `/inbox` fills the toolbar
with `InboxToolbar`, every project page gets `ProjectToolbar` from
`group.Fill`, and an issue page overrides it with `IssueToolbar`. If one
function could compute the buttons from the URL alone, a route area
would do. Because each route picks its own component, the toolbar is an
outlet.

## Outlets and fills

**Use an outlet when** a region of the shell belongs to the route, not
the shell — the page's own action buttons, a per-page context panel,
the docs article's table of contents. One URL can fill several outlets;
a child declares which ones.

Declare typed handles with `NewOutlet` and pass them to the layout's
spec; a screen or group fills one by value:

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
var site *app.App
type InboxScreen struct{}
func (i *InboxScreen) Render() render.HTML { return render.Text("Inbox") }
type InboxToolbar struct{}
func (t *InboxToolbar) Render() render.HTML { return render.Text("Toolbar") }
type HelpPanel struct{}
func (h *HelpPanel) Render() render.HTML { return render.Text("Help") }
var build func(ctx context.Context, l *app.LayoutTree) render.HTML
-->
```go
toolbar := app.NewOutlet("toolbar")
aside := app.NewOutlet("aside", app.OutletOptions{
	Default: &HelpPanel{}, // renders when nothing fills the outlet
})

shell := app.NewLayout("shell", app.LayoutSpec{
	Outlets: []*app.Outlet{toolbar, aside},
}, build)
site.SetDefaultLayout(shell)

site.RegisterScreen(app.NewScreen("/inbox", &InboxScreen{}).
	Fill(toolbar, &InboxToolbar{}), nil)
```

The handle is typed: a mistyped outlet (`shell.Tolbar`) does not
compile, and a fill whose outlet belongs to a layout outside the
screen's chain panics at render, naming the layout and the outlet.
Outlet names are letters, digits, `-`, `_` (the name is a wire
address).

Resolution per outlet, first candidate that loads wins:

1. the screen's own `Fill`,
2. group fills, innermost group to outermost,
3. the outlet's `Default`,
4. the fallback: `FallbackNothing` (the zero value) renders nothing;
   `FallbackNotFound` makes the request answer 404 (below).

A fill component is instantiated and loaded per request exactly like a
screen component: fresh instance, route params via `SetParams`, DI,
`Load`. **Declining:** a fill whose `Load` returns
`app.ErrNoFill` declines and the next candidate is tried — the docs
TOC fill of an article without headings declines and the column
collapses. Every other `Load` error is contained to the outlet (see
below), never the page.

A docs page's owned sheet observes an empty toc outlet with
`:has(> :empty)` (acme's `helpdocs`). The right column disappears when a
navigation clears that cell and returns when another article fills it;
the kept layout needs no script.

`ScreenGroup.Fill(o, c)` declares the fill for every screen of the
group (and its sub-groups) that does not fill the outlet itself — the
project layer's default toolbar.

**The mistake it prevents:** making per-page chrome into separate
screens or layouts. One shell, many fills: sibling navigation swaps the
primary and re-applies every fill, without re-rendering the shell.

### The 404 outlet

`FallbackNotFound` is for a route that must not exist when a value is
missing — `/issues/{n}` where the store has no issue 9999:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core-ui/app"
stmt: _ = issue
-->
```go
issue := app.NewOutlet("issue", app.OutletOptions{Fallback: app.FallbackNotFound})
```

The route registers, but a request where no candidate renders (every
candidate declined with `ErrNoFill`) answers 404 through the app's
not-found page: the primary becomes the not-found body, every other
outlet renders its `Default` or nothing. `NewLayout` refuses
`FallbackNotFound` beside a `Default` — the two disagree about what an
unfilled outlet is. A contained fill failure never triggers the 404; it
degrades the outlet instead.

## Route areas

**Use a route area when** a region of kept chrome must follow the route
— breadcrumbs, a docs nav marking the current article. `l.RouteArea`
runs its function on every render the layer is part of, kept or not:
on a partial the kept layer's area fn still runs and its HTML travels
with the response as a fill.

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
func crumbsArea(ctx context.Context, m app.Match) render.HTML {
	return render.Text(m.Path())
}
var build func(ctx context.Context, l *app.LayoutTree) render.HTML
stmt: _ = shell
-->
```go
shell := app.NewLayout("shell", app.LayoutSpec{
	Areas: []app.AreaSpec{{Name: "crumbs"}},
}, build)
// in the build: l.RouteArea("crumbs", crumbsArea)
```

The fn receives the resolved `app.Match` (`m.Path()`, `m.Param("id")`,
`m.ScreenID()`); this is the one place inside a build where route state
is readable. Areas are not filled by screens: nothing targets them, and
every build runs on every render.

**The mistake it prevents:** reading the match once in the static chrome
— the first navigation leaves stale breadcrumbs and a stale
current-article mark forever.

Chrome that must follow the route *without* a server round-trip (a
highlight that repaints instantly, derived values) uses the route
signals instead — next section.

## Route signals: `route.*` in the browser

A static region can bind to the route through the store's reserved
`route.` slices (`app.Route`): `route.path`, `route.pattern`,
`route.title`, `route.params.<name>`, `route.query.<key>`. The server
stamps the first paint from the resolved route; after a navigation the
runtime merges the new values as ordinary seeds, so a binding in a kept
layer repaints with no request.

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
var ctx context.Context
-->
```go
app.Route.Title.Bind(ctx, "span", nil) // text follows route.title
app.Route.Param("id").BindAttr(ctx, "div", "data-issue", nil)
```

Every route value is text (paths and titles carry user-influenced
characters): `Bind` and `BindAttr` are the offered modes and `BindHTML`
panics on the family. Values derived from the route (breadcrumbs) are
ordinary store state computed server-side on first paint, or a route
area (above) — never a rebuild of the static chrome. App slices cannot
use the `route.` prefix; it is reserved.

**Highlighting the current nav item needs none of this.** The runtime's
active-link sweep marks `aria-current="page"` on every `nav a` whose
href matches the path (exact match; `data-cui-match-prefix` opts a link
into segment-boundary prefix matching, which `ui.Sidebar` emits for
`MatchPath` items), and opens the closest sidebar group or `<details>`
ancestor of the current link. A static sidebar therefore keeps its
scroll, its open groups, and its fresh highlight across navigations —
write the nav once as static chrome and let the sweep own the mark.

## The route tree: ScreenGroup

`ScreenGroup` is the nesting API: a URL prefix plus a layout that wraps
every screen under it. Groups nest inside each other; the whole group
nests under the app default layout.

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
type IntroScreen struct{}
func (i *IntroScreen) Render() render.HTML { return render.Text("Intro") }
type DeployScreen struct{}
func (d *DeployScreen) Render() render.HTML { return render.Text("Deploy") }
var docsNav *app.Layout
var guidesNav *app.Layout
var application *app.App
-->
```go
docs := app.NewScreenGroup("/docs", docsNav)
docs.Screen(app.NewScreen("/docs/intro", &IntroScreen{}), nil)
guides := docs.SubGroup("/docs/guides", guidesNav)
guides.Screen(app.NewScreen("/docs/guides/deploy", &DeployScreen{}), nil)
application.Router.ScreenGroup(docs)
```

`/docs/guides/deploy` renders inside three layers: the app default
(outermost, owns the single `<main>`), the docs layer, the guides
layer. Rules that shape the chain:

- A group's screens inherit the group layout; `group.Screen(s, other)`
  replaces it for that one screen. The group boundary is kept, but its
  layer compares as different, so navigating to it re-renders that
  shell. A screen whose layout is overridden leaves the group's
  layout — the group's fills fail validation for it.
- `SubGroup(prefix, nil)` inherits the parent's layout. The level still
  exists as an addressable marker; it adds no duplicate chrome.
- `Standalone()` on a group suppresses the app default layout for
  everything under it (a back-office shipping its own full shell).
- Drawer/sheet/dialog screens and layout-less pages have no chain.
- Group policies gate every screen under the prefix:
  `app.NewScreenGroup("/dashboard", layout, auth.SessionPolicy())`.

### Param group prefixes: one layer per entity

A group prefix may carry route params. `/projects/{project}` serves
every project with ONE group, and the layer key embeds the resolved
value — `g:/projects/billing/:project` — so navigating between one
project's pages keeps the layer (the issue list, its filter state) and
switching projects re-renders it:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
type IssueScreen struct{}
func (i *IssueScreen) Render() render.HTML { return render.Text("Issue") }
var projectLayer *app.Layout
var site *app.App
-->
```go
group := app.NewScreenGroup("/projects/{project}", projectLayer)
group.Screen(app.NewScreen("/projects/{project}/issues/{n}", &IssueScreen{}), nil)
site.Router.ScreenGroup(group)
```

This is the "list on the left, detail on the right" shape: the project
layer is the list pane, kept across issue pages; each issue page is the
primary. Both `:param` and `{param}` spellings register identically.

### Keying a layer independently of its name

`WithKey` decouples the layer's identity from its name. Use it when one
layout shape must re-render per context while its styling stays the
same: the multilingual shell (same name `docs`, keys `docs-en` /
`docs-es`; the language switch becomes an ordinary cross-chain
navigation) — see [internationalization](i18n.md).

Reach for a distinct name when the shape genuinely differs; reach for
`WithKey` when only the content or context varies. A region that only
some pages show is neither: it is an outlet that only those pages fill.
`examples/acme-site` places an `announcement` outlet in its site shell,
and only the changelog screen fills it (see
[Outlets and fills](#outlets-and-fills)).

## Resolvers: shared values from the route

**Use a resolver when** more than one region needs the same looked-up
value — the screen and the aside both read the project. A typed key
names the resolver and the type it returns; a screen or group
declares the resolver with `key.From`, and guards, `Load`, fills, and
layout builds read it with `key.Get`. One cell per key per request:
the first reader runs it, concurrent readers wait, the value or error
is kept.

<!-- gofastr:compile
import "context"
import "fmt"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
type Project struct{ Slug string }
func projectBySlug(slug string) (Project, bool) { return Project{}, true }
var group *app.ScreenGroup
var ctx context.Context
stmt: _ = p
stmt: _ = err
-->
```go
var ProjectKey = app.NewKey[Project]("project") // a package var

group.Resolve(ProjectKey.From(func(ctx context.Context) (Project, error) {
	m, _ := app.MatchFromContext(ctx)
	p, ok := projectBySlug(m.Param("project"))
	if !ok {
		return Project{}, fmt.Errorf("%s: %w", m.Param("project"), app.ErrNotFound)
	}
	return p, nil
}))
group.Requires(ProjectKey) // eager: runs in the policy phase, before any Load

p, err := ProjectKey.Get(ctx)
```

Keys compare by identity, not by name: two packages that both call a
key "project" never share a value. Declare each key once, as a package
var, and share the var. The name only appears in errors.

Resolvers must be reads. Where an error lands depends on the phase that
first read it: a guard, the screen's `Load`, or an eager `Requires` →
a whole-page outcome (`app.ErrNotFound` renders the not-found page with
status 404, anything else the error page with status 500, no error text
in the body). First read inside a fill, area, or build → contained to
that region and logged. `uihost.WithErrorScreen(&ErrorScreen{})` gives
the 500 page a product surface through the app's layout, like
`examples/tracker`'s.

A `Requires` key with no visible declaration (the resolver sits on a
sibling group, or was never declared) panics at mount
(`Router.ValidateRequires`, called by `uihost.Mount` beside the fill
check) and at the first render of a screen registered later, naming
the group and the screen: the eager check it was meant to add would
otherwise be silently dropped. Declare the resolver on the group
when only some member screens read the value; the others still run it
eagerly (typically resolving to an inert value).

**The mistake it prevents:** each region re-fetching the same record
(the screen, the aside, the toolbar each running the query), or a
missing entity rendering half a page.

## Region guards

A `Policy` can attach to one region instead of the whole screen:
`OutletOptions.Policy`, `AreaSpec.Policy`, or `app.FillPolicy(p)` on one
fill declaration. Every region guard runs in the policy phase, before
any `Load`. The first `Redirect` moves the whole page; `RenderAlt` and
`Block` are recorded and applied to their region alone.

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core-ui/app/decide"
import "github.com/DonaldMurillo/gofastr/core-ui/component"
import "github.com/DonaldMurillo/gofastr/core/render"
type ArchivedNotice struct{}
func (a *ArchivedNotice) Render() render.HTML { return render.Text("Read only") }
func archivedGuard(ctx context.Context) app.Decision {
	return decide.RenderAlt(func() component.Component { return &ArchivedNotice{} })
}
var issue *app.Screen
var aside *app.Outlet
var activity component.Component
-->
```go
issue.Fill(aside, activity, app.FillPolicy(app.PolicyFunc(archivedGuard)))
```

**The mistake it prevents:** a condition that belongs to one region
(the archived project's read-only aside) blocking or replacing the
whole screen.

## Deferred outlets: parallel part requests

**Use `Deferred` when** an outlet's fill is slow but the page should
paint — an activity feed, a suggestions panel. Deferral belongs to the
outlet, never to one fill (a candidate can decline at request time, so
a per-fill flag cannot tell the manifest in advance):

<!-- gofastr:compile
import "time"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/framework/ui"
stmt: _ = aside
-->
```go
aside := app.NewOutlet("aside", app.OutletOptions{
	Deferred: true,
	Loading: &app.Loading{
		Show:  app.LoadingComponent(ui.SkeletonTimeline(ui.SkeletonTimelineConfig{Rows: 3})),
		After: 150 * time.Millisecond,
	},
})
```

On a client navigation the page request skips the outlet's loaders and
ships its loading content in place; the fill arrives as its own request
(one per deferred outlet, visible in DevTools with its own body and
timing) and lands when ready. First loads and static export ignore
deferral and render the fill inline — nothing depends on JavaScript.
A deferred loader must be a read: the part request starts before the
page answer says whether the page redirects. When the two disagree
(session expired, a guard redirected), the part answers 409 and the
runtime reloads the URL as a whole document, once.

## Loading content

**Use `Loading` when** a navigation is slow enough that the old content
lying in place reads wrong. Declare what a region shows while a
navigation that will change it is in flight — per outlet
(`OutletOptions.Loading`), per route area (`AreaSpec.Loading`), and per
screen for the primary slot (`Screen.WithLoading`, with
`ScreenGroup.WithLoading` as the group default):

<!-- gofastr:compile
import "time"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core-ui/component"
import "github.com/DonaldMurillo/gofastr/framework/ui"
type ReportsScreen struct{ component.Component }
var reports *app.Screen
-->
```go
reports.WithLoading(&app.Loading{
	Show:  app.LoadingComponent(ui.SkeletonCard(ui.SkeletonCardConfig{Label: "Loading"})),
	After: 150 * time.Millisecond, // shown only past this wait
	Min:   300 * time.Millisecond,  // held at least this long once shown
})
```

The server renders `Show` once into an inert `<template>` beside the
region, so the browser holds the bytes before the fetch starts. Past
`After` the runtime parks the region's old nodes and clones the
template in; a response faster than `After` paints nothing extra, and a
failed navigation puts the parked nodes back as they were. Loading
content is presentational: no `Load`, no DI, no params. A slow screen
`Load` is the swap slot's loading case; a slow outlet fill is the
outlet's (or a deferred part).

## Transitions

**Use a transition when** a swap should read as motion — the
master-detail slide, a crossfade that avoids a flash of identical
chrome. Typed values on the spec; the framework generates the CSS:

<!-- gofastr:compile
import "context"
import "time"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/uihost"
func build(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }
var site *app.App
var fwApp *framework.App
-->
```go
layout := app.NewLayout("shell", app.LayoutSpec{
	Primary: app.PrimaryConfig{Transition: app.Transition{Name: "page"}},
	Outlets: []*app.Outlet{
		app.NewOutlet("detail", app.OutletOptions{Transition: app.FadeThrough(150 * time.Millisecond)}),
	},
}, build)
site.SetDefaultLayout(layout)
fwApp.Mount(uihost.New(site)) // the host ships the layout's transition CSS
```

Presets: `app.Slide(app.Right, d)` is the master-detail move (the
back direction mirrors automatically), `app.Crossfade(d)` fades both
legs, `app.FadeThrough(d)` runs the legs sequentially — for regions
whose two states carry text that must not ghost over itself.
`app.Instant()` swaps the region in one frame while the page
transitions around it: the fit for a breadcrumb trail, whose root is
the same on every page and would blink under any fade. Nor does it dim while a slow navigation is in flight, as other busy regions do; it keeps `aria-busy` for screen readers. `Transition{Name: "..."}` assigns a snapshot name without
generating keyframes; use a framework preset when the region needs a move.
The host collects every registered layout's generated
`Layout.TransitionCSS()` into app.css; there is nothing to wire. app.css
is composed once, at first render, so register screens and groups before
serving: a layout with transitions that arrives later logs a warning
naming it and ships no transition CSS. Root-wide
presets by name come from `app.ViewTransitionPresetCSS("fade")`. The
runtime wraps the swap in the View Transitions API with the direction
type (`forward`/`back`/`reload`), never animates under
prefers-reduced-motion, and a document declaring none swaps directly as
before.

## Owned styles

**Use one when** a layout, a screen or the whole app needs a look the
kit components don't give it. Write the CSS in a `<name>.style.css`
file beside the Go, run `gofastr generate styles` (see the
[CLI](cli.md)), and hand the generated handle to its owner:

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
type BoardScreen struct{}
func (b *BoardScreen) Render() render.HTML { return render.Text("Board") }
func build(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }
var site *app.App
var ShellStyle, BoardStyle, AppStyle app.OwnedStyle
-->
```go
shell := app.NewLayout("shell", app.LayoutSpec{Style: ShellStyle}, build) // shell.style.css
site.SetDefaultLayout(shell)
site.RegisterScreen(app.NewScreen("/board", &BoardScreen{}).WithStyle(BoardStyle), nil) // board.style.css
site.WithStyle(AppStyle) // app.style.css
```

- `LayoutSpec.Style` scopes the sheet to the layout. The layout root
  carries `data-cui-scope="<name>"`, and the rules reach everything the
  build renders: the chrome, the screens in its primary, the fills in
  its outlets.
- `Screen.WithStyle` scopes the sheet to one screen's content. The
  content is wrapped once, in its `<article>` for an article screen and
  in one plain `<div>` otherwise. The primary cell is never the root,
  because it stays in place while screens swap inside it, and the
  screen's fills render outside the wrapper.
- `App.WithStyle` declares `app.style.css`, the one sheet that covers
  every page and loads on every page. A scoped sheet passed here
  panics, and so does the app sheet passed to a layout or a screen.

A scope stops at a nested owner's content (a styled screen inside a
styled layout, a scoped component) and at markup the framework marks
internal; the nested owner's root element is still in the outer reach.
Inside its reach an owned rule beats a kit rule of equal specificity
whatever order the two sheets loaded in, because a scoped rule wins on
proximity. A scoped sheet loads when its owner's marker renders: in the
first paint's head, and on insertion after a client navigation.

## Validation, containment, and the layoutfunc lint

Rules the code enforces, each failing loudly instead of silently:

- **Mount validation** (`NewLayout` panics): duplicate outlet name
  in one layout; a handle bound to two layouts; `FallbackNotFound`
  beside a `Default`; an invalid view-transition name; both the static
  `Transition` and the per-request `Transitions` set. A mistyped outlet
  is a compile error before any of this.
- **Fill validation** (render-time panic): every `Fill` must name an
  outlet whose owning layout is in the screen's resolved chain,
  compared by pointer identity. A group's fills fail for a member
  screen whose layout the group overrides.
- **Inventory per render**: each build must place its primary and every
  outlet it owns exactly once, name each route area once, and emit no
  `<main>` of its own. The count is taken on the markers in the output,
  so markup copied into two places is caught. A mismatch is a render
  error naming the layout.
- **Containment**: a panicking build renders an empty layer; a failing
  fill (any non-`ErrNoFill` `Load` error, a render panic) is logged
  once, scrubbed, and contained to its outlet — the region renders the
  failing fill's `component.ErrorBoundary` fallback, else the outlet's
  `Default`, else nothing. The page keeps the screen's status; no error
  text reaches the HTML. Fills load concurrently (bounded), so sibling
  fills never serialize; a fill must not depend on another fill's side
  effects.
- **The `layoutfunc` lint** (a type-aware analyzer in the repo's vet
  tool and `gofastr verify`): it flags `app.MatchFromContext`,
  `Match.Param`, `Match.Path`, `route.From`, and
  `app.RequestFromContext` read lexically inside a `LayoutFunc` body
  and outside a `RouteArea` closure — the build's markup is static
  chrome a partial never refreshes, so request-derived chrome goes
  stale after the first navigation. **The fix:** move the read into the
  `RouteArea` closure (it receives the match), bind a `route.*` signal
  for browser-side updates, or make the region an outlet a screen
  fills. Everything outside a build body (a resolver, a screen `Load`,
  a helper) is silent: the lint is about where the read lives.

## What the server renders

Every layer wraps in a div carrying `data-cui-layout="<name>"` and
`data-cui-layout-key="<key>"` (the layer's identity: `l:<identity>` for
a plain layout, `g:<prefix>:<identity>` for a group layer, with
resolved param values substituted into `<prefix>`). The primary cell
carries `data-cui-layout-slot="<key>"` — `<main id="main-content">` at
the root, a div below it. Other outlets render
`data-cui-outlet="<key>#<name>"`, areas `data-cui-area="<key>~<name>"`;
those addresses are derived from the chain, never written by hand.

The route manifest carries each route's chain as the `layouts` array of
those keys, outermost first. On navigation the client finds the deepest
layer shared with the DOM, sends `X-Gofastr-From`, and the server
renders only the layers below it, naming the boundary in
`X-Gofastr-Swap`; the response also carries every non-primary outlet
and route area of the kept layers as fills, so one navigation response
updates every region that follows the route.

## What the client swaps

- Sibling inside a group → only the innermost content cell changes.
  The sidebar keeps its DOM nodes, scroll, and open disclosures.
- Different branch under a shared root → the shared chrome stays; the
  diverging layers re-render.
- Nothing shared → full page fetch, whole shell replaced. Still no
  hard reload.

One difference between routes always overrides the chain comparison:
document-lifetime scripts. A script registered with
`uihost.RegisterDocumentScript(src, scope)` ships only on pages the
scope accepts, tagged `data-cui-doc`; the runtime compares the
destination's set against the live document's tags at every soft-nav
entry, and a difference is a real navigation, never a partial swap,
because removing a script tag does not uninstall what the script
installed (WebMCP tools are the case that made this a boundary).
Back/Forward across an edge loads the destination document fresh.

## Opt-in, and what each opt-in costs

Everything above the plain tree layout is opt-in; the zero value of
every option does nothing:

- **No outlets, no areas** → navigation is the plain partial swap it
  always was; the page ships the core runtime only. The marketing pages
  of `examples/acme-site` declare none of the machinery.
- **Outlets/areas present** → the client sends the fills header and the
  response is an envelope; the envelope module loads on demand. Costs
  the fill DOM: outlet content belongs to one route and is re-applied
  on every navigation; content that must survive sibling navigation
  (a list pane, its scroll) belongs in a group layer, not an outlet.
- **Loading content** (`Loading.Show` set) → an inert template per
  region; the loading module loads on demand.
- **Deferred outlets** → one extra request per deferred outlet per
  navigation, capped in flight; loaders must be reads.
- **Transitions** → generated CSS at the root and a demand module; the
  plain swap is the default.

`route.*` values are seeded only when the render read a route slice or
the chain has a route area.

## Porting from v0.85's fixed-template layouts

The previous release's layout was a fixed template (a header, sidebar,
footer, and content slots declared through builder methods); those
builders are gone. Port by moving the header/sidebar/footer components
into the build function's static chrome and the screen cell to
`l.Primary()`; `ui.Sidebar` is unchanged, the header and footer are
the app's own packages (`ui.SiteHeader`, `ui.SiteFooter` and
`ui.DocLayout` are gone; see [What changed in
v0.86.0](release-0-86.md#layouts-one-primitive-no-preset-frames)), and
the frame is composed from the structural pieces (the page-tall
`ui.Stack`, `ui.ContentRow` for the sidebar row, `ui.Container` for the
centered column):

```go
layout := app.NewLayout("app", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
	nav, _ := component.SafeRenderCtx(ctx, ui.Sidebar(cfg))
	return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		ui.ContentRow(ui.ContentRowConfig{Sidebar: nav}, l.Primary()))
})
site.SetDefaultLayout(layout)
```

### Context columns and list/detail panes

`ui.ContentRowConfig.Aside` adds a labelled context column after main.
`AsideLabel` defaults to "Context". The column has a fixed wide-screen
width and stacks below main on phones. An empty outlet in `Aside` takes
no space; a later navigation can fill it again.

`ContentRowConfig.Toolbar` places an existing `ui.Toolbar` above main
beside the sidebar. It does not create another toolbar component. The
row is a `<section>` landmark named by `ToolbarLabel` ("Toolbar" by
default), so controls placed there sit inside a landmark.
`Viewport: true` confines desktop scrolling to main, sidebar and aside.
Below the breakpoint it returns to document flow, without hiding either
pane — and it reads the header band's height from
`--size-header-height` (`Theme.Layout.HeaderHeight`), so recipes pair it with a page-tall
`ui.Stack{Screen: true}` and an app bar whose height is that token.
`Sticky: true` is the frame for a shell with no header band (the
admin's): the window still scrolls the page, the nav column sticks to
the top at one viewport tall and scrolls its own overflow, and the
`Toolbar` row sticks to the top at every width, painted over the
content beneath it. The client router's scroll restore and fragment
jumps keep working because the window stays the scroller. Sticky and
Viewport are two scroll models; setting both panics.
`PhoneNavFlush: true` drops the nav column's phone separator below the
breakpoint — set it when the sidebar's phone navigation lives outside
the column (a `NativeMobile` sidebar whose drawer trigger,
`ui.SidebarDrawerTrigger`, the app bar hosts), so an empty column
draws no rule.
`Dense: true` gives the frame the compact density on a fine pointer (a
mouse or trackpad): buttons, fields, the toolbar's controls and table
rows take the 36px control height (44px table rows), the smaller gap,
and the small step as the `--ui-control-padding-y` inset. The row also
sets `--spacing-touch-target` to 36px, so search fields, checkboxes,
menus and icon buttons sized from that token follow. A touch
screen keeps the theme's density, so a phone holds its 44px targets.
It is for operator consoles (the admin sets it); a `ui.Themed` scope
inside the row redeclares its own density.
List/detail panes (`ui.ListDetail`) fill whatever column the row's
build gives them, so the frame decides their width. A sidebar and an
aside are two fixed columns beside that content: with both present the
detail pane gets roughly 300px at 1280px. Give list/detail pages the
whole column. In an app frame, leave `Aside` unfilled on screens that
don't need it: an empty aside outlet releases its column. `Viewport:
true` makes the panes scroll on their own instead of the page:

```go
return ui.ContentRow(ui.ContentRowConfig{Sidebar: nav, Viewport: true}, l.Primary())
```

`PaneHost` is different: it manages openable panes and mobile drawers,
not detail routes.

## Prefetch

A route can declare that the client may fetch its content before the
user clicks:

```go
application.Register("/pricing", &PricingScreen{}, nil, app.Preload(app.PreloadHover))
```

`PreloadHover` fetches on link hover or keyboard focus, `PreloadVisible`
when a link to the route scrolls into view, `PreloadEager` at idle
after page load. The prefetched response lives in a small 30-second
cache the router checks before fetching; `ui.InvalidateScreens`
selectors evict it like the screen cache. Prefetch requests carry
`X-Gofastr-Prefetch: 1`, skip session side effects, and are never used
for routes that would open as overlays. Declare it on cheap,
frequently-next routes; a prefetched page with deferred outlets
fetches the page only.

## Scroll and history

The runtime restores scroll position per history entry: Back and
Forward land where the user left, kept panes keep their scroll, and a
reload keeps its position. A history move whose only URL change is
in-page state (a pane or widget deep-link parameter) closes or reopens
that state without refetching the screen. Search and pagination
parameters are screen identity and refetch as before.

Modules that write in-page state into the URL must go through
`__gofastr._pushURL(url)`. A raw `history.pushState` leaves the router
unaware of the change and breaks both behaviors above. Query-only
changes (`history.replaceState` by a filter island) do not activate a
route and do not move `route.query.*`; a filter that wants a routed
query navigates.

## Common mistakes

- **Hand-rolling the shell in every screen.** Declare one
  `NewLayout`, `SetDefaultLayout`, and register screens under it.
  A screen with its own layout renders in that layout alone; only
  `ScreenGroup` screens nest under the default.
- **Reading the match in the build's static chrome.** The markup is
  built once and never refreshed by a partial; the `layoutfunc` lint
  flags it. Use a route area, a `route.*` binding, or an outlet.
- **Putting per-route state in an outlet and expecting it to survive
  navigation.** Outlet fills are re-applied on every navigation;
  persistent panes belong in a group layer (the param group prefix is
  the tool).
- **Filling an outlet from a layout that is not in the screen's
  chain.** It panics at render, naming both; move the screen into the
  group or fill from the right layer.
- **A screen layout override inside a group that the group fills.** The
  override leaves the group's layer, so the group's fills are invalid
  for that screen; fill from the screen or drop the override.
- **`FallbackNotFound` beside a `Default`.** Refused at
  `NewLayout`: an unfilled outlet is either the Default's content
  or the route's 404, never both.
- **A deferred loader that writes.** The part request races the page's
  outcome; deferral is for reads.
- **Prefetching mutating or per-user-expensive routes.** Prefetch is a
  real render on the server for a page the user may never open.
- **Reusing one layout name for two different shapes.** Layer keys
  embed the name; two distinct `*Layout` values named `"docs"` at the
  same depth compare as the same layer, and navigation between them
  keeps the wrong shell. Distinct shape → distinct name; same shape
  varying by context → same name plus `WithKey`.
- **Writing `history.pushState` directly in custom client code.** Use
  `__gofastr._pushURL(url)`; a raw push leaves `currentPath` stale and
  breaks scroll restoration for the entry.
