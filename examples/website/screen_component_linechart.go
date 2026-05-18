package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type LineChartScreen struct{}

func (s *LineChartScreen) ScreenTitle() string { return "Line Chart" }
func (s *LineChartScreen) ScreenDescription() string {
	return "Multi-series time-series line chart."
}
func (s *LineChartScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *LineChartScreen) Render() render.HTML {
	demo := ui.LineChart(ui.LineChartConfig{
		Series: []ui.LineSeries{
			{Name: "Signups", Values: []float64{12, 18, 22, 31, 28, 35, 42, 48, 55, 62, 68, 74}, Area: true},
			{Name: "Activations", Values: []float64{8, 12, 16, 20, 22, 28, 31, 35, 42, 48, 53, 58}},
			{Name: "Churn", Values: []float64{2, 3, 2, 4, 3, 5, 4, 6, 5, 7, 6, 8}, Color: "danger"},
		},
		Labels:     []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		Width:      540, Height: 220,
		ShowLegend: true,
	})

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Line Chart")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Multi-series SVG line chart. Each series can opt into Area fill underneath. Colors cycle through the theme palette; per-series Color overrides apply. Labels render evenly under the chart; ShowLegend adds a legend strip below.")),
		demoFrame(demo, `ui.LineChart(ui.LineChartConfig{
    Series: []ui.LineSeries{
        {Name: "Signups",     Values: signups, Area: true},
        {Name: "Activations", Values: activations},
        {Name: "Churn",       Values: churn, Color: "danger"},
    },
    Labels:     []string{"Jan", "Feb", …, "Dec"},
    ShowLegend: true,
})`),
	)
}
