package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type BarChartScreen struct{}

func (s *BarChartScreen) ScreenTitle() string { return "Bar Chart" }
func (s *BarChartScreen) ScreenDescription() string {
	return "Categorical SVG bar chart."
}
func (s *BarChartScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *BarChartScreen) Render() render.HTML {
	demo := ui.BarChart(ui.BarChartConfig{
		Bars: []ui.BarChartBar{
			{Label: "Mon", Value: 28},
			{Label: "Tue", Value: 42},
			{Label: "Wed", Value: 35},
			{Label: "Thu", Value: 58, Color: "success"},
			{Label: "Fri", Value: 47},
			{Label: "Sat", Value: 22},
			{Label: "Sun", Value: 16},
		},
		Width: 480, Height: 200,
		ShowAxis: true, ShowLabels: true,
	})

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Bar Chart")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Categorical SVG bar chart. Per-bar Color overrides apply (palette key or any CSS color). ShowAxis adds min/max value labels; ShowLabels adds x-axis category labels. Each bar's Label becomes a <title> for AT hover/SR.")),
		demoFrame(demo, `ui.BarChart(ui.BarChartConfig{
    Bars: []ui.BarChartBar{
        {Label: "Mon", Value: 28},
        {Label: "Tue", Value: 42},
        {Label: "Wed", Value: 35},
        {Label: "Thu", Value: 58, Color: "success"},
        {Label: "Fri", Value: 47},
        {Label: "Sat", Value: 22},
        {Label: "Sun", Value: 16},
    },
    Width: 480, Height: 200,
    ShowAxis: true, ShowLabels: true,
})`),
	)
}
