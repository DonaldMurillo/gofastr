//gofastr:generated generator=app kind=scaffold version=dev sum=f6805696c356e940

package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The words this file says, in English. The app's catalog
// (locales/*.json) translates them by key.
var (
	reviewsCrudTextReviews                   = i18n.Define("shopfront.screens.reviews_crud.reviews", "Reviews")
	reviewsCrudTextCustomerReviewsAndRatings = i18n.Define("shopfront.screens.reviews_crud.customer_reviews_and_ratings", "Customer reviews and ratings")
	reviewsCrudTextCustomerReviews           = i18n.Define("shopfront.screens.reviews_crud.customer_reviews", "Customer Reviews")
	reviewsCrudTextLatestReviews             = i18n.Define("shopfront.screens.reviews_crud.latest_reviews", "Latest Reviews")
	reviewsCrudTextNoReviewsYet              = i18n.Define("shopfront.screens.reviews_crud.no_reviews_yet", "No reviews yet.")
)

type ReviewsScreen struct{ component.ContextOnly }

func (s *ReviewsScreen) ScreenTitle(ctx context.Context) string {
	return reviewsCrudTextReviews.Text(ctx)
}
func (s *ReviewsScreen) ScreenDescription(ctx context.Context) string {
	return reviewsCrudTextCustomerReviewsAndRatings.Text(ctx)
}
func (s *ReviewsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ReviewsScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text(reviewsCrudTextCustomerReviews.Text(ctx))),
		appUI.List("reviews").Columns("author_name", "rating", "title").PageSize(20).NoCreate().NoLinks().Heading(reviewsCrudTextLatestReviews.Text(ctx), 2).Empty(reviewsCrudTextNoReviewsYet.Text(ctx)).RenderCtx(ctx),
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
