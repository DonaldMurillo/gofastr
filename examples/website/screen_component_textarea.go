package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type TextAreaScreen struct{}

func (s *TextAreaScreen) ScreenTitle() string { return "Text Area" }
func (s *TextAreaScreen) ScreenDescription() string {
	return "Multi-line text input with optional autogrow."
}
func (s *TextAreaScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TextAreaScreen) Render() render.HTML {
	basic := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.TextArea(ui.TextAreaConfig{
			Name: "bio", Label: "Bio",
			Placeholder: "Tell us about yourself…",
			Rows:        3,
			Help:        "A few sentences is plenty.",
		}),
	)

	autogrow := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.TextArea(ui.TextAreaConfig{
			Name: "notes", Label: "Notes",
			Placeholder: "Type and watch the field grow…",
			Rows:        2,
			Autogrow:    true,
		}),
	)

	withError := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.TextArea(ui.TextAreaConfig{
			Name: "feedback", Label: "Feedback",
			Value: "TLDR",
			Error: "Feedback must be at least 20 characters.",
		}),
	)

	src := `ui.TextArea(ui.TextAreaConfig{
    Name: "bio", Label: "Bio",
    Placeholder: "Tell us about yourself…",
    Rows: 3,
    Help: "A few sentences is plenty.",
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Text Area")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Labelled multi-line text input. Native <textarea> under the hood; framework adds Help/Error states, error styling, and the typed Autogrow toggle that hooks into the runtime's data-fui-autogrow primitive.")),
		demoFrame(basic, src),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Autogrow")),
		render.Tag("p", nil, render.Text(
			"Setting Autogrow: true wires the textarea to the runtime auto-resize handler — every input event resets the height to fit content, so the field always shows the full message without an internal scrollbar.")),
		demoFrame(autogrow, `ui.TextArea(ui.TextAreaConfig{
    Name: "notes", Label: "Notes",
    Autogrow: true,
})`),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Error state")),
		demoFrame(withError, `ui.TextArea(ui.TextAreaConfig{
    Name: "feedback", Label: "Feedback",
    Value: "TLDR",
    Error: "Feedback must be at least 20 characters.",
})`),
	)
}
