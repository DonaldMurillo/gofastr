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

type TagsScreen struct{ component.ContextOnly }

func (s *TagsScreen) ScreenTitle() string        { return "Tags" }
func (s *TagsScreen) ScreenDescription() string  { return "All tags" }
func (s *TagsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TagsScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Tags")),
		appResources["tags"].WithColumns("name").WithLimit(50).WithHeading("All Tags").WithEmpty("No tags yet.").List(ctx),
	)
}

func mountTagsScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	appResources["tags"] = ResourceConfig{
		Title: "Tags", Singular: "Tag", BasePath: "/tags", APIPath: "/api/tags",
		Crud: fwApp.MustCrudHandler("tags"),
		Fields: []ResField{
			{Key: "name", Label: "Name", Type: "string"},
		},
	}
	site.Register("/tags", &TagsScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 2, fn: mountTagsScreen},
	)
}
