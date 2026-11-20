package auth

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

//go:embed passkey.js
var passkeyJS string

//go:embed passkey_manage.js
var passkeyManageJS string

var _ = registry.RegisterBehavior("passkey", passkeyJS,
	registry.Markers("[data-passkey-login]", "[data-passkey-autofill]"))

var _ = registry.RegisterBehavior("passkey-manage", passkeyManageJS,
	registry.Requires("passkey"),
	registry.Markers("[data-passkey-register]", "[data-passkey-upgrade]", "[data-passkey-rename]",
		"[data-passkey-delete]", "[data-passkey-reauth]", "[data-passkey-list]"))

var (
	sayPasskeyAdded      = i18n.Define("auth.passkey_added", "Passkey added")
	sayPasskeyRemoved    = i18n.Define("auth.passkey_removed", "Passkey removed")
	sayPasskeyFailed     = i18n.Define("auth.passkey_failed", "Passkey request failed")
	sayPasskeyRenamed    = i18n.Define("auth.passkey_renamed", "Passkey renamed")
	sayPasskeyExistsHere = i18n.Define("auth.passkey_exists_here", "This device already holds a passkey for this account")
	sayPasskeyWrongSite  = i18n.Define("auth.passkey_wrong_site", "Passkeys do not work on this site address")
)

func init() {
	runtime.RegisterStrings(sayPasskeyAdded, sayPasskeyRemoved, sayPasskeyFailed,
		sayPasskeyRenamed, sayPasskeyExistsHere, sayPasskeyWrongSite)
}

// PasskeyAutocomplete is the autocomplete value for a username field PasskeyAutofillAttrs marks.
const PasskeyAutocomplete = "username webauthn"

// PasskeyReauthField is the form field a PasskeyReauthAttrs button fills with the step-up proof.
const PasskeyReauthField = "passkey_reauth"

// PasskeyAutofillAttrs marks the sign-in username field for passkey autofill against basePath.
func PasskeyAutofillAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-autofill": basePath}
}

// PasskeyUpgradeAttrs marks a post-password-sign-in page that asks for a passkey without a prompt.
func PasskeyUpgradeAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-upgrade": basePath}
}

// PasskeyRenameAttrs marks a button that renames passkey id to its form's PasskeyNameAttrs field.
func PasskeyRenameAttrs(basePath string, id []byte) html.Attrs {
	return html.Attrs{"data-passkey-rename": basePath, "data-passkey-id": PasskeyID(id), "hidden": ""}
}

// PasskeyReauthAttrs marks a sensitive action's submit button that steps up with a passkey first.
func PasskeyReauthAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-reauth": basePath, "hidden": ""}
}

// PasskeyListAttrs marks a server-drawn passkey list the module syncs to the password manager.
func PasskeyListAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-list": basePath}
}

// PasskeyLoginAttrs marks a button as passkey sign-in against basePath; it stays hidden where WebAuthn is missing.
func PasskeyLoginAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-login": basePath, "hidden": ""}
}

// PasskeyRegisterAttrs marks a button as "add a passkey"; a PasskeyNameAttrs field in its form names it.
func PasskeyRegisterAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-register": basePath, "hidden": ""}
}

// PasskeyNameAttrs marks the field PasskeyRegisterAttrs reads the name from.
func PasskeyNameAttrs() html.Attrs {
	return html.Attrs{"data-passkey-name": ""}
}

// PasskeyDeleteAttrs marks a button that removes the signed-in account's passkey id.
func PasskeyDeleteAttrs(basePath string, id []byte) html.Attrs {
	return html.Attrs{"data-passkey-delete": basePath, "data-passkey-id": PasskeyID(id), "hidden": ""}
}

// PasskeyID is a credential id as unpadded base64url.
func PasskeyID(id []byte) string { return b64url.EncodeToString(id) }
