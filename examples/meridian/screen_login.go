package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type LoginScreen struct{ component.ContextOnly }

func (s *LoginScreen) ScreenTitle() string        { return "Sign in" }
func (s *LoginScreen) ScreenDescription() string  { return "" }
func (s *LoginScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *LoginScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		ui.AuthCard(ui.AuthCardConfig{Title: "Sign in to Meridian", Alert: authError(ctx), Body: ui.Form(ui.FormConfig{Action: "/auth/login", Method: "POST", SubmitLabel: "Sign in"}, render.Raw("<input type=\"hidden\" name=\"next\" value=\"/app\">"), ui.FormField(ui.FormFieldConfig{Label: "Email", For: "auth-email", Required: true, Input: render.Raw("<input id=\"auth-email\" name=\"email\" type=\"email\" autocomplete=\"email\" required>")}), ui.FormField(ui.FormFieldConfig{Label: "Password", For: "auth-password", Required: true, Input: render.Raw("<input id=\"auth-password\" name=\"password\" type=\"password\" autocomplete=\"current-password\" required>")})), Footer: render.Raw("<a href=\"/signup\">Create an account</a>")}),
	)
}

// mountLoginScreen mounts the login screen with site.
func mountLoginScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/login", &LoginScreen{}).WithTitle("Sign in").WithPolicy(guestPolicy("/app")), marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 5, fn: mountLoginScreen})
}
