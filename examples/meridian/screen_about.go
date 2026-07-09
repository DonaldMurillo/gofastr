package main

import (
	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
)

type AboutScreen struct{}

func (s *AboutScreen) ScreenTitle() string        { return "About Meridian" }
func (s *AboutScreen) ScreenDescription() string  { return "Why we built a calmer billing console." }
func (s *AboutScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AboutScreen) Render() render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("We think billing should feel calm.")),
		render.Tag("p", nil, render.Text("Meridian is a demonstration product built entirely from a GoFastr blueprint — a single declarative file that generates this marketing site, the authenticated console, auth, roles, and an admin back-office, all server-rendered.")),
		render.Tag("p", nil, render.Text("It exists to show that a framework can generate a real, polished web application — not a CRUD scaffold.")),
	)
}

// mountAboutScreen mounts the about screen with site.
func mountAboutScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/about", &AboutScreen{}, marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 2, fn: mountAboutScreen})
}
