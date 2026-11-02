//gofastr:generated generator=app kind=scaffold version=dev sum=252c13b073067f8f

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
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The words this file says, in English. The app's catalog
// (locales/*.json) translates them by key.
var (
	loginScreenTextSignIn = i18n.Define("shopfront.screens.login.sign_in", "Sign in")
)

type LoginScreen struct{ component.ContextOnly }

func (s *LoginScreen) ScreenTitle(ctx context.Context) string   { return loginScreenTextSignIn.Text(ctx) }
func (s *LoginScreen) ScreenSEO(ctx context.Context) uihost.SEO { return uihost.SEO{} } // deliberate SEO opt-out: set description in the app spec or replace with real copy
func (s *LoginScreen) ScreenType() app.ScreenType               { return app.ScreenPage }

func (s *LoginScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		ui.AuthCard(ui.AuthCardConfig{Title: loginScreenTextSignIn.Text(ctx), Alert: authError(ctx), Body: ui.Form(ui.FormConfig{Ctx: ctx, Action: "/auth/login", Method: "POST", SubmitLabel: loginScreenTextSignIn.Text(ctx), SubmitFullWidth: true}, html.Input(html.InputConfig{Type: "hidden", Name: "next", Value: "/"}), ui.FormField(ui.FormFieldConfig{Label: i18nui.AuthEmail.Text(ctx), For: "auth-email", Required: true, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "email", AutoComplete: "email"})
		}}), ui.FormField(ui.FormFieldConfig{Label: i18nui.AuthPassword.Text(ctx), For: "auth-password", Required: true, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "password", Name: "password", AutoComplete: "current-password"})
		}}))}),
	)
}

// mountLoginScreen mounts the login screen with site.
func mountLoginScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/login", &LoginScreen{}).WithTitle("Sign in").WithPolicy(guestPolicy("/")), appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 8, fn: mountLoginScreen})
}
