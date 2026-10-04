# UI composition recipes

GoFastr's component catalog lists what components exist. This guide covers
which layout pattern fits a given user task. Pick a recipe after filling out
the project's `DESIGN.md`, then build it from framework primitives. Apps ship
no CSS of their own and don't rebuild structural markup that already exists.

These are decision recipes, not templates to copy unchanged. Preserve the
hierarchy and responsive intent; adapt the content and components to the
product.

---

## Page shapes: layouts from the primitive

The four page shapes below are whole-page grammars: which layout owns
which chrome, and what each navigation swaps. They are built from the
layout primitive (`app.NewLayout`, outlets, fills, route areas —
see [layouts](layouts.md)), composed out of `framework/ui` components.
**There is no layout constructor in `framework/ui`** and the framework
ships no ready-made layouts: a page shape is your layout declared with
the primitive, not an export. Real apps, not fixtures, carry each
shape.

### App shell

**Use for:** signed-in product surfaces — "a sidebar app", the admin
back-office, any screen set sharing a top bar, a nav column, a content
region, and per-page tool regions.

One tree layout owns the shell: the top bar, sidebar, and toolbar row
are static chrome; a crumbs area follows the route server-side; a
toolbar outlet and an aside outlet are the page's own regions; the
primary slot is the route's screen.

From `examples/tracker` (`main.go`, `buildSite` + `buildShell`):

<!-- gofastr:compile
import "context"
import "time"
import uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
type InboxScreen struct{}
func (i *InboxScreen) Render() render.HTML { return render.Text("Inbox") }
type InboxToolbar struct{}
func (t *InboxToolbar) Render() render.HTML { return render.Text("Toolbar") }
func buildShell(ctx context.Context, l *uiapp.LayoutTree) render.HTML { return l.Primary() }
var site *uiapp.App
-->
```go
toolbar := uiapp.NewOutlet("toolbar", uiapp.OutletOptions{
	Transition: uiapp.FadeThrough(150 * time.Millisecond),
})
aside := uiapp.NewOutlet("aside", uiapp.OutletOptions{Deferred: true})
shell := uiapp.NewLayout("shell", uiapp.LayoutSpec{
	Outlets: []*uiapp.Outlet{toolbar, aside},
	Areas:   []uiapp.AreaSpec{{Name: "crumbs"}},
}, buildShell)
site.SetDefaultLayout(shell)
site.RegisterScreen(uiapp.NewScreen("/inbox", &InboxScreen{}).
	Fill(toolbar, &InboxToolbar{}), nil)
```

The build function composes the shell rather than recreating its columns:

<!-- gofastr:compile
import "context"
import uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
import "github.com/DonaldMurillo/gofastr/framework/ui"
var toolbar = uiapp.NewOutlet("toolbar")
var aside = uiapp.NewOutlet("aside")
var appBar = render.Text("the app's own bar package renders the banner header")
var nav = render.Text("Project navigation")
func crumbsArea(_ context.Context, m uiapp.Match) render.HTML { return render.Text(m.Path()) }
-->
```go
func buildShell(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
	return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		appBar,
		ui.ContentRow(ui.ContentRowConfig{
			Viewport: true,
			Sidebar:  nav,
			Toolbar: ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween},
				l.RouteArea("crumbs", crumbsArea), l.Place(toolbar)),
			Aside: l.Place(aside), AsideLabel: "Context",
		}, l.Primary()))
}
```

`nav` is the rendered `ui.Sidebar`. `appBar` is the app's own package,
not a framework component: `examples/tracker/appbar` is one Go file
that renders the `html.Header` banner landmark (brand, search,
notifications, theme toggle, the sidebar's drawer trigger) and an
owned style sheet that holds the bar at `--size-header-height`, the
token the row's viewport mode subtracts. `Aside` releases
its column when the outlet is empty. `Viewport` keeps desktop scroll
inside the row's regions and returns to document flow on phones.

**Avoid:** a per-screen header copy, a nav column rebuilt per page
(it loses scroll and open groups), and page-action buttons hardcoded
into the shell. The runtime's active-link sweep marks the current nav
item; nothing in the shell reads the route.

### List / detail

**Use for:** "list on the left, detail on the right" — mail, issues,
orders, any master-detail where the URL addresses the detail and the
list (with its filter and scroll) survives moving between details.

A group layer at a param prefix owns the list; each detail is the
layer's primary. The layer key embeds the resolved value, so one
project's list is kept across its pages and re-rendered when the
entity changes.

From `examples/tracker` (`screens.go`, `newProjectLayout` +
`buildProject`):

<!-- gofastr:compile
import "context"
import uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
type Project struct{ Slug string }
func projectBySlug(ctx context.Context) (Project, error) { return Project{}, nil }
type IssueScreen struct{}
func (i *IssueScreen) Render() render.HTML { return render.Text("Issue") }
func buildProject(ctx context.Context, l *uiapp.LayoutTree) render.HTML { return l.Primary() }
var site *uiapp.App
var resolvedProject *uiapp.Key[Project]
-->
```go
project := uiapp.NewLayout("project", uiapp.LayoutSpec{}, buildProject)
group := uiapp.NewScreenGroup("/projects/{project}", project)
group.Resolve(resolvedProject.From(
	func(ctx context.Context) (Project, error) { return projectBySlug(ctx) }))
group.Screen(uiapp.NewScreen("/projects/{project}/issues/{n}", &IssueScreen{}), nil)
site.Router.ScreenGroup(group)
```

The group build puts the list outside the replaceable primary slot:

<!-- gofastr:compile
import "context"
import "fmt"
import uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
import "github.com/DonaldMurillo/gofastr/framework/ui"
type Issue struct { Number int; Title string }
type Project struct { Slug string; Issues []Issue }
var resolvedProject *uiapp.Key[Project]
-->
```go
func buildProject(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
	p, err := resolvedProject.Get(ctx)
	if err != nil {
		return render.Text("Project unavailable")
	}
	links := make([]render.HTML, 0, len(p.Issues))
	for _, issue := range p.Issues {
		links = append(links, ui.LinkButton(ui.LinkButtonConfig{
			Href: fmt.Sprintf("/projects/%s/issues/%d", p.Slug, issue.Number),
			Label: issue.Title,
		}))
	}
	return ui.ListDetail(ui.ListDetailConfig{
		List: ui.Stack(ui.StackConfig{Gap: ui.GapSM}, links...),
		ListLabel: "Issues", Detail: l.Primary(),
	})
}
```

The list scrolls independently and keeps its DOM across detail navigation.
On phones the detail stacks above the list. Tracker adds a `FilterToolbar`
GET form: Apply filters on the server; issue links retain the filter query.
Tracker opts into `MobileSinglePane: true` and wraps its unselected index
in `ui.ListDetailPlaceholder`. That gives the phone journey: list on the
project index, detail on an issue. `BackHref: "/projects/" + slug` and
`BackLabel: "Back to issues"` add the way back: ListDetail renders the
link at the top of the detail pane, outside the detail content, and shows
it only on a phone with a selected detail. Setting `BackHref` without
`MobileSinglePane` panics.

`ui.ListDetail` fills the column the content row gives it, so the
frame sets the panes' width. Tracker's row uses `Viewport: true` and
leaves the aside outlet unfilled on screens without a context panel
(an empty aside releases its column). With a sidebar, an aside, and no
width opt-in the detail pane gets roughly 300px at 1280px. A
marketing-measure page that needs the panes wide drops the reading
container and lets the row span the page:

```go
return ui.ContentRow(ui.ContentRowConfig{Sidebar: nav, Viewport: true}, l.Primary())
```

**Avoid:** `ui.PaneHost` for routed detail (panes are in-page state;
a shareable URL is a route — hard rule 1) and putting the list pane in
an outlet (outlet content is re-applied per navigation; persistent
panes live in a group layer).

For dense record lists, use `ui.Card` with `Variant: ui.CardRow`: `Heading`
is the short record key, `Description` is its title, and the body holds
status and assignee metadata. It remains one keyboard-focusable link.
Use `DetailListConfig.Inline` for short label/value pairs in a narrow
detail pane; long values wrap in their column. The default still stacks
fields in narrow containers, which suits avatar groups and long values.
`FilterToolbarConfig.Compact` keeps its search and actions on one row down
to an 18rem container, retaining the 44px controls and server-side GET
submission; narrower containers still stack.
`StackConfig.TrimMargins` removes direct-child block margins so `Gap`
alone sets the spacing between description paragraphs.

`PageHeaderConfig.Compact` removes the header's outer padding and divider;
level-two compact headings use the smaller pane title size.
`PageHeaderConfig.Badge` puts status beside the title, wrapping below when
space runs out; keep page-level controls in `Actions`.
`ToolbarConfig.Plain` removes the action group's frame when the shell
already supplies chrome. The shell toolbar owns its gutter and never
grows vertically to fill unused space.

In a sidebar app, the app bar renders `ui.SidebarDrawerTrigger(cfg)`.
Set `cfg.SuppressDrawerTrigger` so the sidebar does not render its own,
and keep `cfg.NativeMobile` for the no-script fallback. The page then has
one phone menu control rather than two, and the sidebar's stylesheet
hides the trigger at widths where the column shows.

### Marketing site

**Use for:** "header and footer" — landing, pricing, changelog: static
chrome around page content, no machinery.

The build places the header, `l.Primary()` and the footer in a
page-tall stack. The header and the footer are the site's own packages,
not framework components. Each is a Go file that composes `core-ui/html`
elements and framework parts, plus an owned style sheet
(`siteheader.style.css`) that `gofastr gen styles` turns into typed
class methods. Every dimension is a theme token: the page measure
(`--size-page-width`, with `--size-page-gutter` outside it) and the bar
height (`--size-header-height`) are built-ins, and a value only the
header needs is the package's own token in `siteheader.tokens.css`,
which the site adds with `Theme.Extend`. The phone menu is
`headless.Disclosure`: it traps focus while open and closes on Escape,
on a link tap and on navigation. [Theming](theming.md) covers owned
sheets and app tokens.

`gofastr generate package siteheader` and `gofastr generate package
sitefooter` copy the canonical packages into the app as owned code
(Go, owned sheet, generated class methods, tests); `examples/acme-site`
keeps customised copies of the same packages as a worked example. The
header has the brand, a nav whose Help link stays lit on every page
under `/help` (`data-cui-match-prefix`), a call to action, a theme
toggle, and a phone menu that works without JavaScript. Its chromium
test checks that the brand and the call to action sit on the page
column at 1440 and 390 px.

Main sits on the same measure with `ui.Container{Width:
ContainerPage}`. The container's `Pad` gives the page its block rhythm:
`ContainerPadPage` pads under the header and above the footer,
`ContainerPadEnd` only above the footer (for a page whose first block
sits right under the header). Without it the first block touches the
header's rule. Acme's screens each bring their own container with
`Pad: ContainerPadEnd`. Acme also places an announcement outlet above
the header; only the changelog fills it, and navigation clears it when
the visitor leaves the changelog.

From `examples/acme-site` (`main.go`, `buildSite` + `buildSiteShell`):

<!-- gofastr:compile
import "context"
import uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/framework/ui"
import "github.com/DonaldMurillo/gofastr/core/render"
func siteHeader(ctx context.Context) render.HTML { return render.Text("header") }
func siteFooter() render.HTML { return render.Text("footer") }
var announce = uiapp.NewOutlet("announce")
-->
```go
func buildSiteShell(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
	// The header is the banner landmark and a direct child of the
	// page-tall stack, so it stays pinned for the whole page.
	return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		l.Place(announce),
		siteHeader(ctx), // siteheader.Render(siteheader.Config{...})
		l.Primary(),
		siteFooter(), // sitefooter.Render(sitefooter.Config{...})
	)
}
```

The theme picks up the packages' tokens once, where the site is built:

```go
site.WithTheme(theme.Default(theme.Overrides{Primary: "#0F766E"}).
	Extend(siteheader.Tokens, helpdocs.Tokens))
```

**Avoid:** a page-owned header or footer wrapper, overriding a framework
component's internals from the site's sheet, and literal sizes in the
sheet where a token belongs (the checker refuses them in owned sheets).
Compose the screen's own rhythm from `ui.Stack` gaps, `ui.Section`, and
`ui.PageHeader` inside main.

### Docs site with a table of contents

A group layout under the section prefix draws the docs page, and the
page is again the site's own package: `gofastr generate package
docpage` copies the canonical one into the app (`examples/acme-site/
helpdocs` is a customised copy of it). Its sheet lays out the nav
rail, the article and the contents rail on the page measure, keeps
the rails sticky under the header, sets the article's reading rhythm,
and draws the previous/next cards. The nav
rail is a route area (the current-article mark is re-derived
server-side on every navigation). Crumbs, toc and pager are outlets each
article fills; the crumbs are `ui.Breadcrumbs`. An article without
headings declines the TOC fill (`ErrNoFill`), and the sheet collapses
the empty column with `:has(> :empty)`, including after client
navigation.

From `examples/acme-site` (`main.go`, `buildSite` + `buildHelpDocs`):

<!-- gofastr:compile
import "context"
import uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core/render"
type ArticleScreen struct{}
func (a *ArticleScreen) Render() render.HTML { return render.Text("Article") }
type ArticleCrumbs struct{}
func (c *ArticleCrumbs) Render() render.HTML { return render.Text("Crumbs") }
type ArticleToc struct{}
func (t *ArticleToc) Render() render.HTML { return render.Text("TOC") }
type ArticlePager struct{}
func (p *ArticlePager) Render() render.HTML { return render.Text("Pager") }
var crumbs = uiapp.NewOutlet("crumbs")
var toc = uiapp.NewOutlet("toc")
var pager = uiapp.NewOutlet("pager")
func buildHelpDocs(ctx context.Context, l *uiapp.LayoutTree) render.HTML { return l.Primary() }
var site *uiapp.App
-->
```go
docs := uiapp.NewLayout("docs", uiapp.LayoutSpec{
	Outlets: []*uiapp.Outlet{crumbs, toc, pager},
}, buildHelpDocs)
group := uiapp.NewScreenGroup("/help", docs)
group.Screen(uiapp.NewScreen("/help/{slug}", &ArticleScreen{}).
	Fill(crumbs, &ArticleCrumbs{}).
	Fill(toc, &ArticleToc{}).
	Fill(pager, &ArticlePager{}), nil)
site.Router.ScreenGroup(group)
```

`buildHelpDocs` hands the layout's parts to the package:

```go
func buildHelpDocs(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
	return helpdocs.Render(helpdocs.Config{
		Nav: l.RouteArea("helpnav", func(ctx context.Context, m uiapp.Match) render.HTML {
			return helpNav(ctx, m.Path())
		}),
		Crumbs: l.Place(docsCrumbs),
		Body:   l.Primary(),
		Pager:  l.Place(docsPager),
		Toc:    l.Place(docsToc),
	})
}
```

Use `SidebarConfig.Compact` for article navigation: short rows and a thin
current-item marker instead of the application sidebar's filled item.
Groups remain optional; use them only when the articles need categories.
On phones the compact rail shows `NavLabel` on a full-width drawer trigger;
the drawer links retain 44px touch targets.

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core-ui/component"
import "github.com/DonaldMurillo/gofastr/core/render"
import "github.com/DonaldMurillo/gofastr/framework/ui"
-->
```go
func helpNav(path string) render.HTML {
	nav, err := component.SafeRenderCtx(context.Background(),
		ui.Sidebar(ui.SidebarConfig{
			NavLabel: "Browse the help", CurrentPath: path,
			DrawerName: "help-nav", DrawerTitle: "Browse the help",
			NativeMobile: true, Compact: true,
			Items: []ui.SidebarItem{
				{Label: "Help center", Href: "/help"},
				{Label: "Articles", Open: true, Children: []ui.SidebarItem{
					{Label: "Projects", Href: "/help/projects"},
					{Label: "Issues", Href: "/help/issues"},
				}},
			},
		}))
	if err != nil {
		return render.Text("Navigation unavailable")
	}
	return nav
}
```

The article's rhythm (heading margins, a reading measure for paragraphs
from the package's own `--size-prose-measure` token) lives in
`helpdocs.style.css` under `.article`, so screens render article blocks
directly instead of adding a `Stack` gap on top.
`PageHeaderConfig.Compact` removes the article title's divider. The
docs page owns the full rail-and-article width: give it a plain main,
not a reading container, so the rails span the page.

Use `SectionConfig.Compact` inside a `Stack` when the stack, not the
sections' margins, should set the release-to-release gap. An
announcement above the site header uses `BannerConfig.Strip`:
full-width, square corners, and wrapping inline copy. Ordinary banners
remain bordered status boxes.

Mount the same Sidebar config with `ui.MountSidebar` once at boot.
`NativeMobile` supplies a native disclosure when scripting is disabled;
scripted browsers use the drawer. The route area refreshes `CurrentPath`.

**Avoid:** marking the current article in static nav chrome (stale
after the first click — the nav is a route area or the sweep's, never
a first-render mark), and a TOC that renders for articles without
headings (decline the fill; collapse the column).

---

## 1. Command center

**Use for:** incident response, monitoring, operations, fulfillment, or any
screen organized around an urgent current state and the next decision.

**Density:** compact. **Dominant element:** one `ui.RecordSummary` containing
the active state and its next decision. **Avoid:** an equal-weight stat-card
row, a separate Banner that repeats the same state, and a Card around every
fact.

Compose:

```go
summary := ui.RecordSummary(ui.RecordSummaryConfig{
    Eyebrow:     "INC-2841 · Payments",
    Title:       "Checkout latency is elevated",
    Description: "Card authorizations are slower while the team validates the mitigation.",
    Status:      ui.StatusBadge(ui.StatusBadgeConfig{Label: "SEV-1 active", Variant: ui.StatusDanger}),
    Highlight: ui.Callout(
        ui.CalloutConfig{Title: "Next decision · 14:30 UTC", Variant: ui.StatusWarning},
        render.Text("Rollback if authorization latency remains above 800 ms."),
    ),
    Metrics: ui.MetricBand(ui.MetricBandConfig{Items: []ui.MetricBandItem{
        {Label: "Impact", Value: "32%", Hint: "checkout requests"},
        {Label: "Started", Value: "13:42 UTC", Hint: "32 min ago"},
        {Label: "p95 latency", Value: "1.4 s", Hint: "down from 1.6 s"},
        {Label: "Services", Value: "3", Hint: "2 recovering"},
    }}),
    Aside: ui.Stack(ui.StackConfig{Gap: ui.GapSM},
        ui.Muted(render.Text("Live bridge · Mina leads")),
        ui.AvatarGroup(ui.AvatarGroupConfig{
            Avatars: responderAvatars,
            Max: 3, Label: "Responders on the live bridge", ShowNames: true,
        }),
    ),
    Footer:  ui.Muted(render.Text("Commander: Mina Chen · Updated 2 min ago")),
    Actions: ui.LinkButton(ui.LinkButtonConfig{Label: "Open incident", Href: "/incidents/2841"}),
    Tone:    ui.RecordSummaryToneDanger,
})

ui.Container(ui.ContainerConfig{Width: ui.ContainerWide},
    ui.Stack(ui.StackConfig{Gap: ui.GapLG},
        summary,
        ui.HeroSplit(ui.HeroSplitConfig{
            Ratio: ui.HeroSplitCopyWide,
            Copy:  ui.Timeline(/* current chronology */),
            Media: ui.DetailList(/* ownership and affected services */),
        }),
    ),
)
```

`RecordSummary` controls the heading scale on phones, places its natural-width
`Actions` in the lead region, and gives a concise `Aside` a support rail on
wide canvases. On phones the action leads that support region instead
of falling below the full summary. Keep `Description` to one or two sentences,
keep `Highlight` to one decision plus one short condition, and move the full
narrative later. `MetricBand` stays one compact row on wide viewports and
becomes a two-column signal band on phones; an odd final signal spans the row
instead of stranding an empty quadrant. Use `Hint` for a trend or qualifier
rather than repeating the value. If the site header's identity is long,
give the header package a shorter phone mark.

Let the status and primary path lead. The mobile opening should preserve status
→ concise impact → action → compact live context → next decision → signals
without a second mobile-only summary tree. Full responder lists and archival evidence belong later or in
`ui.Collapsible`; do not shrink them into micro text.

On a wide detail route, put related bounded modules in `ui.Grid` when they
have similar weight: two `DetailList`s, a record and live impact, or metadata
and ownership. Do not stack a narrow table down the left half of a desktop canvas and
leave an accidental empty rail. Let the framework grid reflow the pair to a
single readable column on phones.

**Live proof:** [RecordSummary](/components/recordsummary), [MetricBand](/components/metricband), and [Timeline](/components/timeline) in the component gallery.

---

## 2. Investigation workspace

**Use for:** research, support triage, log exploration, review queues, and
source-plus-annotation tools.

**Density:** compact to balanced. **Dominant element:** the evidence currently
being examined. **Avoid:** independent floating cards for query, source,
excerpt, notes, and synthesis.

Compose the desktop work area with `ui.PaneHost`:

```go
ui.PaneHost(ui.PaneHostConfig{
    Primary:        evidenceReader,
    Secondary:      sourceNavigator,
    SecondaryOpen:  true,
    SecondaryLabel: "Sources",
    Tertiary:       annotationInspector,
    TertiaryOpen:   true,
    TertiaryLabel:  "Annotations",
})
```

Use `ui.Responsive` when the mobile task needs a materially different order.
On a phone, render the reader first and make source/annotation access explicit
with normal routes or framework drawers; do not serialize three desktop panes
into one endless page.

**Live proof:** the full [/examples/workspace](/examples/workspace) route and the [PaneHost](/components/panehost) gallery demonstration.

---

## 3. Split narrative

**Use for:** technical product pages, documentation entry pages, launches, and
content where a concrete artifact should explain the product beside the copy.

**Density:** balanced to spacious. **Dominant element:** a concrete artifact
such as code, terminal output, a diagram, or a product image, not a decorative gradient.
**Avoid:** centered hero → three equal feature cards → centered CTA.

Start with `ui.HeroSplit`; pair concise copy with `ui.CodeBlock`,
`ui.TerminalBlock`, `ui.OptimizedImage`, or a meaningful chart. Continue with
alternating full-width `ui.Section` blocks, changing which side carries the
artifact. Use `ui.Grid` only when the concepts are genuinely peers.

On mobile, put the explanatory copy before the artifact when it establishes
context; put the artifact first when recognition is the primary task. Check
long code lines and actions at 390px rather than assuming the two columns will
collapse cleanly.

**Live proof:** [HeroSplit](/components/herosplit), [CodeBlock](/components/codeblock), and [TerminalBlock](/components/terminalblock) in the component gallery.

---

## 4. Marketplace browse and detail

**Use for:** products, media libraries, creator work, property, or any domain
where visual comparison and trust details drive selection.

**Density:** balanced. **Dominant element:** real item media on browse; the
purchase/selection decision on detail. **Avoid:** forcing every item into the
same text-heavy SaaS Card.

Browse with `ui.FilterToolbar` followed by `ui.Gallery` (grid or masonry) and
use captions for the minimum comparison facts. On detail, use
`ui.HeroSplit{Ratio: ui.HeroSplitMediaWide}` for gallery versus identity,
price, availability, and action; follow it with `ui.DetailList` for provenance
and fulfillment facts.

Mobile detail should keep identity, price, availability, and the primary
action close to the first useful image. Do not make the user traverse a full
desktop gallery before reaching the action.

**Live proof:** [Gallery](/components/gallery), [Lightbox](/components/lightbox), and [FilterToolbar](/components/filtertoolbar) in the component gallery.

---

## 5. Transactional field flow

**Use for:** inspections, setup, checkout, approvals, intake, and tasks that
must be completed accurately under time pressure.

**Density:** compact. **Dominant element:** the current step and its action.
**Avoid:** a dashboard summary above the actual work and a desktop sidebar
that becomes a long preamble on mobile.

Use `ui.ProgressSteps` or `ui.StepRail` for orientation, `ui.Form` and
`ui.FormField` for the active step, `ui.Sticky` for the primary action when the
form is long, and `ui.Callout` only for actionable warnings. Render a separate
mobile composition with `ui.Responsive` when the desktop route/context view
would bury the next job or form.

Keep the first mobile viewport focused: route/job identity, state, required
readings, issue capture, and completion. Secondary history can follow after the
active controls.

**Live proof:** [ProgressSteps](/components/progresssteps), [Form](/components/form), and [Sticky](/components/sticky) in the component gallery.

---

## Choosing without overfitting

1. State the primary user task and the first decision.
2. Pick the closest recipe by task, not by visual fashion.
3. Name the dominant element and the content that can remain secondary.
4. Decide desktop regions and the mobile priority order before implementation.
5. Survey `framework/ui` and `core-ui/app` for the named
   primitives.
6. Render at about 390px and 1440px in light and dark schemes.
7. Identify the three weakest visible decisions and revise them.

If a recipe can't be built without local structural markup or CSS, that's a
missing piece in the design system: write it down and add it there. The app
doesn't get an exception to the no-CSS rule; building it is how you find what's
missing.

## See also

- [Layouts](layouts.md) — the primitive these page shapes are built from.
- [UI capability map](ui-capability-map.md) chooses the state, mutation, delivery, and scaling boundaries before a page recipe.
- [UI components index](ui-new-components.md) lists every constructor and live gallery route.
- [Runtime contract](runtime-contract.md) defines SSR, RPC islands, and SSE.
- [Signal store](signal-store.md) covers typed client projection and derived state.
- [Optimistic UI](optimistic-ui.md) for the mutation lifecycle and how optimistic recipes compose into each page grammar.

## Common mistakes

- Choosing a recipe after already assembling a familiar card grid.
- Treating every region as equally important.
- Using Cards, pills, or elevation as decoration rather than semantics.
- Repeating desktop content in a second "mobile" tree without changing its
  priority.
- Calling a component-valid DOM "visually verified" without screenshots.
- Adding app CSS to approximate a recipe instead of filling an upstream gap.
