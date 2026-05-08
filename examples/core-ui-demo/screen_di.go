package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

// =============================================================================
// DI Showcase: a shared singleton service injected into screens
// =============================================================================
//
// What this proves:
//   1. StatsService is registered ONCE at startup in the DI container
//   2. DashboardScreen declares `Stats *StatsService `inject:""``
//   3. Before every render, the framework calls a.Inject(screen.Component)
//   4. The screen receives the SAME singleton — page views keep incrementing
//   5. Reload this page and watch "Page Views" climb — same object in memory
//
// Without DI, the screen would need to import a global or receive the service
// through some other coupling. With DI, it just declares what it needs.
// =============================================================================

// StatsService is a singleton that tracks app-wide stats.
// Registered once in the DI container, shared across all screens.
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
		s.Stats.PageViews++ // bump on every render — proves it's the same singleton
		views = s.Stats.PageViews
		actions = s.Stats.Interactions
	}

	// Build proof strings
	proofText := "StatsService is nil — DI not working!"
	if s.Stats != nil {
		proofText = fmt.Sprintf("DI working! StatsService injected (PageViews=%d, Interactions=%d). Reload this page — the counter keeps climbing because it's the SAME Go pointer in memory.", views, actions)
	}

	return elements.Div(elements.Attrs{"class": "di-showcase"},
		elements.Heading(2, nil, render.Text("Dependency Injection")),
		elements.Paragraph(nil, render.Text(
			"StatsService is registered once at startup and injected into this screen via `inject:\"\"` struct tags. "+
				"Reload this page — the Page Views counter keeps climbing because it's the same singleton in memory.",
		)),

		// Live stats from the injected singleton
		elements.Div(elements.Attrs{"class": "di-card-grid"},
			elements.Div(elements.Attrs{"class": "di-card"},
				elements.Div(elements.Attrs{"class": "di-card-icon"}, render.Text("👁")),
				elements.Div(elements.Attrs{"class": "di-card-label"}, render.Text("Page Views (from DI singleton)")),
				elements.Div(elements.Attrs{"class": "di-card-value", "data-count": "views"}, render.Text(fmt.Sprintf("%d", views))),
				elements.Div(elements.Attrs{"class": "di-card-hint"}, render.Text("Reload to see this increment")),
			),
			elements.Div(elements.Attrs{"class": "di-card"},
				elements.Div(elements.Attrs{"class": "di-card-icon"}, render.Text("📊")),
				elements.Div(elements.Attrs{"class": "di-card-label"}, render.Text("Service Memory Address")),
				elements.Div(elements.Attrs{"class": "di-card-value"}, render.Text(fmt.Sprintf("%p", s.Stats))),
				elements.Div(elements.Attrs{"class": "di-card-hint"}, render.Text("Same address every reload = same object")),
			),
		),

		// Proof
		elements.Div(elements.Attrs{"class": "di-proof"}, render.Text(proofText)),

		// How it works
		elements.Details(elements.Attrs{"class": "di-details"},
			elements.Summary(nil, render.Text("How this works (code)")),
			elements.Div(elements.Attrs{"class": "di-code-block"},
				elements.Pre(nil, render.Text(`// 1. Register singleton at app setup (app.go):
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
