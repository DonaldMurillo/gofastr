package main

import (
	"context"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// authError maps auth redirect codes to the alert rendered by Meridian's auth
// screens. It stays app-owned because the copy belongs to the product.
func authError(ctx context.Context) render.HTML {
	switch appui.QueryFromContext(ctx).Get("error") {
	case "":
		return ""
	case "invalid_credentials":
		return render.Text("Invalid email or password.")
	case "credentials_required":
		return render.Text("Enter your email and password.")
	case "rate_limit":
		return render.Text("Too many attempts. Please wait a moment and try again.")
	case "email_taken", "user_exists", "duplicate":
		return render.Text("That email is already registered.")
	default:
		return render.Text("Sorry, something went wrong. Please try again.")
	}
}
