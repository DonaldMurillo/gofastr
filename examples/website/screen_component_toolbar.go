package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type ToolbarScreen struct{}

func (s *ToolbarScreen) ScreenTitle() string { return "Toolbar" }
func (s *ToolbarScreen) ScreenDescription() string {
	return "Grouped action strip with role=toolbar."
}
func (s *ToolbarScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ToolbarScreen) Render() render.HTML {
	demo := ui.Toolbar(ui.ToolbarConfig{
		Label: "Document formatting",
		Groups: []ui.ToolbarGroup{
			{Label: "Inline", Children: []render.HTML{
				ui.Button(ui.ButtonConfig{Label: "Bold", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
				ui.Button(ui.ButtonConfig{Label: "Italic", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
				ui.Button(ui.ButtonConfig{Label: "Code", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
			}},
			{Label: "Block", Children: []render.HTML{
				ui.Button(ui.ButtonConfig{Label: "List", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
				ui.Button(ui.ButtonConfig{Label: "Quote", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
			}},
			{Label: "Actions", Children: []render.HTML{
				ui.Button(ui.ButtonConfig{Label: "Publish"}),
			}},
		},
	})

	src := `ui.Toolbar(ui.ToolbarConfig{
    Label: "Document formatting",
    Groups: []ui.ToolbarGroup{
        {Label: "Inline", Children: []render.HTML{
            ui.Button(ui.ButtonConfig{Label: "Bold",   Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
            ui.Button(ui.ButtonConfig{Label: "Italic", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
            ui.Button(ui.ButtonConfig{Label: "Code",   Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
        }},
        {Label: "Block", Children: []render.HTML{ … }},
        {Label: "Actions", Children: []render.HTML{
            ui.Button(ui.ButtonConfig{Label: "Publish"}),
        }},
    },
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Toolbar")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"role=\"toolbar\" wrapper with optional logical groups. Each group renders side-by-side with a thin separator; labeled groups become role=group + aria-label so AT users hear the structure.")),
		demoFrame(demo, src),
	)
}
