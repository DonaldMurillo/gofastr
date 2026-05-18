package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/disclosure"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type DisclosureScreen struct{}

func (s *DisclosureScreen) ScreenTitle() string { return "Disclosure" }
func (s *DisclosureScreen) ScreenDescription() string {
	return "Single styled <details>/<summary> primitive."
}
func (s *DisclosureScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *DisclosureScreen) Render() render.HTML {
	demo := render.Tag("div", map[string]string{"class": "demo-stack"},
		disclosure.Render(disclosure.Config{Title: "What's included in the free plan?"},
			html.Paragraph(html.TextConfig{},
				render.Text("Up to 5 projects, 1 GB storage, community support, and all core features. Upgrade to Pro for unlimited projects + priority support.")),
		),
		disclosure.Render(disclosure.Config{Title: "Can I export my data?", Open: true},
			html.Paragraph(html.TextConfig{},
				render.Text("Yes. Settings → Export emits a JSON archive with everything we have about you, no questions asked.")),
		),
	)

	src := `disclosure.Render(disclosure.Config{Title: "What's included?"},
    html.Paragraph(html.TextConfig{}, render.Text("Up to 5 projects, 1 GB storage, …")),
)

disclosure.Render(disclosure.Config{Title: "Export?", Open: true},
    html.Paragraph(html.TextConfig{}, render.Text("Settings → Export emits a JSON archive.")),
)`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Disclosure")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Single styled <details>/<summary>. Native semantics — keyboard, screen reader, browser \"find in page\" expansion all work without JavaScript. Use for one-off reveals (FAQ rows, expandable help). Accordion composes groups of these.")),
		demoFrame(demo, src),
	)
}
