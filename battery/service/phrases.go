package service

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonNotOwnLoopback = i18n.Define("service.not_own_loopback", "forbidden: this address is not the app's own loopback address")
	reasonNotSignedIn    = i18n.Define("service.not_signed_in", "Not signed in. Run `{{command}}` to sign in.")
	reasonSignInLinkUsed = i18n.Define("service.sign_in_link_used", "This sign-in link has expired or was already used. Run `{{command}}` again.")
)
