package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type LoginScreen struct{ component.ContextOnly }

func (s *LoginScreen) ScreenTitle() string        { return "Sign in" }
func (s *LoginScreen) ScreenDescription() string  { return "" }
func (s *LoginScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *LoginScreen) RenderCtx(ctx context.Context) render.HTML {
	return html.Div(html.DivConfig{},
		ui.AuthCard(ui.AuthCardConfig{Title: "Sign in to Meridian", Alert: authError(ctx), Body: ui.Form(ui.FormConfig{Action: "/auth/login", Method: "POST", SubmitLabel: "Sign in"}, render.Raw("<input type=\"hidden\" name=\"next\" value=\"/app\">"), ui.FormField(ui.FormFieldConfig{Label: "Email", For: "auth-email", Required: true, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "email", AutoComplete: "email"})
		}}), ui.FormField(ui.FormFieldConfig{Label: "Password", For: "auth-password", Required: true, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "password", Name: "password", AutoComplete: "current-password"})
		}})), Footer: ui.Link(ui.LinkConfig{Href: "/signup", Text: "Create an account"})}),
	)
}

// mountLoginScreen mounts the login screen with site.
func mountLoginScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/login", &LoginScreen{}).WithTitle("Sign in").WithPolicy(guestPolicy("/app")), marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 5, fn: mountLoginScreen})
}
