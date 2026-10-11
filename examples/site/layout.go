package main

// =============================================================================
// Site identity shared by the chrome and the SEO surfaces.
//
// The header and footer themselves are the site's own owned-style
// packages now (examples/site/siteheader, examples/site/sitefooter):
// their markup, their stylesheets, their tokens. main.go's layout wires
// them around the primary slot; see those packages for the contract.

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/examples/site/docpage"
	"github.com/DonaldMurillo/gofastr/examples/site/sitefooter"
	"github.com/DonaldMurillo/gofastr/examples/site/siteheader"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// =============================================================================
// The layout tree. Every page renders inside the default "main" layer
// (header, primary, footer). Sections whose pages share chrome get a
// ScreenGroup with their own layer below it, so a navigation between two
// pages of one section swaps only that layer's primary slot and the
// section chrome (its nav column, its rail) stays in place:
//
//	main                      siteheader · primary · sitefooter
//	├── g:/            hub    reading column + on-this-page rail (toc outlet)
//	│     /primitives /framework /agents /interactivity /generator
//	├── g:/examples/   examples   examples nav column + primary
//	│     /examples /examples/workspace /examples/catalog(/:id)
//	│     /examples/presence /examples/live-dashboard
//	├── g:/plugins/    plugins    registry nav column + primary
//	│     /plugins /plugins/:name
//	├── g:/docs/       docs       docs nav · crumbs outlet · primary ·
//	│     /docs/ /docs/{path...}  pager outlet · rail outlet (docpage.Layer)
//	└── g:/components/ components (components.go)
//
// Everything else (home, get started, kiln, philosophy, seo, reader, the
// headless theme showcases) renders straight in the main layer.
// =============================================================================

// mainLayout is the site frame: the page-tall stack with the sticky banner
// bar directly inside it (siteheader owns the sticky + z-order), the
// primary slot under it, and the colophon closing the page.
func mainLayout() *app.Layout {
	return app.NewLayout("main", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
			siteHeader(ctx),
			l.Primary(),
			siteFooter(),
		)
	})
}

// hubLayout frames the taught area hubs: the wide reading container with a
// content row whose aside holds the toc outlet. The rail sits beside the
// content on wide screens and stacks under it on phones.
//
// The toc outlet is the hubs' on-this-page rail: each hub fills it with its
// own concept list (TeachHubScreen.TOC), and an unfilled outlet releases
// the aside column. An outlet handle binds to exactly one layout, so the
// pair is minted together per setupServer call.
func hubLayout() (*app.Layout, *app.Outlet) {
	hubTOC := app.NewOutlet("toc")
	return app.NewLayout("hub", app.LayoutSpec{Outlets: []*app.Outlet{hubTOC}}, func(_ context.Context, l *app.LayoutTree) render.HTML {
		return ui.Container(ui.ContainerConfig{Width: ui.ContainerWide, Pad: ui.ContainerPadPage},
			ui.ContentRow(ui.ContentRowConfig{
				Aside:      l.Place(hubTOC),
				AsideLabel: "On this page",
			}, l.Primary()),
		)
	}), hubTOC
}

// sectionNavLayout frames a section with a nav column: the content row with
// a SectionMenu (sticky rail on desktop, drawer trigger on phones) beside
// the primary slot. The menu is static chrome; the runtime's active-link
// sweep marks the current page, so the column keeps its scroll across
// sibling navigations.
func sectionNavLayout(name string, menu func() interactive.SectionMenuConfig) *app.Layout {
	return app.NewLayout(name, app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML {
		// The row's column landmark keeps its default name: the menu
		// inside it is its own named nav, and two navs sharing one
		// name fail landmark-unique.
		return ui.ContentRow(ui.ContentRowConfig{Sidebar: interactive.SectionMenu(menu())}, l.Primary())
	})
}

// docsOutlets are the docs layer's per-route regions. An outlet handle
// binds to exactly one layout, so they are minted with it.
type docsOutlets struct {
	// Crumbs sits above the article: each doc page fills its trail; the
	// index leaves it empty and the cell takes no room.
	Crumbs *app.Outlet
	// Pager sits under the article: each doc page fills its
	// previous/next cards.
	Pager *app.Outlet
	// Rail is the right column: the index fills it with its intent rail;
	// a doc page leaves it empty and the column collapses.
	Rail *app.Outlet
}

// docsLayout frames /docs/ and every /docs/<slug> page: the site's owned
// docpage shell around the docs nav, the outlets and the primary slot.
// The nav is static chrome (the whole catalog, ~110 links), so the layer
// keeps it, and its scroll, across doc-to-doc navigations; the runtime's
// active-link sweep marks the current doc. Only the primary and the
// outlet fills travel on a sibling navigation.
func docsLayout() (*app.Layout, docsOutlets) {
	o := docsOutlets{
		Crumbs: app.NewOutlet("crumbs"),
		Pager:  app.NewOutlet("pager"),
		Rail:   app.NewOutlet("rail"),
	}
	return app.NewLayout("docs", app.LayoutSpec{Outlets: []*app.Outlet{o.Crumbs, o.Pager, o.Rail}}, func(_ context.Context, l *app.LayoutTree) render.HTML {
		return docpage.Layer(docpage.LayerConfig{
			Nav:    interactive.SectionMenu(docsSectionMenuConfig()),
			Crumbs: l.Place(o.Crumbs),
			Body:   l.Primary(),
			Pager:  l.Place(o.Pager),
			Rail:   l.Place(o.Rail),
		})
	}), o
}

// examplesSectionMenuConfig is the /examples section nav: the reference-app
// index, the in-site demo apps, and the headless theme showcases. Shared by
// the inline rail and the phone drawer mounted in setupServer.
func examplesSectionMenuConfig() interactive.SectionMenuConfig {
	themes := make([]interactive.SectionItem, 0, 2*len(landingRoutes))
	for _, r := range landingRoutes {
		themes = append(themes, interactive.SectionItem{Label: r.Name + " landing", Href: landingRoutePath(r.Segment)})
	}
	for _, r := range landingRoutes {
		themes = append(themes, interactive.SectionItem{Label: r.Name + " dashboard", Href: dashboardRoutePath(r.Segment)})
	}
	return interactive.SectionMenuConfig{
		AriaLabel:    "Examples navigation",
		TriggerLabel: "Examples",
		DrawerName:   "examples-section-menu",
		Lead:         &interactive.SectionItem{Label: "Reference apps", Href: "/examples"},
		Groups: []interactive.SectionGroup{
			{Label: "In-site apps", Items: []interactive.SectionItem{
				{Label: "Support workspace", Href: "/examples/workspace"},
				{Label: "Catalog (intercepting route)", Href: "/examples/catalog"},
			}},
			{Label: "Live data", Items: []interactive.SectionItem{
				{Label: "Live dashboard", Href: "/examples/live-dashboard?presence=" + liveDashTopic},
				{Label: "Live presence", Href: "/examples/presence?presence=" + presenceDemoTopic},
			}},
			{Label: "Theme layer", Items: themes, Collapsed: true},
		},
	}
}

// pluginsSectionMenuConfig is the /plugins section nav: the registry index
// and one entry per vendored plugin row.
func pluginsSectionMenuConfig() interactive.SectionMenuConfig {
	cfg := interactive.SectionMenuConfig{
		AriaLabel:    "Plugins navigation",
		TriggerLabel: "Plugins",
		DrawerName:   "plugins-section-menu",
		Lead:         &interactive.SectionItem{Label: "All plugins", Href: "/plugins"},
	}
	reg, err := pluginReg()
	if err != nil {
		return cfg
	}
	items := make([]interactive.SectionItem, 0, len(reg.Plugins))
	for _, p := range reg.Plugins {
		items = append(items, interactive.SectionItem{Label: p.Name, Href: "/plugins/" + p.Name})
	}
	cfg.Groups = []interactive.SectionGroup{{Label: "Registry · " + reg.Release.Tag, Items: items}}
	return cfg
}

// groupScreen builds a group member from a component the way App.Register
// does: title, description, and type read from the component's own
// metadata interfaces, then the registration options applied.
func groupScreen(path string, comp component.Component, opts ...app.ScreenOption) *app.Screen {
	s := app.NewScreen(path, comp)
	if t, ok := comp.(app.ScreenTitler); ok {
		s.Title = t.ScreenTitle()
	}
	if d, ok := comp.(app.ScreenDescriber); ok {
		s.Description = d.ScreenDescription()
	}
	if ty, ok := comp.(app.ScreenTyper); ok {
		s.Type = ty.ScreenType()
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// build time from the deployment's git tag via
//
//	-ldflags "-X 'main.siteVersion=$(git describe --tags --abbrev=0 | sed s/^v//)'"
//
// (see scripts/dev-watch.sh, Makefile build-examples, .github/workflows/pages.yml).
// The "dev" fallback is what an un-injected `go build`/`go run` shows locally,
// so the deployed site always matches the tag it was built from, instead of a
// hand-bumped constant drifting behind releases.
var siteVersion = "dev"

// siteOrigin is the public origin the site declares to crawlers and agents:
// og:url, the sitemap's BaseURL, and (via resolveBaseURL) every agent-ready
// discovery URL. It is a variable, not a constant, so a deployment can point
// it at its own hostname at build time via
//
//	-ldflags "-X 'main.siteOrigin=https://example.com'"
//
// The default is the origin the site is actually served from today. It used
// to be hardcoded to a domain that resolves to nothing, which pointed every link
// preview, sitemap entry, and agent card at a dead host, the one class of SEO
// bug that no page-level test catches, because each page was internally
// consistent with the wrong origin.
var siteOrigin = "https://donaldmurillo.github.io/gofastr"

// versionLabel renders siteVersion for display: bare for the local "dev"
// fallback, "v"-prefixed for a real injected release (e.g. "v0.8.0"). Keeps
// the brand badge + aria-label from ever reading the malformed "vdev".
func versionLabel() string {
	if siteVersion == "" || siteVersion == "dev" {
		return siteVersion
	}
	return "v" + siteVersion
}

// siteInstallTarget keeps the public install command reproducible. Deployed
// builds receive the release tag through siteVersion; local source builds
// intentionally point at main rather than pretending @latest is pinned.
func siteInstallTarget() string {
	if siteVersion == "" || siteVersion == "dev" {
		return "main"
	}
	return versionLabel()
}

// siteNavLinks is the primary navigation, shared by the header package's
// desktop nav and phone menu: the seven content sections, each lit on every
// page under its prefix.
func siteNavLinks() []siteheader.Link {
	return []siteheader.Link{
		{Label: "Primitives", Href: "/primitives", Section: true},
		{Label: "Framework", Href: "/framework", Section: true},
		{Label: "Agents", Href: "/agents", Section: true},
		{Label: "Interactivity", Href: "/interactivity", Section: true},
		{Label: "Generator", Href: "/generator", Section: true},
		{Label: "Examples", Href: "/examples", Section: true},
		{Label: "Plugins", Href: "/plugins", Section: true},
	}
}

// siteNavExtraLinks render only in the header's phone menu: the destinations
// a thumb expects first, plus the repo.
func siteNavExtraLinks() []siteheader.Link {
	return []siteheader.Link{
		{Label: "Home", Href: "/"},
		{Label: "Docs (all)", Href: "/docs/", Section: true},
		{Label: "Get started", Href: "/get-started", Section: true},
		{Label: "GitHub ↗", Href: "https://github.com/DonaldMurillo/gofastr", External: true},
	}
}

// siteFooterColumns is the colophon's link columns.
func siteFooterColumns() []sitefooter.Column {
	return []sitefooter.Column{
		{Title: "Read", Links: []sitefooter.Link{
			{Label: "Get started", Href: "/get-started"},
			{Label: "Docs", Href: "/docs/"},
			{Label: "Philosophy", Href: "/philosophy"},
			{Label: "Journal", Href: "https://github.com/DonaldMurillo/gofastr/commits/main", External: true},
		}},
		{Title: "Use", Links: []sitefooter.Link{
			{Label: "Examples", Href: "/examples"},
			{Label: "Plugins", Href: "/plugins"},
			{Label: "Kiln (experimental)", Href: "/kiln"},
			{Label: "CLI", Href: "https://pkg.go.dev/github.com/DonaldMurillo/gofastr/cmd/gofastr", External: true},
		}},
		{Title: "Make", Links: []sitefooter.Link{
			{Label: "Contribute", Href: "https://github.com/DonaldMurillo/gofastr/blob/main/CONTRIBUTING.md", External: true},
			{Label: "RFCs", Href: "https://github.com/DonaldMurillo/gofastr/tree/main/docs", External: true},
			{Label: "Releases", Href: "https://github.com/DonaldMurillo/gofastr/releases", External: true},
		}},
		{Title: "Elsewhere", Links: []sitefooter.Link{
			{Label: "GitHub", Href: "https://github.com/DonaldMurillo/gofastr", External: true},
			{Label: "pkg.go.dev", Href: "https://pkg.go.dev/github.com/DonaldMurillo/gofastr", External: true},
			{Label: "Discussions", Href: "https://github.com/DonaldMurillo/gofastr/discussions", External: true},
		}},
	}
}
