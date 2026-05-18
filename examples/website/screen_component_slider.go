package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type SliderScreen struct{}

func (s *SliderScreen) ScreenTitle() string { return "Slider" }
func (s *SliderScreen) ScreenDescription() string {
	return "Styled range input."
}
func (s *SliderScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SliderScreen) Render() render.HTML {
	basic := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.Slider(ui.SliderConfig{
			Name: "volume", Label: "Volume", Min: 0, Max: 100, Value: 60,
			ShowValue:      true,
			ShowEdgeLabels: true,
		}),
	)

	stepped := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.Slider(ui.SliderConfig{
			Name: "zoom", Label: "Zoom (%)", Min: 50, Max: 200, Step: 10,
			Value: 100, ShowValue: true,
		}),
	)

	src := `ui.Slider(ui.SliderConfig{
    Name: "volume", Label: "Volume",
    Min: 0, Max: 100, Value: 60,
    ShowValue:      true, // <output> tracks the thumb live
    ShowEdgeLabels: true, // tiny Min / Max labels under track
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Slider")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Styled <input type=\"range\"> with optional live value display and Min/Max edge labels. Native keyboard support (Arrow keys, PageUp/Down, Home/End); ShowValue uses a small runtime module to mirror the live value into an <output> next to the label.")),
		demoFrame(basic, src),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("With step granularity")),
		demoFrame(stepped, `ui.Slider(ui.SliderConfig{
    Name: "zoom", Label: "Zoom (%)",
    Min: 50, Max: 200, Step: 10, Value: 100,
    ShowValue: true,
})`),
	)
}
