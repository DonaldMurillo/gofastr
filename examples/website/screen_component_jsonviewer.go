package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type JSONViewerScreen struct{}

func (s *JSONViewerScreen) ScreenTitle() string { return "JSON Viewer" }
func (s *JSONViewerScreen) ScreenDescription() string {
	return "Collapsible tree for arbitrary data."
}
func (s *JSONViewerScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *JSONViewerScreen) Render() render.HTML {
	value := map[string]any{
		"id":   "user_4821",
		"name": "Ada Lovelace",
		"meta": map[string]any{
			"created_at": "2026-05-18T09:14:02Z",
			"flags":      []any{"beta", "admin"},
			"limits": map[string]any{
				"requests_per_minute": 60,
				"max_seats":           5,
			},
		},
		"verified": true,
		"deletedAt": nil,
	}

	demo := ui.JSONViewer(ui.JSONViewerConfig{
		Value: value, OpenDepth: 1,
	})

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("JSON Viewer")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Collapsible tree of any Go value. Uses native <details>/<summary> for collapse — keyboard, find-in-page, and screen reader all work. Strings / numbers / bools render inline; objects + arrays become collapsible nodes. OpenDepth controls initial expansion.")),
		demoFrame(demo, `ui.JSONViewer(ui.JSONViewerConfig{
    Value:     someStruct,
    OpenDepth: 1, // root + first nested level open by default
})`),
	)
}
