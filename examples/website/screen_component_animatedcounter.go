package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type AnimatedCounterScreen struct{}

func (s *AnimatedCounterScreen) ScreenTitle() string { return "Animated Counter" }
func (s *AnimatedCounterScreen) ScreenDescription() string {
	return "Number that ticks up on first scroll into view."
}
func (s *AnimatedCounterScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AnimatedCounterScreen) Render() render.HTML {
	demos := render.Tag("div", map[string]string{"class": "demo-stack-toast"},
		render.Tag("div", nil,
			html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Plain")),
			html.Heading(html.HeadingConfig{Level: 2},
				ui.AnimatedCounter(ui.AnimatedCounterConfig{To: 12483})),
		),
		render.Tag("div", nil,
			html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("With prefix + suffix")),
			html.Heading(html.HeadingConfig{Level: 2},
				ui.AnimatedCounter(ui.AnimatedCounterConfig{
					To: 99, Prefix: "$", Suffix: "k+", DurationMs: 1800,
				})),
		),
		render.Tag("div", nil,
			html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("From a starting value")),
			html.Heading(html.HeadingConfig{Level: 2},
				ui.AnimatedCounter(ui.AnimatedCounterConfig{
					From: 1000, To: 1234, DurationMs: 1500,
				})),
		),
	)

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Animated Counter")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Number that ticks from From to To over DurationMs. SSR renders the final value (no-JS + reduced-motion users see the target straight away); the runtime hooks an IntersectionObserver so the animation fires when the element scrolls into view, exactly once.")),
		demoFrame(demos, `ui.AnimatedCounter(ui.AnimatedCounterConfig{To: 12483})
ui.AnimatedCounter(ui.AnimatedCounterConfig{
    To: 99, Prefix: "$", Suffix: "k+", DurationMs: 1800,
})
ui.AnimatedCounter(ui.AnimatedCounterConfig{
    From: 1000, To: 1234,
})`),
	)
}
