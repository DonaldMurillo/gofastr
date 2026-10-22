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
)

// The words this file says, in English. The app's catalog
// (locales/*.json) translates them by key.
var (
	signupScreenTextCreateYourAccount         = i18n.Define("meridian.screens.signup.create_your_account", "Create your account")
	signupScreenTextCreateAccount             = i18n.Define("meridian.screens.signup.create_account", "Create account")
	signupScreenTextAlreadyHaveAnAccount      = i18n.Define("meridian.screens.signup.already_have_an_account", "Already have an account? Sign in")
	signupScreenTextCreateYourMeridianAccount = i18n.Define("meridian.screens.signup.create_your_meridian_account", "Create your Meridian account")
)

type SignupScreen struct{ component.ContextOnly }

func (s *SignupScreen) ScreenTitle(ctx context.Context) string {
	return signupScreenTextCreateYourAccount.Text(ctx)
}
func (s *SignupScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *SignupScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }

func (s *SignupScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		ui.AuthCard(ui.AuthCardConfig{Title: signupScreenTextCreateYourMeridianAccount.Text(ctx), Alert: authError(ctx), Body: ui.Form(ui.FormConfig{Ctx: ctx, Action: "/auth/register", Method: "POST", SubmitLabel: signupScreenTextCreateAccount.Text(ctx), SubmitFullWidth: true}, html.Input(html.InputConfig{Type: "hidden", Name: "next", Value: "/app"}), ui.FormField(ui.FormFieldConfig{Label: i18nui.AuthEmail.Text(ctx), For: "auth-email", Required: true, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "email", AutoComplete: "email"})
		}}), ui.FormField(ui.FormFieldConfig{Label: i18nui.AuthPassword.Text(ctx), For: "auth-password", Required: true, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "password", Name: "password", AutoComplete: "new-password", MinLength: 8})
		}})), Footer: ui.Link(ui.LinkConfig{Href: "/login", Text: signupScreenTextAlreadyHaveAnAccount.Text(ctx)})}),
	)
}

// mountSignupScreen mounts the signup screen with site.
func mountSignupScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/signup", &SignupScreen{}).WithTitle("Create your account").WithPolicy(guestPolicy("/app")), marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 6, fn: mountSignupScreen})
}
