package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type CommandPaletteScreen struct{}

func (s *CommandPaletteScreen) ScreenTitle() string {
	return "Command Palette"
}
func (s *CommandPaletteScreen) ScreenDescription() string {
	return "Ctrl/Cmd+K overlay combining Modal preset with combobox search."
}
func (s *CommandPaletteScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CommandPaletteScreen) Render() render.HTML {
	// Visible trigger so people can click without relying on the chord.
	// The palette modal is mounted in new_components_demos.go scoped to
	// /components/commandpalette.
	trigger := ui.Button(ui.ButtonConfig{
		Label:   "Open palette",
		Variant: ui.ButtonPrimary,
		ExtraAttrs: html.Attrs{
			"data-fui-open":           "demo-command-palette",
			"data-fui-shortcut-click": "Meta+K",
		},
	})

	hint := html.Paragraph(html.TextConfig{},
		render.Text("Or press "),
		ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "Mod+K"}),
		render.Text(" anywhere on this page."),
	)

	src := `trigger, paletteBuilder := ui.CommandPalette(ui.CommandPaletteConfig{
    Name:     "command-palette",
    RPCPath:  "/commands/search",
    Shortcut: "Meta+K",   // runtime accepts either Cmd or Ctrl
})
def := paletteBuilder.Build()
widget.Mount(r, &def)

// Render trigger in your global chrome (Sidebar, top nav).`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Command Palette")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A ⌘K-triggered modal combining a focus-trapped dialog with an always-open combobox. Server returns ranked options for each search; options can carry data-fui-rpc / data-fui-push-state so picking one navigates or fires an action.")),
		demoFrame(trigger, src),
		hint,
	)
}
