package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/elements"
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

	return elements.Div(elements.DivConfig{Class: "di-showcase"},
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Dependency Injection")),
		elements.Paragraph(elements.TextConfig{}, render.Text(
			"StatsService is registered once at startup and injected into this screen via `inject:\"\"` struct tags. "+
				"Reload this page — the Page Views counter keeps climbing because it's the same singleton in memory.",
		)),
		elements.Div(elements.DivConfig{Class: "di-card-grid"},
			elements.Div(elements.DivConfig{Class: "di-card"},
				elements.Div(elements.DivConfig{Class: "di-card-icon"}, render.Text("👁")),
				elements.Div(elements.DivConfig{Class: "di-card-label"}, render.Text("Page Views (from DI singleton)")),
				elements.Div(elements.DivConfig{Class: "di-card-value", Attrs: elements.Attrs{"data-count": "views"}}, render.Text(fmt.Sprintf("%d", views))),
				elements.Div(elements.DivConfig{Class: "di-card-hint"}, render.Text("Reload to see this increment")),
			),
			elements.Div(elements.DivConfig{Class: "di-card"},
				elements.Div(elements.DivConfig{Class: "di-card-icon"}, render.Text("📊")),
				elements.Div(elements.DivConfig{Class: "di-card-label"}, render.Text("Service Memory Address")),
				elements.Div(elements.DivConfig{Class: "di-card-value"}, render.Text(fmt.Sprintf("%p", s.Stats))),
				elements.Div(elements.DivConfig{Class: "di-card-hint"}, render.Text("Same address every reload = same object")),
			),
		),
		elements.Div(elements.DivConfig{Class: "di-proof"}, render.Text(proofText)),
		elements.Details(elements.DetailsConfig{Class: "di-details"},
			elements.Summary(elements.SummaryConfig{}, render.Text("How this works (code)")),
			elements.Div(elements.DivConfig{Class: "di-code-block"},
				elements.Pre(elements.TextConfig{}, render.Text(`// 1. Register singleton at app setup (app.go):
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
