package main

import (
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type AboutScreen struct{}

func (s *AboutScreen) ScreenTitle() string        { return "About Meridian" }
func (s *AboutScreen) ScreenDescription() string  { return "Why we built a calmer billing console." }
func (s *AboutScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AboutScreen) Render() render.HTML {
	// The screen owns its rhythm: a PageHeader for the heading, prose
	// through ui.Markdown so it keeps a reading measure and its own
	// paragraph spacing — the page frame supplies only the page measure.
	// html.Div root: the pack reader (reverseRenderBody) accepts a
	// tag-primitive root; the stack inside owns the rhythm.
	return html.Div(html.DivConfig{}, ui.Stack(ui.StackConfig{Gap: ui.Gap2XL},
		ui.PageHeader(ui.PageHeaderConfig{Title: "We think billing should feel calm."}),
		ui.Markdown(ui.MarkdownConfig{Source: "Meridian is a demonstration product built entirely from a GoFastr blueprint, a single declarative file that generates this marketing site, the authenticated console, auth, roles, and an admin back-office, all server-rendered.\n\nIt exists to show that a framework can generate a real, polished web application, not a CRUD scaffold.", Measure: true}),
	))
}

// mountAboutScreen mounts the about screen with site.
func mountAboutScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/about", &AboutScreen{}, marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 2, fn: mountAboutScreen})
}
