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

type SignupScreen struct{ component.ContextOnly }

func (s *SignupScreen) ScreenTitle() string        { return "Create your account" }
func (s *SignupScreen) ScreenDescription() string  { return "" }
func (s *SignupScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SignupScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		ui.AuthCard(ui.AuthCardConfig{Title: "Create your Meridian account", Alert: authError(ctx), Body: ui.Form(ui.FormConfig{Action: "/auth/register", Method: "POST", SubmitLabel: "Create account"}, render.Raw("<input type=\"hidden\" name=\"next\" value=\"/app\">"), ui.FormField(ui.FormFieldConfig{Label: "Email", For: "auth-email", Required: true, Input: render.Raw("<input id=\"auth-email\" name=\"email\" type=\"email\" autocomplete=\"email\" required>")}), ui.FormField(ui.FormFieldConfig{Label: "Password", For: "auth-password", Required: true, Input: render.Raw("<input id=\"auth-password\" name=\"password\" type=\"password\" autocomplete=\"new-password\" required minlength=\"8\">")})), Footer: render.Raw("<a href=\"/login\">Already have an account? Sign in</a>")}),
	)
}

// mountSignupScreen mounts the signup screen with site.
func mountSignupScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/signup", &SignupScreen{}).WithTitle("Create your account").WithPolicy(guestPolicy("/app")), marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 6, fn: mountSignupScreen})
}
