package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type FilterChipBarScreen struct{}

func (s *FilterChipBarScreen) ScreenTitle() string {
	return "Filter Chip Bar"
}
func (s *FilterChipBarScreen) ScreenDescription() string {
	return "Toolbar of removable filter chips above a table or search result."
}
func (s *FilterChipBarScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *FilterChipBarScreen) Render() render.HTML {
	// Demo state is reset per-session — start fresh on each page load.
	filterDemoState.reset()
	demo := ui.FilterChipBar(ui.FilterChipBarConfig{
		ID:           "filter-bar-demo",
		Label:        "Active filters",
		Filters:      filterDemoState.snapshot(),
		ClearAllPath: "/islands/new-components/filter-clear",
		RPCSignal:    "filter-bar-demo",
		SignalName:   "filter-bar-demo",
	})
	resetBtn := ui.Button(ui.ButtonConfig{
		Label:   "Reset filters",
		Variant: ui.ButtonSecondary,
		ExtraAttrs: html.Attrs{
			"data-fui-rpc":        "/islands/new-components/filter-reset",
			"data-fui-rpc-method": "POST",
			"data-fui-rpc-signal": "filter-bar-demo",
		},
	})
	wrapped := render.Tag("div", map[string]string{"class": "demo-stack demo-stack--sm"},
		demo, resetBtn)
	src := `ui.FilterChipBar(ui.FilterChipBarConfig{
    Label: "Active filters",
    Filters: []ui.FilterChip{
        {Label: "Status: Active", DismissPath: "/filters/remove?key=status", Variant: ui.StatusSuccess},
        {Label: "Tag: urgent",    DismissPath: "/filters/remove?key=tag",    Variant: ui.StatusWarning},
        {Label: "Owner: Alice",   DismissPath: "/filters/remove?key=owner"},
    },
    ClearAllPath: "/filters/clear-all",
    RPCSignal:    "filter-bar",
    SignalName:   "filter-bar",
})

// Server handler returns the new chip bar HTML after each remove:
func filterRemove(w http.ResponseWriter, r *http.Request) {
    activeFilters.Remove(r.FormValue("key"))
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Write([]byte(ui.FilterChipBar(activeFilters.Config()))) // re-renders bar
}`
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Filter Chip Bar")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"role=\"toolbar\" wrapper of removable Tag chips with optional \"Clear all\" trailing button. Each × click POSTs to the chip's DismissPath; the server tracks active filters and returns the full re-rendered bar HTML, which the runtime swaps in via the signal binding.")),
		demoFrame(wrapped, src),
		render.Tag("p", map[string]string{"class": "demo-note"}, render.Text(
			"Click any × to remove a filter, or \"Clear all\" to wipe them. Use \"Reset filters\" below to restore the demo state.")),
	)
}
