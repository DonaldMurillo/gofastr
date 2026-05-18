package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type KbdScreen struct{}

func (s *KbdScreen) ScreenTitle() string        { return "Kbd" }
func (s *KbdScreen) ScreenDescription() string  { return "Semantic <kbd> primitive for keyboard input." }
func (s *KbdScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *KbdScreen) Render() render.HTML {
	demo := html.Paragraph(html.TextConfig{},
		render.Text("Press "),
		html.Kbd(html.TextConfig{}, render.Text("Esc")),
		render.Text(" to dismiss or "),
		html.Kbd(html.TextConfig{}, render.Text("/")),
		render.Text(" to focus search."),
	)
	src := `html.Paragraph(html.TextConfig{},
    render.Text("Press "),
    html.Kbd(html.TextConfig{}, render.Text("Esc")),
    render.Text(" to dismiss."),
)`
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Kbd")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Renders a native <kbd> element. Pair with framework/ui.ShortcutHint for styled chord chips, or use inline for documentation prose.")),
		demoFrame(demo, src),
	)
}
