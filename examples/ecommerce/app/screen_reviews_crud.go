package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type ReviewsScreen struct{ component.ContextOnly }

func (s *ReviewsScreen) ScreenTitle() string        { return "Reviews" }
func (s *ReviewsScreen) ScreenDescription() string  { return "Customer reviews and ratings" }
func (s *ReviewsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ReviewsScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Customer Reviews")),
		appUI.List("reviews").Columns("author_name", "rating", "title").PageSize(20).NoCreate().Heading("Latest Reviews", 2).Empty("No reviews yet.").RenderCtx(ctx),
	)
}

func mountReviewsScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/reviews", &ReviewsScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 4, fn: mountReviewsScreen},
	)
}
