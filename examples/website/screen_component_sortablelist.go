package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/sortablelist"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type SortableListScreen struct{}

func (s *SortableListScreen) ScreenTitle() string { return "Sortable List" }
func (s *SortableListScreen) ScreenDescription() string {
	return "Reorderable list with drag-and-drop + keyboard fallback."
}
func (s *SortableListScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SortableListScreen) Render() render.HTML {
	demo := sortablelist.Render(sortablelist.Config{
		Label:   "Priorities",
		RPCPath: "/components/sortablelist/reorder",
		Items: []sortablelist.Item{
			{Key: "deploy", Label: "Ship the deploy pipeline"},
			{Key: "auth", Label: "Wire up SSO"},
			{Key: "billing", Label: "Replace Stripe checkout"},
			{Key: "docs", Label: "Refresh framework docs"},
			{Key: "onboarding", Label: "Tighten onboarding flow"},
		},
	})

	src := `sortablelist.Render(sortablelist.Config{
    Label:   "Priorities",
    RPCPath: "/api/priorities/reorder",
    Items: []sortablelist.Item{
        {Key: "deploy",     Label: "Ship the deploy pipeline"},
        {Key: "auth",       Label: "Wire up SSO"},
        {Key: "billing",    Label: "Replace Stripe checkout"},
        // …
    },
})

// Server handler — POST order=<comma-keys>, apply or reject.
func priorityReorder(w http.ResponseWriter, r *http.Request) {
    keys := strings.Split(r.FormValue("order"), ",")
    // … persist the new sort … return 2xx on success
}`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Sortable List")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Drag-and-drop reorderable list with keyboard fallback. Tab onto a row, press Space to grab, Arrow Up/Down to move, Space again to drop, Esc to cancel. After a successful reorder the runtime POSTs the new key sequence to RPCPath; non-2xx response reverts the DOM.")),
		demoFrame(demo, src),
	)
}
