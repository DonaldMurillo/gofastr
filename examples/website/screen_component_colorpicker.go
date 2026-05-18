package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type ColorPickerScreen struct{}

func (s *ColorPickerScreen) ScreenTitle() string { return "Color Picker" }
func (s *ColorPickerScreen) ScreenDescription() string {
	return "Styled native color input."
}
func (s *ColorPickerScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ColorPickerScreen) Render() render.HTML {
	picker := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.ColorPicker(ui.ColorPickerConfig{
			Name:  "brand-primary",
			Label: "Brand primary",
			Value: "#4F46E5",
		}),
	)

	src := `ui.ColorPicker(ui.ColorPickerConfig{
    Name:  "brand-primary",
    Label: "Brand primary",
    Value: "#4F46E5",
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Color Picker")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Native <input type=\"color\"> wrapped with theme-aware chrome. The browser handles the actual color UI; we own the label, sizing (44×44 touch target), and focus ring. Preset-swatches strip is on the roadmap.")),
		demoFrame(picker, src),
	)
}
