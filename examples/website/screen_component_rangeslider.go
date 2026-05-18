package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type RangeSliderScreen struct{}

func (s *RangeSliderScreen) ScreenTitle() string { return "Range Slider" }
func (s *RangeSliderScreen) ScreenDescription() string {
	return "Dual-thumb range with cross-clamp."
}
func (s *RangeSliderScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *RangeSliderScreen) Render() render.HTML {
	demo := html.Form(html.FormConfig{Method: "post", Action: "#"},
		render.Tag("div", map[string]string{"class": "demo-stack"},
			ui.RangeSlider(ui.RangeSliderConfig{
				Name: "price", Label: "Price ($)", Min: 0, Max: 500,
				Step: 10, ValueLow: 80, ValueHigh: 320, ShowValue: true,
			}),
			ui.RangeSlider(ui.RangeSliderConfig{
				Name: "age", Label: "Age",
				Min: 0, Max: 100, ValueLow: 18, ValueHigh: 65, ShowValue: true,
			}),
		),
	)

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Range Slider")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Two overlaid <input type=\"range\"> elements. Native keyboard nav on each thumb; the runtime enforces min ≤ max via cross-clamp. Form-submit shape: Name+\"-min\" and Name+\"-max\" — server gets explicit lo/hi values.")),
		demoFrame(demo, `ui.RangeSlider(ui.RangeSliderConfig{
    Name: "price", Label: "Price ($)",
    Min: 0, Max: 500, Step: 10,
    ValueLow: 80, ValueHigh: 320,
    ShowValue: true,
})`),
	)
}
