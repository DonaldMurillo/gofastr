package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type PieChartScreen struct{}

func (s *PieChartScreen) ScreenTitle() string { return "Pie / Donut Chart" }
func (s *PieChartScreen) ScreenDescription() string {
	return "Pure-SVG ratio chart with optional donut hole."
}
func (s *PieChartScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *PieChartScreen) Render() render.HTML {
	slices := []ui.PieSlice{
		{Label: "Mobile", Value: 48},
		{Label: "Desktop", Value: 32},
		{Label: "Tablet", Value: 12},
		{Label: "Other", Value: 8},
	}

	pie := ui.PieChart(ui.PieChartConfig{Slices: slices})
	donut := ui.PieChart(ui.PieChartConfig{
		Slices:        slices,
		InnerRadius:   0.6,
		CenterLabel:   "100",
		CenterSubtext: "Users (k)",
	})

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Pie / Donut Chart")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Pure-SVG ratio chart. Slice colors cycle through the theme palette (primary/info/success/warning/danger); per-slice Color overrides apply. Set InnerRadius (0–1) to cut the center out → donut. Each slice's Label becomes an SVG <title> for AT.")),
		demoFrame(render.Tag("div", map[string]string{"class": "demo-row-flex"}, pie, donut),
			`ui.PieChart(ui.PieChartConfig{
    Slices: []ui.PieSlice{
        {Label: "Mobile",  Value: 48},
        {Label: "Desktop", Value: 32},
        {Label: "Tablet",  Value: 12},
        {Label: "Other",   Value: 8},
    },
})

// Donut variant with center label
ui.PieChart(ui.PieChartConfig{
    Slices:        slices,
    InnerRadius:   0.6,
    CenterLabel:   "100",
    CenterSubtext: "Users (k)",
})`),
	)
}
