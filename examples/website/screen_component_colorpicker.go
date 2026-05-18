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

	// Multi-input form — pick up to three brand colors at once.
	threeColors := html.Form(html.FormConfig{Method: "post", Action: "#"},
		render.Tag("div", map[string]string{"class": "demo-stack"},
			ui.ColorPicker(ui.ColorPickerConfig{
				Name: "color-primary", Label: "Primary", Value: "#4F46E5",
			}),
			ui.ColorPicker(ui.ColorPickerConfig{
				Name: "color-accent", Label: "Accent", Value: "#10B981",
			}),
			ui.ColorPicker(ui.ColorPickerConfig{
				Name: "color-danger", Label: "Danger", Value: "#DC2626",
			}),
		),
	)

	disabled := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.ColorPicker(ui.ColorPickerConfig{
			Name:     "frozen",
			Label:    "Background (locked)",
			Value:    "#0F172A",
			Disabled: true,
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

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Multiple pickers in one form")),
		render.Tag("p", nil, render.Text(
			"Each picker submits its own Name. Click any swatch to open the native palette — keyboard nav inside the picker is the browser's responsibility (Enter to open, Esc to dismiss, Arrow keys inside).")),
		demoFrame(threeColors, `ui.ColorPicker(ui.ColorPickerConfig{Name: "color-primary", Label: "Primary", Value: "#4F46E5"})
ui.ColorPicker(ui.ColorPickerConfig{Name: "color-accent",  Label: "Accent",  Value: "#10B981"})
ui.ColorPicker(ui.ColorPickerConfig{Name: "color-danger",  Label: "Danger",  Value: "#DC2626"})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Disabled")),
		demoFrame(disabled, `ui.ColorPicker(ui.ColorPickerConfig{
    Name: "frozen", Label: "Background (locked)",
    Value: "#0F172A", Disabled: true,
})`),
	)
}
