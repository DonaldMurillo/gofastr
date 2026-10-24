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
	privacyScreenTextPrivacyPolicy           = i18n.Define("meridian.screens.privacy.privacy_policy", "Privacy Policy")
	privacyScreenTextPrivacyPolicyMeridianIs = i18n.Define("meridian.screens.privacy.privacy_policy_meridian_is", "# Privacy Policy\n\nMeridian is a demo and stores only the sample data you create while exploring it. The content below is placeholder Markdown.\n\n## What we collect\n\nNothing personal. A demo account and any records you add in the console, all reset periodically.\n\n## What we share\n\nNothing. There are no third parties, trackers, or analytics in this demonstration app.")
)

type PrivacyScreen struct{ component.ContextOnly }

func (s *PrivacyScreen) ScreenTitle(ctx context.Context) string {
	return privacyScreenTextPrivacyPolicy.Text(ctx)
}
func (s *PrivacyScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *PrivacyScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }

func (s *PrivacyScreen) RenderCtx(ctx context.Context) render.HTML {
	return html.Div(html.DivConfig{}, ui.Markdown(ui.MarkdownConfig{Source: privacyScreenTextPrivacyPolicyMeridianIs.Text(ctx), Measure: true}))
}

// mountPrivacyScreen mounts the privacy screen with site.
func mountPrivacyScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/privacy", &PrivacyScreen{}, marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 4, fn: mountPrivacyScreen})
}
