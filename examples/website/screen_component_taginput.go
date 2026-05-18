package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type TagInputScreen struct{}

func (s *TagInputScreen) ScreenTitle() string { return "Tag Input" }
func (s *TagInputScreen) ScreenDescription() string {
	return "Free-form text → chips."
}
func (s *TagInputScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TagInputScreen) Render() render.HTML {
	demo := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.TagInput(ui.TagInputConfig{
			Name: "tags", Label: "Tags",
			Placeholder: "Type and press Enter…",
			Values:      []string{"go", "ssr", "ui"},
			Help:        "Enter or comma commits. Backspace removes the last on empty.",
			MaxLength:   24,
		}),
	)

	src := `ui.TagInput(ui.TagInputConfig{
    Name: "tags", Label: "Tags",
    Placeholder: "Type and press Enter…",
    Values:      []string{"go", "ssr", "ui"},
    MaxLength:   24,
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Tag Input")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Free-form chips. Type a tag, hit Enter or comma to commit; Backspace on empty removes the last tag. Each chip becomes its own <input type=hidden> sharing the same Name — the form submits the standard repeated-key pattern.")),
		demoFrame(demo, src),
	)
}
