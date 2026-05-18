package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

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
	)
}
