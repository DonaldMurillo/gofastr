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

type CategoriesScreen struct{ component.ContextOnly }

func (s *CategoriesScreen) ScreenTitle() string        { return "Categories" }
func (s *CategoriesScreen) ScreenDescription() string  { return "Browse product categories" }
func (s *CategoriesScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CategoriesScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Categories")),
		appUI.List("categories").Columns("name", "description", "active").PageSize(50).NoCreate().Heading("All Categories", 2).Empty("No categories yet.").RenderCtx(ctx),
	)
}

func mountCategoriesScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/categories", &CategoriesScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 2, fn: mountCategoriesScreen},
	)
}
