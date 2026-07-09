package main

import (
	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type TermsScreen struct{}

func (s *TermsScreen) ScreenTitle() string        { return "Terms of Service" }
func (s *TermsScreen) ScreenDescription() string  { return "" }
func (s *TermsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TermsScreen) Render() render.HTML {
	return render.Tag("div", nil,
		ui.Markdown(ui.MarkdownConfig{Source: "# Terms of Service\n\nThis is a demonstration application. The text below is placeholder content that shows long-form, **readable** typography rendered from Markdown in the marketing layout.\n\n## Acceptance\n\nBy using Meridian you agree these terms are illustrative only — there is no real service, billing, or obligation.\n\n## Use of the service\n\n- Evaluate Meridian freely for any purpose.\n- Sample data is reset periodically without notice.\n- Don't rely on it for anything that matters.\n\n## Liability\n\nMeridian is provided *as-is*, without warranty of any kind."}),
	)
}

// mountTermsScreen mounts the terms screen with site.
func mountTermsScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/terms", &TermsScreen{}, marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 3, fn: mountTermsScreen})
}
