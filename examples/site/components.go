package main

// =============================================================================
// /components, the showcase screens.
//
// The catalog itself (the entries, the code snippets, the note-only
// set, the demo support code for the three stateful demos) now lives in
// framework/gallery, so the theme-configuration tool inside cmd/gofastr
// can render every component without importing examples/. This file keeps
// the parts that are genuinely site-local:
//
//   - Backward-compatible aliases (componentCatalog, componentEntry,
//     componentCode, noteOnlyComponents, componentPkg, groupCatalog,
//     categorySlug, initialKanbanColumns, initialOptimisticNotes,
//     sidebarShowcaseConfig, demoSectionMenuConfig, kanbanCard/column,
//     optimisticNote). Tests, demo_state.go, main.go, and
//     components_sidebar.go reference these names directly.
//   - The SSR demo wrappers (kanbanDemo, optimisticCreateDemo,
//     optimisticDeleteDemo) that pull per-visitor session state out of
//     demoState and pass it to gallery's lock-free render helpers. The
//     gallery's own Demo closures render the SEED view; these render the
//     LIVE view for the visitor.
//   - ComponentsIndexScreen and ComponentShowcaseScreen, the two screens
//     registered in main.go.
//
// Both screens compose framework/ui only (PageHeader, Section, Card, Grid,
// Box, CodeBlock) and ship no CSS; the showcase article sits in the site's
// owned docpage shell.
// =============================================================================

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/examples/site/docpage"
	"github.com/DonaldMurillo/gofastr/framework/gallery"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// ── Backward-compatible aliases ─────────────────────────────────────
// These exist so callers in this package (and its tests) keep using the
// names they did when the catalog lived here. They are thin re-exports;
// the gallery package owns the actual data.

type componentEntry = gallery.Entry

// demoState's stateful demo fields reference these types; aliasing them
// here keeps demo_state.go (which we do not own) unchanged.
type kanbanCard = gallery.KanbanCard
type kanbanColumn = gallery.KanbanColumn
type optimisticNote = gallery.OptimisticNote

var (
	componentCatalog = gallery.Catalog

	initialKanbanColumns   = gallery.InitialKanbanColumns
	initialOptimisticNotes = gallery.InitialOptimisticNotes

	sidebarShowcaseConfig = gallery.SidebarShowcaseConfig
	demoCompany           = gallery.DemoCompany

	componentPkg = gallery.PkgForSlug
	categorySlug = gallery.CategorySlug
)

// groupCatalog, demoSectionMenuConfig, and componentGroup live in
// components_sidebar.go, they share the SectionMenu builder with the
// sidebar render. They are thin re-exports of gallery.Grouped(),
// gallery.DemoSectionMenuConfig(), and gallery.Group respectively.
// columnByID looks up a column in this session's board by container id.
// Caller must hold sess.mu.
func (sess *demoState) columnByID(id string) (int, *kanbanColumn) {
	for i := range sess.kanban {
		if sess.kanban[i].ID == id {
			return i, &sess.kanban[i]
		}
	}
	return -1, nil
}

// resetKanbanBoard clears every demo session so the next touch re-seeds.
// Called by the e2e test to guarantee isolation across runs.
func resetKanbanBoard() { resetDemoSessions() }

// resetOptimisticNotes clears every demo session so the next touch re-seeds
// both lists to their initial state. Called by e2e tests for isolation.
func resetOptimisticNotes() { resetDemoSessions() }

// ── Session-aware SSR demo builders ─────────────────────────────────
// The three stateful demos build their full markup via gallery's
// parameterized render helpers, passing the visitor's session data in.
// gallery's own catalog closures call the same helpers with the seed data
// (no request), that's what a theme previewer / static export sees.

// kanbanDemo renders the sortable-kanban demo for the ctx's session.
func kanbanDemo(ctx context.Context) render.HTML {
	sess := demoStateRead(ctx)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return gallery.RenderKanbanBoard(sess.kanban, sess.kanbanVer)
}

// optimisticCreateDemo renders the optimistic-create demo for the ctx's
// session (code block + Add button + the session's current list).
func optimisticCreateDemo(ctx context.Context) render.HTML {
	sess := demoStateRead(ctx)
	sess.mu.Lock()
	html := gallery.RenderOptimisticCreateDemoFor(sess.createNotes)
	sess.mu.Unlock()
	return html
}

// optimisticDeleteDemo renders the optimistic-delete demo for the ctx's
// session (code block + the session's current list + the will-fail trigger).
func optimisticDeleteDemo(ctx context.Context) render.HTML {
	sess := demoStateRead(ctx)
	sess.mu.Lock()
	html := gallery.RenderOptimisticDeleteDemoFor(sess.deleteNotes)
	sess.mu.Unlock()
	return html
}

// =============================================================================
// /components/, the index page listing every catalog entry as a card,
// grouped by category: a ui.PageHeader, then one ui.Section per category
// holding a ui.Grid of linked ui.Cards.
// =============================================================================

type ComponentsIndexScreen struct{}

func (s *ComponentsIndexScreen) ScreenTitle() string { return "Components" }
func (s *ComponentsIndexScreen) ScreenDescription() string {
	return "Every framework/ui constructor, one page each."
}
func (s *ComponentsIndexScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ComponentsIndexScreen) Render() render.HTML {
	// The inner /components/* layout supplies the sidebar (ComponentsSidebar
	// component), this screen is just the overview content cell. Grouped
	// card grid, no rail (the sidebar is the persistent nav).
	groups := groupCatalog()

	head := ui.PageHeader(ui.PageHeaderConfig{
		Eyebrow:  "Components · v" + siteVersion,
		Title:    "Every component, as typed Go.",
		Subtitle: "One page per constructor. Use the sidebar to jump between them; it tracks the page you're on.",
	})

	sections := make([]render.HTML, 0, len(groups))
	for _, g := range groups {
		cards := make([]render.HTML, 0, len(g.Entries))
		for _, c := range g.Entries {
			cards = append(cards, ui.Card(ui.CardConfig{
				Heading:      c.Name,
				HeadingLevel: 3,
				Description:  c.Desc,
				Href:         "/components/" + c.Slug,
				Variant:      ui.CardOutlined,
			}))
		}
		sections = append(sections, ui.Section(ui.SectionConfig{
			Heading:     g.Name,
			Description: itoa(len(g.Entries)) + " constructors",
			ID:          categorySlug(g.Name),
		}, ui.Grid(ui.GridConfig{Min: "16rem", Gap: ui.GapMD}, cards...)))
	}

	return ui.Container(ui.ContainerConfig{Width: ui.ContainerWide, Pad: ui.ContainerPadPage},
		ui.Stack(ui.StackConfig{Gap: ui.Gap2XL}, head, ui.Stack(ui.StackConfig{Gap: ui.Gap2XL}, sections...)))
}

// =============================================================================
// /components/{slug}, single-component showcase page.
// =============================================================================

// ComponentShowcaseScreen implements RenderCtx so the three stateful demos
// (kanban, optimistic create/delete) read the visitor's session off the request
// in ctx and a reload reflects their own state. It ALSO keeps an explicit
// Render() that delegates to RenderCtx with a background context: SSR and
// static export prefer RenderCtx, but the llm.md generator
// (core-ui/app/llmmd.go) calls Component.Render() directly, without this the
// seed-rendering fallback would be an empty component.ContextOnly stub and
// every /components/*/llm.md page would go blank.
type ComponentShowcaseScreen struct {
	Entry componentEntry
}

func (s *ComponentShowcaseScreen) ScreenTitle() string {
	return s.Entry.Name
}
func (s *ComponentShowcaseScreen) ScreenDescription() string  { return s.Entry.Desc }
func (s *ComponentShowcaseScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// Render is the no-request fallback for direct callers (llm.md). It renders the
// seed state for the stateful demos; live SSR uses RenderCtx instead.
func (s *ComponentShowcaseScreen) Render() render.HTML {
	return s.RenderCtx(context.Background())
}

// renderDemo returns the live demo for the entry. The three stateful demos
// get the request ctx so SSR shows the visitor's own session; every other
// demo is stateless and ignores it.
func (s *ComponentShowcaseScreen) renderDemo(ctx context.Context) render.HTML {
	switch s.Entry.Slug {
	case "sortablelist":
		return kanbanDemo(ctx)
	case "optimisticcreate":
		return optimisticCreateDemo(ctx)
	case "optimisticdelete":
		return optimisticDeleteDemo(ctx)
	default:
		return s.Entry.Demo()
	}
}

// demoStage renders the demo in a section with an honest heading: "Live"
// for a real interactive instance, "Note" for a wiring explanation. The
// demo sits in an outlined, padded Box so it reads as a stage apart from
// the page's prose.
func (s *ComponentShowcaseScreen) demoStage(ctx context.Context) render.HTML {
	label := "Live"
	if gallery.IsNoteOnly(s.Entry.Slug) {
		label = "Note"
	}
	return ui.Section(ui.SectionConfig{Heading: label, Compact: true},
		ui.Box(ui.BoxConfig{Pad: ui.BoxPadLG, Outlined: true}, s.renderDemo(ctx)),
	)
}

func (s *ComponentShowcaseScreen) RenderCtx(ctx context.Context) render.HTML {
	pkg := componentPkg(s.Entry.Slug)
	head := ui.PageHeader(ui.PageHeaderConfig{
		Eyebrow:  s.Entry.Category,
		Title:    s.Entry.Name,
		Subtitle: s.Entry.Desc,
		// Real source package, linked to its API docs: the per-component
		// usage/reference link.
		Actions: ui.Link(ui.LinkConfig{
			Href:       "https://pkg.go.dev/github.com/DonaldMurillo/gofastr/" + pkg,
			Text:       pkg + " ↗",
			ExtraAttrs: html.Attrs{"rel": "external"},
		}),
	})

	// Narrow (no-rail) docpage: breadcrumb + head + live demo + usage
	// code, the article centered in the shell the site's docpage
	// package draws.
	return docpage.Render(docpage.Config{
		Crumbs: []ui.Crumb{
			{Text: "Components", Href: "/components/"},
			{Text: s.Entry.Category, Href: "/components/#" + categorySlug(s.Entry.Category)},
			{Text: s.Entry.Name, Current: true},
		},
		CrumbsLabel: "Components",
	},
		ui.Stack(ui.StackConfig{Gap: ui.Gap2XL},
			head,
			// Demo panel. Components that render a self-contained live
			// instance are labeled "Live"; ones that show an explanatory
			// note (need per-page wiring) are labeled "Note".
			s.demoStage(ctx),
			// The site-local registered behaviour (behavior_ping.go), on the
			// Button page: proof a host package ships behaviour the same way
			// it ships a stylesheet.
			s.registeredBehaviorSection(),
			// Example code, the Go that produced the live demo above.
			s.usage(),
		),
	)
}

// registeredBehaviorSection renders the "Registered behaviour" section
// on the Button showcase page only. The button below carries
// data-site-ping; the site-ping module (behavior_ping.go) is
// demand-loaded by the runtime when it sees the marker, attaches, and
// toggles aria-pressed on click. Composed entirely from design-system
// pieces (hard rule 7): ui.Section + ui.Button, zero bespoke CSS.
func (s *ComponentShowcaseScreen) registeredBehaviorSection() render.HTML {
	if s.Entry.Slug != "button" {
		return render.HTML("")
	}
	return ui.Section(ui.SectionConfig{
		Heading:     "Registered behaviour",
		Description: "This button's behaviour is registered by the site itself with registry.RegisterBehavior and demand-loaded by the runtime when it sees the data-site-ping marker.",
		Compact:     true,
	},
		// A Cluster, so the button keeps its own width inside the
		// section's stacked body.
		ui.Cluster(ui.ClusterConfig{}, ui.Button(ui.ButtonConfig{
			Label: "Ping",
			ID:    "site-ping-btn",
			// data-cui-prefetch warms the module on hover; aria-pressed
			// is the attribute the behaviour toggles on click.
			ExtraAttrs: html.Attrs{
				"data-site-ping":    "1",
				"data-cui-prefetch": "site-ping",
				"aria-pressed":      "false",
			},
		})),
	)
}

// usage renders the example-code section for the component, when one is
// registered via gallery.CodeSnippet. Returns empty HTML otherwise. The
// section carries id="example" so a link (and a test) can address it.
func (s *ComponentShowcaseScreen) usage() render.HTML {
	code := gallery.CodeSnippet(s.Entry.Slug)
	if code == "" {
		return render.HTML("")
	}
	return ui.Section(ui.SectionConfig{Heading: "Example", ID: "example", Compact: true},
		ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: code}),
	)
}
