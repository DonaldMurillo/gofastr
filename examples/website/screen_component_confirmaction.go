package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type ConfirmActionScreen struct{}

func (s *ConfirmActionScreen) ScreenTitle() string {
	return "Confirm Action"
}
func (s *ConfirmActionScreen) ScreenDescription() string {
	return "Trigger + alertdialog Modal preset for destructive confirmations."
}
func (s *ConfirmActionScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ConfirmActionScreen) Render() render.HTML {
	// Live trigger — the modal preset is mounted in new_components_demos.go
	// scoped to /components/confirmaction. We render the trigger via
	// ui.Button so the demo uses the same framework component the
	// production API generates.
	trigger := ui.Button(ui.ButtonConfig{
		Label:   "Delete account",
		Variant: ui.ButtonDanger,
		Attrs: html.Attrs{
			"data-fui-open": "demo-confirm-delete",
		},
	})

	src := `trigger, modalBuilder := ui.ConfirmAction(ui.ConfirmActionConfig{
    Name:         "delete-user-42",
    TriggerLabel: "Delete account",
    Title:        "Delete account?",
    Body:         "This permanently removes the user and their data.",
    RPCPath:      "/users/42/delete",
})
def := modalBuilder.Build()
widget.Mount(r, &def)
// render trigger inline anywhere`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Confirm Action")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A declarative pair: trigger button + alertdialog Modal. Cancel autofocuses by default (safer for destructive flows — accidental Enter doesn't fire). Escape closes; backdrop click closes; focus returns to the trigger. role=\"alertdialog\" + aria-labelledby + aria-describedby for AT.")),
		demoFrame(trigger, src),
	)
}
