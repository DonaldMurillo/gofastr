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

type TasksScreen struct{ component.ContextOnly }

func (s *TasksScreen) ScreenTitle() string        { return "Tasks" }
func (s *TasksScreen) ScreenDescription() string  { return "My tasks" }
func (s *TasksScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TasksScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Tasks")),
		appResources["tasks"].WithColumns("title", "done").WithLimit(20).WithHeading("My Tasks").WithEmpty("No tasks yet.").List(ctx),
	)
}

func mountTasksScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	appResources["tasks"] = ResourceConfig{
		Title: "Tasks", Singular: "Task", BasePath: "/tasks", APIPath: "/api/tasks",
		Crud: fwApp.MustCrudHandler("tasks"),
		Fields: []ResField{
			{Key: "title", Label: "Title", Type: "string"},
			{Key: "done", Label: "Done", Type: "bool"},
			{Key: "user_id", Label: "User", Type: "relation"},
		},
		Relations: map[string]RelSource{
			"user_id": {Crud: fwApp.MustCrudHandler("users"), Display: "name"},
		},
	}
	site.Register("/tasks", &TasksScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 1, fn: mountTasksScreen},
	)
}
