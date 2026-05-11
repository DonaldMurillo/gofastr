package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
)

// StatsService is a singleton that tracks app-wide stats.
type StatsService struct {
	PageViews    int
	Interactions int
}

// DashboardScreen demonstrates DI: its Stats field is auto-filled
// from the container on every render.
type DashboardScreen struct {
	Stats *StatsService `inject:""`
}

func (s *DashboardScreen) ScreenTitle() string       { return "Dashboard" }
func (s *DashboardScreen) ScreenDescription() string { return "DI showcase" }

func (s *DashboardScreen) Render() render.HTML {
	views := 0
	actions := 0
	if s.Stats != nil {
		s.Stats.PageViews++
		views = s.Stats.PageViews
		actions = s.Stats.Interactions
	}

	proofText := "StatsService is nil — DI not working!"
	if s.Stats != nil {
		proofText = fmt.Sprintf("DI working! StatsService injected (PageViews=%d, Interactions=%d). Reload this page — the counter keeps climbing because it's the SAME Go pointer in memory.", views, actions)
	}

	return html.Div(html.DivConfig{Class: "di-showcase"},
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Dependency Injection")),
		html.Paragraph(html.TextConfig{}, render.Text(
			"StatsService is registered once at startup and injected into this screen via `inject:\"\"` struct tags. "+
				"Reload this page — the Page Views counter keeps climbing because it's the same singleton in memory.",
		)),
		html.Div(html.DivConfig{Class: "di-card-grid"},
			html.Div(html.DivConfig{Class: "di-card"},
				html.Div(html.DivConfig{Class: "di-card-icon"}, render.Text("👁")),
				html.Div(html.DivConfig{Class: "di-card-label"}, render.Text("Page Views (from DI singleton)")),
				html.Div(html.DivConfig{Class: "di-card-value", Attrs: html.Attrs{"data-count": "views"}}, render.Text(fmt.Sprintf("%d", views))),
				html.Div(html.DivConfig{Class: "di-card-hint"}, render.Text("Reload to see this increment")),
			),
			html.Div(html.DivConfig{Class: "di-card"},
				html.Div(html.DivConfig{Class: "di-card-icon"}, render.Text("📊")),
				html.Div(html.DivConfig{Class: "di-card-label"}, render.Text("Service Memory Address")),
				html.Div(html.DivConfig{Class: "di-card-value"}, render.Text(fmt.Sprintf("%p", s.Stats))),
				html.Div(html.DivConfig{Class: "di-card-hint"}, render.Text("Same address every reload = same object")),
			),
		),
		html.Div(html.DivConfig{Class: "di-proof"}, render.Text(proofText)),
		html.Details(html.DetailsConfig{Class: "di-details"},
			html.Summary(html.SummaryConfig{}, render.Text("How this works (code)")),
			html.Div(html.DivConfig{Class: "di-code-block"},
				html.Pre(html.TextConfig{}, render.Text(`// 1. Register singleton at app setup (app.go):
app.Provide(&StatsService{})

// 2. Screen declares dependency via tag (this file):
type DashboardScreen struct {
    Stats *StatsService `+"`inject:\"\"`"+`
}

// 3. Framework auto-injects before every render:
//    a.Inject(screen.Component)

// 4. Screen uses the injected service:
func (s *DashboardScreen) Render() render.HTML {
    s.Stats.PageViews++  // mutates the real singleton
    // ...
}

// The screen doesn't create StatsService.
// It doesn't import a global variable.
// It just says "I need one" via the struct tag.
// The container provides it. That's DI.`)),
			),
		),
	)
}
