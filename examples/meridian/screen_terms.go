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
	termsScreenTextTermsOfService     = i18n.Define("meridian.screens.terms.terms_of_service", "Terms of Service")
	termsScreenTextTermsOfServiceThis = i18n.Define("meridian.screens.terms.terms_of_service_this", "# Terms of Service\n\nThis is a demonstration application. The text below is placeholder content that shows long-form, **readable** typography rendered from Markdown in the marketing layout.\n\n## Acceptance\n\nBy using Meridian you agree these terms are illustrative only: there is no real service, billing, or obligation.\n\n## Use of the service\n\n- Evaluate Meridian freely for any purpose.\n- Sample data is reset periodically without notice.\n- Don't rely on it for anything that matters.\n\n## Liability\n\nMeridian is provided *as-is*, without warranty of any kind.")
)

type TermsScreen struct{ component.ContextOnly }

func (s *TermsScreen) ScreenTitle(ctx context.Context) string {
	return termsScreenTextTermsOfService.Text(ctx)
}
func (s *TermsScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *TermsScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }

func (s *TermsScreen) RenderCtx(ctx context.Context) render.HTML {
	return html.Div(html.DivConfig{}, ui.Markdown(ui.MarkdownConfig{Source: termsScreenTextTermsOfServiceThis.Text(ctx), Measure: true}))
}

// mountTermsScreen mounts the terms screen with site.
func mountTermsScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/terms", &TermsScreen{}, marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 3, fn: mountTermsScreen})
}
