package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/progress"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type ProgressScreen struct{}

func (s *ProgressScreen) ScreenTitle() string        { return "Progress" }
func (s *ProgressScreen) ScreenDescription() string  { return "Native <progress> with theme-aware styling." }
func (s *ProgressScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProgressScreen) Render() render.HTML {
	determinate := progress.New(progress.Config{
		Value: 73, Max: 100, Label: "Upload progress", Description: "73 of 100",
	})
	determinateLow := progress.New(progress.Config{
		Value: 18, Max: 100, Label: "Storage used", Description: "18% of 1 TB",
	})
	indeterminate := progress.New(progress.Config{
		Value: -1, Label: "Working…", Description: "Reticulating splines…",
	})

	stack := render.Tag("div", map[string]string{"class": "demo-stack"},
		determinate, determinateLow, indeterminate,
	)

	source := `progress.New(progress.Config{
    Value: 73, Max: 100,
    Label: "Upload progress",
    Description: "73 of 100",
})

progress.New(progress.Config{
    Value: -1,                 // indeterminate
    Label: "Working…",
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Progress")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A wrapper around the native <progress> element. Two modes: determinate (Value set) and indeterminate (Value < 0).")),
		demoFrame(stack, source),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Driving live updates")),
		render.Tag("p", nil, render.Text(
			"For a server-pushed progress bar, bind a signal to the <progress value> attribute via data-fui-signal-attr=value. The runtime updates the attribute on every signal push, no page reload. (Pattern reused from core-ui/runtime — see widget docs.)")),
	)
}
