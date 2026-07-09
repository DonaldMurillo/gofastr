package main

import (
	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type PrivacyScreen struct{}

func (s *PrivacyScreen) ScreenTitle() string        { return "Privacy Policy" }
func (s *PrivacyScreen) ScreenDescription() string  { return "" }
func (s *PrivacyScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *PrivacyScreen) Render() render.HTML {
	return render.Tag("div", nil,
		ui.Markdown(ui.MarkdownConfig{Source: "# Privacy Policy\n\nMeridian is a demo and stores only the sample data you create while exploring it. The content below is placeholder Markdown.\n\n## What we collect\n\nNothing personal. A demo account and any records you add in the console — all reset periodically.\n\n## What we share\n\nNothing. There are no third parties, trackers, or analytics in this demonstration app."}),
	)
}

// mountPrivacyScreen mounts the privacy screen with site.
func mountPrivacyScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/privacy", &PrivacyScreen{}, marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 4, fn: mountPrivacyScreen})
}
