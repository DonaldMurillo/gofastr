package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type NumberInputScreen struct{}

func (s *NumberInputScreen) ScreenTitle() string { return "Number Input" }
func (s *NumberInputScreen) ScreenDescription() string {
	return "Number field with explicit +/- stepper buttons."
}
func (s *NumberInputScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *NumberInputScreen) Render() render.HTML {
	basic := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.NumberInput(ui.NumberInputConfig{
			Name: "qty", Label: "Quantity", Min: 1, Max: 99, Value: 1,
			Help: "1 to 99",
		}),
	)

	stepped := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.NumberInput(ui.NumberInputConfig{
			Name: "year", Label: "Year", Min: 1900, Max: 2100, Step: 1, Value: 2026,
		}),
	)

	errorState := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.NumberInput(ui.NumberInputConfig{
			Name: "rate", Label: "Rate (%)", Min: 0, Max: 100, Value: 150,
			Error: "Rate must be between 0 and 100.",
		}),
	)

	src := `ui.NumberInput(ui.NumberInputConfig{
    Name: "qty", Label: "Quantity",
    Min: 1, Max: 99, Value: 1,
    Help: "1 to 99",
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Number Input")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Native <input type=\"number\"> flanked by explicit +/- buttons. Browser spinner arrows are tiny and disappear on touch; explicit buttons are easier to hit and themeable. The runtime increments by Step, clamps to Min/Max, and dispatches an input event so form-RPC pipelines see the change.")),
		demoFrame(basic, src),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Stepped (year picker)")),
		demoFrame(stepped, `ui.NumberInput(ui.NumberInputConfig{
    Name: "year", Label: "Year",
    Min: 1900, Max: 2100, Step: 1, Value: 2026,
})`),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Error state")),
		demoFrame(errorState, `ui.NumberInput(ui.NumberInputConfig{
    Name: "rate", Label: "Rate (%)",
    Min: 0, Max: 100, Value: 150,
    Error: "Rate must be between 0 and 100.",
})`),
	)
}
