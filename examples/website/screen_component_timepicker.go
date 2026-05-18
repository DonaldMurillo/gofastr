package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type TimePickerScreen struct{}

func (s *TimePickerScreen) ScreenTitle() string { return "Time Picker" }
func (s *TimePickerScreen) ScreenDescription() string {
	return "Styled native time input."
}
func (s *TimePickerScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TimePickerScreen) Render() render.HTML {
	demo := html.Form(html.FormConfig{Method: "post", Action: "#"},
		render.Tag("div", map[string]string{"class": "demo-stack"},
			ui.TimePicker(ui.TimePickerConfig{Name: "wake", Label: "Wake at", Value: "07:30"}),
			ui.TimePicker(ui.TimePickerConfig{
				Name: "meeting", Label: "Meeting", Value: "14:00",
				Min: "09:00", Max: "17:00", Help: "Office hours only (09:00–17:00).",
			}),
		),
	)

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Time Picker")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Native <input type=\"time\"> wrapped with theme chrome. Browser handles the actual picker UI; we own the label, 44×44 touch target, focus ring, and error state. Pairs with the deferred Calendar Date Picker.")),
		demoFrame(demo, `ui.TimePicker(ui.TimePickerConfig{
    Name: "meeting", Label: "Meeting",
    Min: "09:00", Max: "17:00", Value: "14:00",
})`),
	)
}
