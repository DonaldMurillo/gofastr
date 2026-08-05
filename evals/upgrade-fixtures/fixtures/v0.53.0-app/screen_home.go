package main

import (
	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
)

type HomeScreen struct{}

func (s *HomeScreen) ScreenTitle() string        { return "Upgrade Fixture" }
func (s *HomeScreen) ScreenDescription() string  { return "Upgrade fixture home" }
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *HomeScreen) Render() render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Upgrade Fixture")),
		render.Tag("p", nil, render.Text("A generated app used to prove the upgrade path.")),
	)
}

// mountHomeScreen mounts the home screen with site.
func mountHomeScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/", &HomeScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 0, fn: mountHomeScreen})
}
