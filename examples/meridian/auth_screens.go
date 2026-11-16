package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The password reset and second-factor screens, hand-maintained (the
// app spec has no block for them, so they live outside screen_*.go,
// which gofastr pack reads back). Each is an AuthCard over a plain HTML
// form that posts to battery/auth and works with JS off.

var (
	forgotTitle      = i18n.Define("meridian.screens.forgot.title", "Reset your password")
	forgotSubmit     = i18n.Define("meridian.screens.forgot.submit", "Email me a reset link")
	forgotSent       = i18n.Define("meridian.screens.forgot.sent", "If an account uses that address, a link to reset its password is on its way.")
	forgotBack       = i18n.Define("meridian.screens.forgot.back", "Back to sign in")
	resetTitle       = i18n.Define("meridian.screens.reset.title", "Choose a new password")
	resetSubmit      = i18n.Define("meridian.screens.reset.submit", "Set password")
	resetNoToken     = i18n.Define("meridian.screens.reset.no_token", "This link is incomplete. Ask for a new one.")
	resetDone        = i18n.Define("meridian.screens.reset.done", "Password changed. Sign in with the new one.")
	twoFactorTitle   = i18n.Define("meridian.screens.two_factor.title", "Enter your authentication code")
	twoFactorSubmit  = i18n.Define("meridian.screens.two_factor.submit", "Verify")
	twoFactorHelp    = i18n.Define("meridian.screens.two_factor.help", "The 6-digit code from your authenticator app.")
	authErrBadToken  = i18n.Define("meridian.auth.invalid_token", "This reset link has expired or was already used. Ask for a new one.")
	authErrBadCode   = i18n.Define("meridian.auth.invalid_code", "That code did not match. Try the current one from your app.")
	authErrWeakPass  = i18n.Define("meridian.auth.weak_password", "Use a password of at least 8 characters.")
	authErrSignAgain = i18n.Define("meridian.auth.not_authenticated", "Your sign-in timed out. Sign in again.")
)

// ForgotPasswordScreen asks for the address to send a reset link to. The
// battery answers every address with the same redirect, so the notice
// never says whether an account exists.
type ForgotPasswordScreen struct{ component.ContextOnly }

func (s *ForgotPasswordScreen) ScreenTitle(ctx context.Context) string { return forgotTitle.Text(ctx) }
func (s *ForgotPasswordScreen) ScreenDescription(context.Context) string {
	return ""
}
func (s *ForgotPasswordScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ForgotPasswordScreen) RenderCtx(ctx context.Context) render.HTML {
	var notice render.HTML
	if app.QueryFromContext(ctx).Get("sent") == "1" {
		notice = render.Text(forgotSent.Text(ctx))
	}
	return ui.AuthCard(ui.AuthCardConfig{
		Title:  forgotTitle.Text(ctx),
		Alert:  authError(ctx),
		Notice: notice,
		Body: ui.Form(ui.FormConfig{Ctx: ctx, Action: "/auth/forgot-password", Method: "POST", SubmitLabel: forgotSubmit.Text(ctx), SubmitFullWidth: true},
			html.Input(html.InputConfig{Type: "hidden", Name: "next", Value: "/forgot-password?sent=1"}),
			ui.FormField(ui.FormFieldConfig{Label: i18nui.AuthEmail.Text(ctx), For: "auth-email", Required: true, Input: func(c headless.FieldControl) render.HTML {
				return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "email", AutoComplete: "username", EnterKeyHint: "send", NoSpellCheck: true})
			}}),
		),
		Footer: ui.Link(ui.LinkConfig{Href: "/login", Text: forgotBack.Text(ctx)}),
	})
}

// ResetPasswordScreen is the page the emailed link opens: it carries the
// link's token into a form that sets the new password.
type ResetPasswordScreen struct{ component.ContextOnly }

func (s *ResetPasswordScreen) ScreenTitle(ctx context.Context) string { return resetTitle.Text(ctx) }
func (s *ResetPasswordScreen) ScreenDescription(context.Context) string {
	return ""
}
func (s *ResetPasswordScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ResetPasswordScreen) RenderCtx(ctx context.Context) render.HTML {
	token := app.QueryFromContext(ctx).Get("token")
	if token == "" {
		return ui.AuthCard(ui.AuthCardConfig{
			Title:  resetTitle.Text(ctx),
			Alert:  render.Text(resetNoToken.Text(ctx)),
			Footer: ui.Link(ui.LinkConfig{Href: "/forgot-password", Text: forgotTitle.Text(ctx)}),
		})
	}
	return ui.AuthCard(ui.AuthCardConfig{
		Title: resetTitle.Text(ctx),
		Alert: authError(ctx),
		Body: ui.Form(ui.FormConfig{Ctx: ctx, Action: "/auth/reset-password", Method: "POST", SubmitLabel: resetSubmit.Text(ctx), SubmitFullWidth: true},
			html.Input(html.InputConfig{Type: "hidden", Name: "token", Value: token}),
			html.Input(html.InputConfig{Type: "hidden", Name: "next", Value: "/login?reset=1"}),
			ui.FormField(ui.FormFieldConfig{Label: i18nui.AdminNewPassword.Text(ctx), For: "auth-password", Required: true,
				Help: i18nui.AdminNewPasswordHelp.Text(ctx, map[string]any{"min": 8}),
				Input: func(c headless.FieldControl) render.HTML {
					return ui.PasswordInput(ui.PasswordInputConfig{Ctx: ctx, Field: c, Name: "password", Autocomplete: "new-password", MinLength: 8, EnterKeyHint: "done"})
				}}),
		),
	})
}

// TwoFactorScreen finishes a sign-in whose account has a second factor:
// battery/auth sends a form login there with a session that only
// /auth/2fa/challenge accepts.
type TwoFactorScreen struct{ component.ContextOnly }

func (s *TwoFactorScreen) ScreenTitle(ctx context.Context) string { return twoFactorTitle.Text(ctx) }
func (s *TwoFactorScreen) ScreenDescription(context.Context) string {
	return ""
}
func (s *TwoFactorScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TwoFactorScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.AuthCard(ui.AuthCardConfig{
		Title: twoFactorTitle.Text(ctx),
		Alert: authError(ctx),
		Body: ui.Form(ui.FormConfig{Ctx: ctx, Action: "/auth/2fa/challenge", Method: "POST", SubmitLabel: twoFactorSubmit.Text(ctx), SubmitFullWidth: true},
			html.Input(html.InputConfig{Type: "hidden", Name: "next", Value: "/app"}),
			ui.FormField(ui.FormFieldConfig{Label: i18nui.AuthOneTimeCode.Text(ctx), For: "auth-code", Required: true, Help: twoFactorHelp.Text(ctx),
				Input: func(c headless.FieldControl) render.HTML {
					return ui.OneTimeCode(ui.OneTimeCodeConfig{Field: c})
				}}),
		),
		Footer: ui.Link(ui.LinkConfig{Href: "/login", Text: forgotBack.Text(ctx)}),
	})
}

// resetNotice is the sign-in page's notice after a finished reset.
func resetNotice(ctx context.Context) render.HTML {
	if app.QueryFromContext(ctx).Get("reset") != "1" {
		return ""
	}
	return render.Text(resetDone.Text(ctx))
}

// mountAuthRecoveryScreens mounts the three screens. The second-factor
// page takes no guest policy: the session it serves is pending, and
// the battery refuses that session everywhere else.
func mountAuthRecoveryScreens(site *app.App) {
	site.RegisterScreen(app.NewScreen("/forgot-password", &ForgotPasswordScreen{}).WithTitle("Reset your password").WithPolicy(guestPolicy("/app")), marketingLayout)
	site.RegisterScreen(app.NewScreen("/reset-password", &ResetPasswordScreen{}).WithTitle("Choose a new password"), marketingLayout)
	site.RegisterScreen(app.NewScreen("/login/2fa", &TwoFactorScreen{}).WithTitle("Enter your authentication code"), marketingLayout)
}
