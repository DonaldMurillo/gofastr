package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func ratingShapeRow(label string, shape ui.RatingShape) render.HTML {
	return render.Tag("div", map[string]string{"class": "demo-row-flex"},
		html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text(label)),
		ui.RatingInput(ui.RatingConfig{
			Name: "shape-" + string(shape), Label: label + " rating",
			Shape: shape, Value: 3,
		}),
	)
}

type RatingScreen struct{}

func (s *RatingScreen) ScreenTitle() string { return "Rating Input" }
func (s *RatingScreen) ScreenDescription() string {
	return "Keyboard-accessible 1-N star/heart rating."
}
func (s *RatingScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *RatingScreen) Render() render.HTML {
	stars := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.RatingInput(ui.RatingConfig{
			Name:  "stars",
			Label: "Rate this product",
			Value: 4,
		}),
	)

	hearts := html.Form(html.FormConfig{Method: "post", Action: "#"},
		ui.RatingInput(ui.RatingConfig{
			Name:  "love",
			Label: "How much did you love it?",
			Shape: ui.RatingShapeHeart,
			Max:   7,
		}),
	)

	// Three sizes side-by-side. Tap targets stay at the 44px floor for
	// all three; only the painted glyph shrinks or grows.
	sizes := render.Tag("div", map[string]string{"class": "demo-stack"},
		render.Tag("div", map[string]string{"class": "demo-row-flex"},
			html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Small")),
			ui.RatingInput(ui.RatingConfig{
				Name: "size-small", Label: "Small rating", Size: ui.RatingSizeSmall, Value: 3,
			}),
		),
		render.Tag("div", map[string]string{"class": "demo-row-flex"},
			html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Default")),
			ui.RatingInput(ui.RatingConfig{
				Name: "size-default", Label: "Default rating", Value: 3,
			}),
		),
		render.Tag("div", map[string]string{"class": "demo-row-flex"},
			html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Large")),
			ui.RatingInput(ui.RatingConfig{
				Name: "size-large", Label: "Large rating", Size: ui.RatingSizeLarge, Value: 3,
			}),
		),
	)

	src := `ui.RatingInput(ui.RatingConfig{
    Name:  "stars",
    Label: "Rate this product",
    Value: 4, // initial selection (0 = none)
})

// 7-heart rating
ui.RatingInput(ui.RatingConfig{
    Name:  "love",
    Label: "How much did you love it?",
    Shape: ui.RatingShapeHeart,
    Max:   7,
})`

	srcSizes := `ui.RatingInput(ui.RatingConfig{
    Name: "size-small",   Label: "Small rating",
    Size: ui.RatingSizeSmall,  Value: 3,
})
ui.RatingInput(ui.RatingConfig{
    Name: "size-default", Label: "Default rating",
    Value: 3,
})
ui.RatingInput(ui.RatingConfig{
    Name: "size-large",   Label: "Large rating",
    Size: ui.RatingSizeLarge,  Value: 3,
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Rating Input")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"5-star (or N-star, N-heart) rating bound to a hidden radio group. Browser handles arrow keys + Space/Enter via the native radio semantics — no JavaScript needed. Hover preview is pure CSS via :hover + ~ sibling selectors.")),
		demoFrame(stars, src),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Hearts (7-rating)")),
		demoFrame(hearts, `ui.RatingInput(ui.RatingConfig{
    Name: "love", Label: "How much did you love it?",
    Shape: ui.RatingShapeHeart, Max: 7,
})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Bundled shapes")),
		render.Tag("p", nil, render.Text(
			"Seven shapes ship in-component: star (default), heart, thumb, fire, diamond, circle, square. Heart and fire auto-tint danger; thumb tints primary; diamond tints info; the rest use the warning yellow.")),
		demoFrame(
			render.Tag("div", map[string]string{"class": "demo-stack"},
				ratingShapeRow("Star", ui.RatingShapeStar),
				ratingShapeRow("Heart", ui.RatingShapeHeart),
				ratingShapeRow("Thumb", ui.RatingShapeThumb),
				ratingShapeRow("Fire", ui.RatingShapeFire),
				ratingShapeRow("Diamond", ui.RatingShapeDiamond),
				ratingShapeRow("Circle", ui.RatingShapeCircle),
				ratingShapeRow("Square", ui.RatingShapeSquare),
			),
			`// Pick any bundled shape:
ui.RatingInput(ui.RatingConfig{Shape: ui.RatingShapeThumb, …})
ui.RatingInput(ui.RatingConfig{Shape: ui.RatingShapeFire,  …})
ui.RatingInput(ui.RatingConfig{Shape: ui.RatingShapeDiamond, …})`,
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Custom glyph (Icon)")),
		render.Tag("p", nil, render.Text(
			"Bring your own SVG via Icon. It overrides Shape and gets cloned into every star. Use currentColor for fill/stroke so the selected-state highlight still works.")),
		demoFrame(
			html.Form(html.FormConfig{Method: "post", Action: "#"},
				ui.RatingInput(ui.RatingConfig{
					Name: "moon-phase", Label: "Moon-phase rating", Max: 5, Value: 3,
					Icon: render.HTML(`<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z"/></svg>`),
				}),
			),
			`ui.RatingInput(ui.RatingConfig{
    Name: "moon-phase", Label: "Moon-phase rating",
    Icon: render.HTML(`+"`"+`<svg viewBox="0 0 24 24" fill="currentColor" …>`+"`"+`),
})`,
		),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Sizes")),
		render.Tag("p", nil, render.Text(
			"Size picks the painted glyph size. The tap target stays at the WCAG 2.5.5 floor (44×44) regardless, so even the Small variant remains touch-friendly.")),
		demoFrame(sizes, srcSizes),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Spacing")),
		render.Tag("p", nil, render.Text(
			"Gap controls the visual spacing between stars, independent of Size. Tight collapses the gap to 0 for inline density; Loose / Wide give the stars breathing room on detail pages.")),
		demoFrame(render.Tag("div", map[string]string{"class": "demo-stack"},
			render.Tag("div", map[string]string{"class": "demo-row-flex"},
				html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Tight")),
				ui.RatingInput(ui.RatingConfig{
					Name: "gap-tight", Label: "Tight rating", Gap: ui.RatingGapTight, Value: 3,
				}),
			),
			render.Tag("div", map[string]string{"class": "demo-row-flex"},
				html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Default")),
				ui.RatingInput(ui.RatingConfig{
					Name: "gap-default", Label: "Default rating", Value: 3,
				}),
			),
			render.Tag("div", map[string]string{"class": "demo-row-flex"},
				html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Loose")),
				ui.RatingInput(ui.RatingConfig{
					Name: "gap-loose", Label: "Loose rating", Gap: ui.RatingGapLoose, Value: 3,
				}),
			),
			render.Tag("div", map[string]string{"class": "demo-row-flex"},
				html.Span(html.TextConfig{Class: "demo-row-label"}, render.Text("Wide")),
				ui.RatingInput(ui.RatingConfig{
					Name: "gap-wide", Label: "Wide rating", Gap: ui.RatingGapWide, Value: 3,
				}),
			),
		), `ui.RatingInput(ui.RatingConfig{Name: "gap-tight",   Gap: ui.RatingGapTight,   …})
ui.RatingInput(ui.RatingConfig{Name: "gap-default", …})
ui.RatingInput(ui.RatingConfig{Name: "gap-loose",   Gap: ui.RatingGapLoose,   …})
ui.RatingInput(ui.RatingConfig{Name: "gap-wide",    Gap: ui.RatingGapWide,    …})`),
	)
}
