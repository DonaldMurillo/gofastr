package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
)

type ReviewsScreen struct{ component.ContextOnly }

func (s *ReviewsScreen) ScreenTitle() string        { return "Reviews" }
func (s *ReviewsScreen) ScreenDescription() string  { return "Customer reviews and ratings" }
func (s *ReviewsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ReviewsScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Customer Reviews")),
		appResources["reviews"].WithColumns("author_name", "rating", "title").WithLimit(20).WithHeading("Latest Reviews").WithEmpty("No reviews yet.").List(ctx),
	)
}

func mountReviewsScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	appResources["reviews"] = ResourceConfig{
		Title: "Reviews", Singular: "Review", BasePath: "/reviews", APIPath: "/api/reviews",
		Crud: fwApp.MustCrudHandler("reviews"),
		Fields: []ResField{
			{Key: "product_id", Label: "Product", Type: "relation"},
			{Key: "author_name", Label: "Author Name", Type: "string"},
			{Key: "rating", Label: "Rating", Type: "int"},
			{Key: "title", Label: "Title", Type: "string"},
			{Key: "body", Label: "Body", Type: "text"},
			{Key: "verified", Label: "Verified", Type: "bool"},
		},
		Relations: map[string]RelSource{
			"product_id": {Crud: fwApp.MustCrudHandler("products"), Display: "name"},
		},
	}
	site.Register("/reviews", &ReviewsScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 4, fn: mountReviewsScreen},
	)
}
