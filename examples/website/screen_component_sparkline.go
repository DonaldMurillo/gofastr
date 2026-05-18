package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type SparklineScreen struct{}

func (s *SparklineScreen) ScreenTitle() string { return "Sparkline" }
func (s *SparklineScreen) ScreenDescription() string {
	return "Inline SVG trend chart."
}
func (s *SparklineScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SparklineScreen) Render() render.HTML {
	series := []float64{12, 17, 14, 22, 19, 25, 21, 27, 30, 28, 33, 31, 38}

	withCard := ui.StatCard(ui.StatCardConfig{
		Label:     "MAU",
		Value:     "12,483",
		Trend:     "+18% vs. last week",
		Direction: ui.TrendUp,
	})

	demos := render.Tag("div", map[string]string{"class": "demo-stack"},
		render.Tag("div", map[string]string{"class": "demo-row-flex"},
			html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Default")),
			ui.Sparkline(ui.SparklineConfig{Values: series}),
		),
		render.Tag("div", map[string]string{"class": "demo-row-flex"},
			html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Area")),
			ui.Sparkline(ui.SparklineConfig{Values: series, Shape: ui.SparklineArea}),
		),
		render.Tag("div", map[string]string{"class": "demo-row-flex"},
			html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Color: success")),
			ui.Sparkline(ui.SparklineConfig{Values: series, Shape: ui.SparklineArea, Color: "success"}),
		),
		render.Tag("div", map[string]string{"class": "demo-row-flex"},
			html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Color: danger")),
			ui.Sparkline(ui.SparklineConfig{Values: []float64{50, 48, 42, 35, 30, 28, 22, 18}, Color: "danger"}),
		),
	)

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Sparkline")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Inline SVG trend chart. Pure render — no JS, no hydration. Normalizes the y-axis to its own min/max so the silhouette is what matters, not the absolute value. Pairs with StatCard.")),
		demoFrame(demos, `ui.Sparkline(ui.SparklineConfig{Values: series})
ui.Sparkline(ui.SparklineConfig{Values: series, Shape: ui.SparklineArea})
ui.Sparkline(ui.SparklineConfig{Values: series, Color: "success"})`),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("With StatCard")),
		demoFrame(withCard, `ui.StatCard(ui.StatCardConfig{
    Label: "MAU", Value: "12,483",
    Trend: "+18% vs. last week", Direction: ui.TrendUp,
})`),
	)
}
