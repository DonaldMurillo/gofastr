package main

import (
	"context"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The words this file says, in English. The app's catalog
// (locales/*.json) translates them by key.
var (
	authErrInvalidCredentials = i18n.Define("meridian.auth.invalid_credentials", "Invalid email or password.")
	authErrCredentialsMissing = i18n.Define("meridian.auth.credentials_required", "Enter your email and password.")
	authErrRateLimit          = i18n.Define("meridian.auth.rate_limit", "Too many attempts. Please wait a moment and try again.")
	authErrEmailTaken         = i18n.Define("meridian.auth.email_taken", "That email is already registered.")
	authErrUnknown            = i18n.Define("meridian.auth.unknown", "Sorry, something went wrong. Please try again.")
)

// authError maps auth redirect codes to the alert rendered by Meridian's auth
// screens. It stays app-owned because the copy belongs to the product.
func authError(ctx context.Context) render.HTML {
	switch appui.QueryFromContext(ctx).Get("error") {
	case "":
		return ""
	case "invalid_credentials":
		return render.Text(authErrInvalidCredentials.Text(ctx))
	case "credentials_required":
		return render.Text(authErrCredentialsMissing.Text(ctx))
	case "rate_limit":
		return render.Text(authErrRateLimit.Text(ctx))
	case "email_taken", "user_exists", "duplicate":
		return render.Text(authErrEmailTaken.Text(ctx))
	default:
		return render.Text(authErrUnknown.Text(ctx))
	}
}
