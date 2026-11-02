//gofastr:generated generator=app kind=scaffold version=dev sum=0dea6e6df6d6cbfd

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
	categoriesCrudTextCategories              = i18n.Define("shopfront.screens.categories_crud.categories", "Categories")
	categoriesCrudTextBrowseProductCategories = i18n.Define("shopfront.screens.categories_crud.browse_product_categories", "Browse product categories")
	categoriesCrudTextAllCategories           = i18n.Define("shopfront.screens.categories_crud.all_categories", "All Categories")
	categoriesCrudTextNoCategoriesYet         = i18n.Define("shopfront.screens.categories_crud.no_categories_yet", "No categories yet.")
)

type CategoriesScreen struct{ component.ContextOnly }

func (s *CategoriesScreen) ScreenTitle(ctx context.Context) string {
	return categoriesCrudTextCategories.Text(ctx)
}
func (s *CategoriesScreen) ScreenDescription(ctx context.Context) string {
	return categoriesCrudTextBrowseProductCategories.Text(ctx)
}
func (s *CategoriesScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CategoriesScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text(categoriesCrudTextCategories.Text(ctx))),
		appUI.List("categories").Columns("name", "description", "active").PageSize(50).NoCreate().NoLinks().Heading(categoriesCrudTextAllCategories.Text(ctx), 2).Empty(categoriesCrudTextNoCategoriesYet.Text(ctx)).RenderCtx(ctx),
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
