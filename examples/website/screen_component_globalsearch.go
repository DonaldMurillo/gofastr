package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type GlobalSearchScreen struct{}

func (s *GlobalSearchScreen) ScreenTitle() string { return "Global Search" }
func (s *GlobalSearchScreen) ScreenDescription() string {
	return "Sticky search bar with /-shortcut focus + combobox dropdown."
}
func (s *GlobalSearchScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *GlobalSearchScreen) Render() render.HTML {
	demo := ui.GlobalSearch(ui.GlobalSearchConfig{
		ID:          "globalsearch-demo",
		Name:        "q",
		Label:       "Search the site",
		Placeholder: "Search components, docs, examples…",
		RPCPath:     "/components/globalsearch/results",
		SignalName:  "globalsearch-demo-results",
	})

	src := `ui.GlobalSearch(ui.GlobalSearchConfig{
    ID:          "global-search",
    Name:        "q",
    Label:       "Search",
    Placeholder: "Search…",
    RPCPath:     "/api/search",
    SignalName:  "global-search-results",
    // Shortcut defaults to "/"; set to " " to disable.
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Global Search")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Sticky inline search bar with /-shortcut focus + a Combobox-driven results dropdown. Distinct from CommandPalette (⌘K focus-trapped modal) — GlobalSearch is persistent, inline, and per-page. Type a query → debounced POST to RPCPath → server response HTML replaces the listbox via signal swap.")),
		demoFrame(demo, src),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Press / anywhere on this page (outside another input) to focus the search bar above.")),
	)
}
