//gofastr:generated generator=app kind=scaffold version=dev sum=550140e39a74351c

package main

import (
	"context"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The auth screens' error copy, in English. The app's catalog
// (locales/*.json) translates it by key.
var (
	authErrInvalidCredentials = i18n.Define("shopfront.auth.invalid_credentials", "Invalid email or password.")
	authErrCredentialsMissing = i18n.Define("shopfront.auth.credentials_required", "Enter your email and password.")
	authErrRateLimit          = i18n.Define("shopfront.auth.rate_limit", "Too many attempts. Please wait a moment and try again.")
	authErrEmailTaken         = i18n.Define("shopfront.auth.email_taken", "That email is already registered.")
	authErrUnknown            = i18n.Define("shopfront.auth.unknown", "Sorry, something went wrong. Please try again.")
)

// authError maps auth redirect codes to the alert rendered by generated auth
// screens. Edit this hook when the app needs different copy or extra codes.
func authError(ctx context.Context) render.HTML {
	var p i18n.Phrase
	switch appui.QueryFromContext(ctx).Get("error") {
	case "":
		return ""
	case "invalid_credentials":
		p = authErrInvalidCredentials
	case "credentials_required":
		p = authErrCredentialsMissing
	case "rate_limit":
		p = authErrRateLimit
	case "email_taken", "user_exists", "duplicate":
		p = authErrEmailTaken
	default:
		p = authErrUnknown
	}
	return render.Text(p.Text(ctx))
}
