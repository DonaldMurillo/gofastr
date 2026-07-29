package main

// =============================================================================
// /components — the showcase screens.
//
// The catalog itself (the 141 entries, the code snippets, the note-only
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
//   - ComponentsIndexScreen and ComponentShowcaseScreen — the two screens
//     registered in main.go.
//
// This is a pure move: no behavior, no markup, no styling changed. The
// catalog iteration order, the rendered HTML, and the route shape are
// byte-for-byte what they were when the catalog lived here.
// =============================================================================

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
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
// components_sidebar.go — they share the SectionMenu builder with the
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
// (no request) — that's what a theme previewer / static export sees.

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
// /components/  — the index page listing every catalog entry as a card,
// grouped by category. Re-uses .docs / .doc.. grid from the concepts page.
// =============================================================================

type ComponentsIndexScreen struct{}

func (s *ComponentsIndexScreen) ScreenTitle() string { return "Components" }
func (s *ComponentsIndexScreen) ScreenDescription() string {
	return "Every framework/ui and core-ui/patterns constructor, one page each."
}
func (s *ComponentsIndexScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ComponentsIndexScreen) Render() render.HTML {
	// The inner /components/* layout supplies the sidebar (ComponentsSidebar
	// component) — this screen is just the overview content cell. Grouped
	// card grid, no rail (the sidebar is the persistent nav).
	groups := groupCatalog()

	hero := html.Div(html.DivConfig{Class: "components-overview__hero"},
		html.Div(html.DivConfig{Class: "mb-lg"}, tagAccent("Components · v"+siteVersion)),
		html.Heading(html.HeadingConfig{Level: 1, Class: "components-overview__title"},
			render.Text("Every component, "),
			html.Span(html.TextConfig{Class: "amber"}, render.Text("as typed Go")),
			render.Text("."),
		),
		html.Paragraph(html.TextConfig{Class: "components-overview__lede"},
			render.Text("One page per constructor. Use the sidebar to jump between them — it tracks the page you're on."),
		),
	)

	sections := []render.HTML{}
	for _, g := range groups {
		cards := []render.HTML{}
		for _, c := range g.Entries {
			cards = append(cards, html.LinkHTML(html.LinkHTMLConfig{
				Href:  "/components/" + c.Slug,
				Class: "doc",
				Content: render.Join(
					html.Div(html.DivConfig{Class: "doc__head"},
						html.Span(html.TextConfig{Class: "pill ui"}, render.Text(g.Name)),
					),
					html.Div(html.DivConfig{Class: "doc__title"}, render.Text(c.Name)),
					html.Div(html.DivConfig{Class: "doc__desc"}, render.Text(c.Desc)),
					html.Div(html.DivConfig{Class: "doc__meta"}, render.Text("/components/"+c.Slug)),
				),
			}))
		}
		sections = append(sections, ui.Section(
			ui.SectionConfig{Heading: g.Name, Class: "intent", ID: categorySlug(g.Name)},
			html.Span(html.TextConfig{Class: "intent__meta"}, render.Text(itoa(len(g.Entries))+" constructors")),
			html.Div(html.DivConfig{Class: "docs"}, cards...),
		))
	}

	return render.Join(hero, html.Div(html.DivConfig{Class: "components-overview__sections"}, sections...))
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

// =============================================================================
// /components/{slug} — single-component showcase page.
// =============================================================================

// ComponentShowcaseScreen implements RenderCtx so the three stateful demos
// (kanban, optimistic create/delete) read the visitor's session off the request
// in ctx and a reload reflects their own state. It ALSO keeps an explicit
// Render() that delegates to RenderCtx with a background context: SSR and
// static export prefer RenderCtx, but the llm.md generator
// (core-ui/app/llmmd.go) calls Component.Render() directly — without this the
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

// demoStage renders the demo box with an honest label: "Live" for a
// real interactive instance, "Note" for a wiring explanation.
func (s *ComponentShowcaseScreen) demoStage(ctx context.Context) render.HTML {
	label := "Live"
	if gallery.IsNoteOnly(s.Entry.Slug) {
		label = "Note"
	}
	return html.Div(html.DivConfig{Class: "demo-stage"},
		html.Heading(html.HeadingConfig{Level: 2, Class: "demo-stage__label"}, render.Text(label)),
		html.Div(html.DivConfig{Class: "demo-stage__viewport"}, s.renderDemo(ctx)),
	)
}

func (s *ComponentShowcaseScreen) RenderCtx(ctx context.Context) render.HTML {
	head := html.Div(html.DivConfig{Class: "doc-head"},
		html.Heading(html.HeadingConfig{Level: 1},
			render.Text(s.Entry.Name),
		),
		html.Div(html.DivConfig{Class: "doc-head__meta"},
			tagAccent(s.Entry.Category),
			// Real source package, linked to its API docs — this is
			// the per-component "usage/reference" the page otherwise
			// lacked. (Was hardcoded "framework/ui" for everything.)
			html.LinkHTML(html.LinkHTMLConfig{
				Href:       "https://pkg.go.dev/github.com/DonaldMurillo/gofastr/" + componentPkg(s.Entry.Slug),
				ExtraAttrs: html.Attrs{"rel": "external"},
				Content:    render.Join(render.Text(componentPkg(s.Entry.Slug)), render.Text(" ↗")),
			}),
		),
		html.Paragraph(html.TextConfig{Class: "doc-head__lede"}, render.Text(s.Entry.Desc)),
	)

	// Narrow (no-rail) DocLayout: breadcrumb + head + live demo + usage code.
	return ui.DocLayout(ui.DocLayoutConfig{
		Crumbs: []ui.DocCrumb{
			{Label: "Components", Href: "/components/"},
			{Label: s.Entry.Category, Href: "/components/#" + categorySlug(s.Entry.Category)},
			{Label: s.Entry.Name},
		},
	},
		head,
		// Demo panel. Components that render a self-contained live instance
		// are labeled "Live"; ones that show an explanatory note (need
		// per-page wiring) are labeled "Note" so the box is honest.
		s.demoStage(ctx),
		// Example code — the Go that produced the live demo above.
		s.usage(),
	)
}

// usage renders the example-code block for the component, when one is
// registered via gallery.CodeSnippet. Returns empty HTML otherwise.
func (s *ComponentShowcaseScreen) usage() render.HTML {
	code := gallery.CodeSnippet(s.Entry.Slug)
	if code == "" {
		return render.HTML("")
	}
	return html.Div(html.DivConfig{Class: "doc-usage"},
		html.Heading(html.HeadingConfig{Level: 2, Class: "doc-usage__title"}, render.Text("Example")),
		ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: code}),
	)
}
