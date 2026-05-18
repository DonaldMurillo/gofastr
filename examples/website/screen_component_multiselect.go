package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/multiselect"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type MultiSelectScreen struct{}

func (s *MultiSelectScreen) ScreenTitle() string { return "Multi-Select" }
func (s *MultiSelectScreen) ScreenDescription() string {
	return "Checkbox-group inside a disclosure with chip rendering."
}
func (s *MultiSelectScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *MultiSelectScreen) Render() render.HTML {
	demo := html.Form(html.FormConfig{Method: "post", Action: "#"},
		multiselect.Render(multiselect.Config{
			Name:        "languages",
			Label:       "Programming languages",
			Placeholder: "Pick languages…",
			Options: []multiselect.Option{
				{Value: "go", Label: "Go", Selected: true},
				{Value: "rust", Label: "Rust"},
				{Value: "typescript", Label: "TypeScript", Selected: true},
				{Value: "python", Label: "Python"},
				{Value: "elixir", Label: "Elixir"},
				{Value: "kotlin", Label: "Kotlin"},
				{Value: "swift", Label: "Swift"},
			},
		}),
	)

	src := `multiselect.Render(multiselect.Config{
    Name:        "languages",
    Label:       "Programming languages",
    Placeholder: "Pick languages…",
    Options: []multiselect.Option{
        {Value: "go", Label: "Go", Selected: true},
        {Value: "rust", Label: "Rust"},
        {Value: "typescript", Label: "TypeScript", Selected: true},
        …
    },
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Multi-Select")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Checkbox group inside a <details> disclosure with chip rendering above. Each checkbox shares the same Name so the form submits the standard repeated-key pattern (?languages=go&languages=typescript) — no hidden field, no JSON. The runtime rebuilds the chip strip on every change; clicking a chip's × unchecks the option.")),
		demoFrame(demo, src),
	)
}
