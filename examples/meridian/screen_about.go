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
	aboutScreenTextAboutMeridian            = i18n.Define("meridian.screens.about.about_meridian", "About Meridian")
	aboutScreenTextWhyWeBuiltA              = i18n.Define("meridian.screens.about.why_we_built_a", "Why we built a calmer billing console.")
	aboutScreenTextWeThinkBillingShould     = i18n.Define("meridian.screens.about.we_think_billing_should", "We think billing should feel calm.")
	aboutScreenTextMeridianIsADemonstration = i18n.Define("meridian.screens.about.meridian_is_a_demonstration", "Meridian is a demonstration product built entirely from a GoFastr app spec, a single declarative file that generates this marketing site, the authenticated console, auth, roles, and an admin back-office, all server-rendered.\n\nIt exists to show that a framework can generate a real, polished web application, not a CRUD scaffold.")
)

type AboutScreen struct{ component.ContextOnly }

func (s *AboutScreen) ScreenTitle(ctx context.Context) string {
	return aboutScreenTextAboutMeridian.Text(ctx)
}
func (s *AboutScreen) ScreenDescription(ctx context.Context) string {
	return aboutScreenTextWhyWeBuiltA.Text(ctx)
}
func (s *AboutScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AboutScreen) RenderCtx(ctx context.Context) render.HTML {
	// The screen owns its rhythm: a PageHeader for the heading, prose
	// through ui.Markdown so it keeps a reading measure and its own
	// paragraph spacing — the page frame supplies only the page measure.
	// html.Div root: the pack reader (reverseRenderBody) accepts a
	// tag-primitive root; the stack inside owns the rhythm.
	return html.Div(html.DivConfig{}, ui.Stack(ui.StackConfig{Gap: ui.Gap2XL},
		ui.PageHeader(ui.PageHeaderConfig{Title: aboutScreenTextWeThinkBillingShould.Text(ctx)}),
		ui.Markdown(ui.MarkdownConfig{Source: aboutScreenTextMeridianIsADemonstration.Text(ctx), Measure: true}),
	))
}

// mountAboutScreen mounts the about screen with site.
func mountAboutScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/about", &AboutScreen{}, marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 2, fn: mountAboutScreen})
}
