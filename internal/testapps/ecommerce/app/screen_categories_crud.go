//gofastr:generated generator=app kind=scaffold version=dev sum=b406c84a80ddb059

package main

import (
	"context"
	"net/http"
	"strings"

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
	fwApp.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				if r.URL.Path == "/api/categories" || strings.HasPrefix(r.URL.Path, "/api/categories/") {
					ui.InvalidateScreens(w, "/categories")
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	site.Register("/categories", &CategoriesScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 2, fn: mountCategoriesScreen},
	)
}
