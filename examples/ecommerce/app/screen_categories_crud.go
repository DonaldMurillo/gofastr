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

type CategoriesScreen struct{ component.ContextOnly }

func (s *CategoriesScreen) ScreenTitle() string        { return "Categories" }
func (s *CategoriesScreen) ScreenDescription() string  { return "Browse product categories" }
func (s *CategoriesScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CategoriesScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Categories")),
		appResources["categories"].WithColumns("name", "description", "active").WithLimit(50).WithHeading("All Categories").WithEmpty("No categories yet.").List(ctx),
	)
}

func mountCategoriesScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	appResources["categories"] = ResourceConfig{
		Title: "Categories", Singular: "Category", BasePath: "/categories", APIPath: "/api/categories",
		Crud: fwApp.MustCrudHandler("categories"),
		Fields: []ResField{
			{Key: "name", Label: "Name", Type: "string"},
			{Key: "slug", Label: "Slug", Type: "string"},
			{Key: "description", Label: "Description", Type: "text"},
			{Key: "image", Label: "Image", Type: "image"},
			{Key: "sort_order", Label: "Sort Order", Type: "int"},
			{Key: "active", Label: "Active", Type: "bool"},
		},
		Related: []RelatedList{
			{
				Title: "Products", ForeignKey: "category_id", BasePath: "/products",
				Crud: fwApp.MustCrudHandler("products"),
				Fields: []ResField{
					{Key: "name", Label: "Name", Type: "string"},
					{Key: "slug", Label: "Slug", Type: "string"},
					{Key: "sku", Label: "SKU", Type: "string"},
					{Key: "description", Label: "Description", Type: "text"},
				},
				Relations: map[string]RelSource{
					"category_id": {Crud: fwApp.MustCrudHandler("categories"), Display: "name"},
				},
			},
		},
	}
	site.Register("/categories", &CategoriesScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 2, fn: mountCategoriesScreen},
	)
}
