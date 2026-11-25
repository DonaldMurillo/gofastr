# Screens, layouts, outlets, and the layout chain

A **screen** is one route's content. A **layout** is the shared chrome around it—the app shell with a sidebar, header and footer, breadcrumbs, and a docs table of contents. GoFastr has one layout primitive—`core-ui/app.NewLayout`—with outlets, fills, route areas, and nested `ScreenGroup` layers composed from standard `framework/ui` building blocks (`ui.Stack`, `ui.ContentRow`, `ui.Sidebar`, `ui.Container`). On client-side navigation, GoFastr diffs the layout chain and swaps only the layers and slots that changed.

See [UI composition recipes](ui-composition-recipes.md) for complete page shapes.

## The primitive: `app.NewLayout`

`NewLayout(name, spec, build)` renders static chrome around three kinds of dynamic cell: `l.Primary()` (the screen), `l.Place(outlet)` (screen/group-filled slot), and `l.RouteArea(name, fn)` (URL-derived chrome refreshed on every navigation):

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

Layer 0's `l.Primary()` renders the page's single `<main id="main-content">`; inner group layers render swap targets inside their parent.

| Need | Tool | Re-renders when |
|---|---|---|
| Route's main content | `l.Primary()` | Every navigation |
| Per-route slot (toolbar actions, context aside, TOC) | `l.Place(outlet)` + `Screen.Fill` / `ScreenGroup.Fill` | Every navigation |
| URL-derived chrome (breadcrumbs, current counts) | `l.RouteArea(name, fn)` | Every navigation |
| Static chrome (header, footer, sidebar) | Plain components in `build` | Only when the layer changes |
| Persistent pane across sibling routes (list/detail) | `ScreenGroup` with its own layout | Only when the group prefix changes |

Do not read route params or query state in static chrome outside a `RouteArea` closure—the `layoutfunc` analyzer flags stale reads at verify/vet time.

## Outlets, fills & route areas

### Outlets (`app.NewOutlet`)

Declare typed outlet handles on `LayoutSpec.Outlets`, place them with `l.Place(o)`, and fill them from screens or groups:

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
	Default: &HelpPanel{},
})

shell := app.NewLayout("shell", app.LayoutSpec{
	Outlets: []*app.Outlet{toolbar, aside},
}, build)
site.SetDefaultLayout(shell)

site.RegisterScreen(app.NewScreen("/inbox", &InboxScreen{}).
	Fill(toolbar, &InboxToolbar{}), nil)
```

- **Resolution order:** screen `Fill` → innermost-to-outermost `ScreenGroup.Fill` → outlet `Default` → fallback (`FallbackNothing` or `FallbackNotFound`).
- **Declining & containment:** A fill whose `Load` returns `app.ErrNoFill` falls through to the next candidate. Other errors or panics in a fill are logged and contained to that outlet (falling back to `component.ErrorBoundary`, `Default`, or empty) without breaking the page.
- **404 outlet:** Use `FallbackNotFound` (mutually exclusive with `Default`) when an unfilled outlet should turn the request into a 404:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core-ui/app"
stmt: _ = issue
-->
```go
issue := app.NewOutlet("issue", app.OutletOptions{Fallback: app.FallbackNotFound})
```

### Route areas (`l.RouteArea`)

Use a route area for chrome computed uniformly from the current `app.Match` (such as breadcrumbs or sidebar record counts; set `AreaSpec{Inline: true}` for a `<span>` wrapper):

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
// inside build: l.RouteArea("crumbs", crumbsArea)
```

### Client route signals (`app.Route`)

Static chrome can react to route changes client-side with zero server round-trip via reserved `route.*` text signals (`route.path`, `route.pattern`, `route.title`, `route.params.<name>`, `route.query.<key>`):

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
var ctx context.Context
-->
```go
app.Route.Title.Bind(ctx, "span", nil)
app.Route.Param("id").BindAttr(ctx, "div", "data-issue", nil)
```

Note: active nav links need neither route areas nor signals—the runtime automatically sets `aria-current="page"` on matching `nav a` links (with `data-cui-match-prefix` for prefix matches) and opens parent `<details>` groups.

## Nesting layouts with `ScreenGroup`

`ScreenGroup` nests a layout under a URL prefix (inside the app's default layout unless `.Standalone()` is set):

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
guides := docs.SubGroup("guides", guidesNav)
guides.Screen(app.NewScreen("/docs/guides/deploy", &DeployScreen{}), nil)
application.Router.ScreenGroup(docs)
```

- **Parameterized prefixes (master/detail):** Prefix params become part of the layer key so sibling routes inside one project keep the outer list pane and scroll position, while switching projects re-renders it:

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

Typed prefixes such as `/projects/{project:int}` also use the captured parameter value in the layer key.

- Pass `nil` layout to `SubGroup` to inherit the parent layout, attach group-wide policies as extra args to `NewScreenGroup`, and use `Layout.WithKey(...)` when one layout shape needs a distinct cache identity per context (e.g., per locale).
- **Document-lifetime scripts:** Switching between pages whose `uihost.RegisterDocumentScript` sets differ triggers a real document navigation rather than a partial swap so removed scripts don't linger.

## Shared resolvers & region guards

### Resolvers (`app.NewKey[T]`)

When a screen, its fills, and its guards all need the same looked-up record, declare a typed resolver once per request:

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
var ProjectKey = app.NewKey[Project]("project")

group.Resolve(ProjectKey.From(func(ctx context.Context) (Project, error) {
	m, _ := app.MatchFromContext(ctx)
	p, ok := projectBySlug(m.Param("project"))
	if !ok {
		return Project{}, fmt.Errorf("%s: %w", m.Param("project"), app.ErrNotFound)
	}
	return p, nil
}))
group.Requires(ProjectKey) // runs eagerly before Load; ErrNotFound -> 404 page

p, err := ProjectKey.Get(ctx)
```

Errors read during policies, `Requires`, or screen `Load` become whole-page outcomes (`app.ErrNotFound` → 404, other errors → 500 via `uihost.WithErrorScreen`); errors read only inside a fill or area stay contained to that region.

### Region guards

Attach a policy to a single outlet (`OutletOptions.Policy`), area (`AreaSpec.Policy`), or fill (`app.FillPolicy`):

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

`RenderAlt` and `Block` apply to that region alone; `Redirect` and `decide.NotFound()` apply to the whole page.
A blocked outlet loads only its `Default`, when present. `ErrNoFill` leaves the outlet empty, and a default load error goes through the outlet's usual error handling.

## Deferred outlets, loading states, transitions & owned styles

### Deferred outlets

Slow, read-only fills (activity feeds, recommendations) can stream in parallel after the main page paints on client navigations (first loads and static exports still render inline):

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

### Loading placeholders

Show a skeleton during slow navigations on a screen (`Screen.WithLoading`), group (`ScreenGroup.WithLoading`), outlet, or route area. Use `app.NewContextComponent` when the placeholder needs the reader's locale:

<!-- gofastr:compile
import "context"
import "time"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core-ui/component"
import "github.com/DonaldMurillo/gofastr/core/render"
import "github.com/DonaldMurillo/gofastr/framework/ui"
type ReportsScreen struct{ component.Component }
var reports *app.Screen
-->
```go
reports.WithLoading(&app.Loading{
	Show: app.NewContextComponent(func(ctx context.Context) render.HTML {
		return ui.SkeletonCard(ui.SkeletonCardConfig{Ctx: ctx})
	}),
	After: 150 * time.Millisecond,
	Min:   300 * time.Millisecond,
})
```

### View transitions

Declare typed transitions on `Primary` or outlets (`app.Slide(app.Right, d)`, `app.Crossfade(d)`, `app.FadeThrough(d)`, `app.Instant()`, or root `app.ViewTransitionPresetCSS("fade")`). Animations respect `prefers-reduced-motion` and are skipped gracefully if unsupported:

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
fwApp.Mount(uihost.New(site))
```

### Owned styles (`*.style.css`)

Attach generated CSS handles from `gofastr gen styles` to a layout, screen, or the whole app:

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
shell := app.NewLayout("shell", app.LayoutSpec{Style: ShellStyle}, build)
site.SetDefaultLayout(shell)
site.RegisterScreen(app.NewScreen("/board", &BoardScreen{}).WithStyle(BoardStyle), nil)
site.WithStyle(AppStyle)
```

Scoped sheets stop at nested owners and load automatically when their owner renders.

## Sidebar, header & shell building blocks (`ui.Stack`, `ui.ContentRow`, phone sheets)

Compose page frames from `framework/ui` structural components:

- **`ui.Stack{Screen: true, StickyHeader: true}`:** Makes the page at least viewport-tall, pins its `<header>` child, publishes `--ui-sticky-top` so sticky rails/toolbars/TOCs sit below it, and adds focus scroll margins (WCAG 2.4.11). Put the header *inside* the `Screen: true` stack as its first child.
- **`ui.ContentRow`:** Combines `Sidebar`, `Toolbar`, `Aside`, and `l.Primary()`. Pick one scroll model (mutually exclusive):
  - `Viewport: true` — desktop panes scroll independently (good for full-screen workspaces and `ui.ListDetail`).
  - `Sticky: true` — window stays the scroller while sidebar and toolbar stick below `--ui-sticky-top` (good for docs and admin consoles).
  - `Dense: true` — applies compact density (36px controls) on fine pointers (mouse/trackpad) while keeping 44px touch targets on phones.
  - `PhoneNavFlush: true` / `PhoneAsideFlush: true` — removes mobile column separators when navigation/TOC uses a floating `PhoneSheet` or header drawer trigger (`ui.SidebarDrawerTrigger`).
- **Mobile navigation & TOC sheets:** `interactive.SectionMenu` (`FillColumn: true`, `Filter: true`, `SwapAt: interactive.SwapMD`/`SwapLG`, paired with `SectionMenuDrawer`/`SectionMenuDrawerFunc`) and `ui.TableOfContents` / `ui.AnchoredRail` (`PhoneSheet: true`, or `PhoneSheetTop: true` when a page already has a bottom pill) fold smoothly into native mobile sheets.

## Prefetch, scroll & history

- **Prefetch:** Pass `app.Preload(app.PreloadHover | app.PreloadVisible | app.PreloadEager)` to `Register` for cheap, frequently visited read routes (30s cache, evicted by `ui.InvalidateScreens`, expires with the session).
- **Scroll & history:** Back/Forward restores scroll per history entry (anchor-first on pages with outlets) and replays in-page pane/widget deep links without refetching. Custom JS that pushes URLs must call `__gofastr._pushURL(url)` instead of raw `history.pushState`.

## Common mistakes

- **Hand-rolling page shells in every screen.** Declare one `NewLayout` + `SetDefaultLayout` so sibling navigations never re-render shared chrome.
- **Reading route state in static chrome.** The `layoutfunc` lint flags it because partials don't re-run static chrome; use `RouteArea`, `app.Route.*`, or an outlet.
- **Putting persistent panes in an outlet.** Outlets re-render every navigation; persistent panes (like a kept project list) belong in a `ScreenGroup` layer.
- **Filling an outlet outside the screen's layout chain, or overriding a group's layout on a screen that relies on group fills.** Both fail validation at render time.
- **Setting both `FallbackNotFound` and `Default` on an outlet.** Refused by `NewLayout`.
- **Running mutations in a deferred outlet loader or prefetching expensive/mutating routes.** Both are for read-only loads.
- **Reusing one layout name for two distinct shapes.** Layer keys embed the name—use distinct names for distinct shapes, or `WithKey` for context variants of the same shape.
- **Calling raw `history.pushState`.** Always use `__gofastr._pushURL(url)` so scroll restoration and router state stay in sync.
