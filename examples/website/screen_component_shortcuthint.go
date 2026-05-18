package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type ShortcutHintScreen struct{}

func (s *ShortcutHintScreen) ScreenTitle() string {
	return "Shortcut Hint"
}
func (s *ShortcutHintScreen) ScreenDescription() string {
	return "Keyboard chord as styled <kbd> chips with OS-correct Mod glyph."
}
func (s *ShortcutHintScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ShortcutHintScreen) Render() render.HTML {
	cmdK := html.Paragraph(html.TextConfig{},
		render.Text("Open command palette: "),
		ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "Mod+K"}),
	)
	slash := html.Paragraph(html.TextConfig{},
		render.Text("Focus search: "),
		ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "/"}),
	)
	combo := html.Paragraph(html.TextConfig{},
		render.Text("Toggle inspector: "),
		ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "Mod+Shift+I"}),
	)
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Shortcut Hint")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Visual chord chips next to triggers. The Mod key resolves to ⌘ on Mac / Ctrl elsewhere via <html data-fui-os>, set at runtime boot. Touch-only devices hide the hints automatically.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Mod+K")),
		demoFrame(cmdK, `ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "Mod+K"})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Single key")),
		demoFrame(slash, `ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "/"})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Multi-modifier")),
		demoFrame(combo, `ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "Mod+Shift+I"})`),
	)
}
