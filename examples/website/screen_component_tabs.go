package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/tabs"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type TabsScreen struct{}

func (s *TabsScreen) ScreenTitle() string        { return "Tabs" }
func (s *TabsScreen) ScreenDescription() string  { return "Zero-JS tabs via <details name=>." }
func (s *TabsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TabsScreen) Render() render.HTML {
	demo := tabs.New(tabs.Config{Name: "demo-tabs", Label: "Component overview"},
		tabs.Tab{
			Label:   "Overview",
			Content: render.Tag("p", nil, render.Text("Tabs render as native <details> elements sharing a name= attribute. The browser enforces exclusivity. CSS Grid arranges the summaries in a horizontal row and reveals the active panel below.")),
			Open:    true,
		},
		tabs.Tab{
			Label:   "Accessibility",
			Content: render.Tag("p", nil, render.Text("Native keyboard support: Tab moves focus between summaries, Enter or Space activates. Screen readers announce as a disclosure widget — honest about the underlying mechanic. If you need ARIA tablist semantics, use core-ui/widget for a custom implementation.")),
		},
		tabs.Tab{
			Label:   "Trade-offs",
			Content: render.Tag("p", nil, render.Text("Zero JavaScript, zero CSP complications, no state synchronization between server and client. Cost: arrow-key tab cycling is not supported (Tab key moves between summaries instead). For most product UIs, this trade is the right one.")),
		},
	)

	source := `tabs.New(tabs.Config{Name: "demo-tabs"},
    tabs.Tab{Label: "Overview",      Content: overview, Open: true},
    tabs.Tab{Label: "Accessibility", Content: a11y},
    tabs.Tab{Label: "Trade-offs",    Content: tradeoffs},
)`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Tabs")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A tabbed-content layout built from native <details> elements with a shared name= attribute. Zero JavaScript, full keyboard accessibility.")),
		demoFrame(demo, source),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("How it works")),
		render.Tag("p", nil, render.Text(
			"Each tab is a <details> element. They share a name= attribute, so the browser closes the previously-open one when a new one opens — same mechanic as accordion.Group. CSS Grid arranges summaries in row 1 and panels in row 2 spanning all columns.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("API")),
		render.Tag("pre", nil, render.Tag("code", nil, render.Text(
			`type Config struct {
    Name  string // required
    Label string
    ID    string
    Class string
}

type Tab struct {
    Label   string      // required
    Content render.HTML // required
    Open    bool        // initially active (auto-defaults to first tab)
    ID      string
}

func New(cfg Config, tabs ...Tab) render.HTML
func BaseCSS() string`,
		))),
	)
}
