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

// The browser half of the passkey plugin is two behaviours, so a page
// loads only the half it marks: "passkey" signs in (a button, the
// autofill field) and holds the shared helpers; "passkey-manage" adds,
// renames and removes passkeys, runs step-ups and syncs the password
// manager, on those helpers. Markers are string literals so the
// runtime's marker gate can read them.
var _ = registry.RegisterBehavior("passkey", passkeyJS,
	registry.Markers("[data-passkey-login]", "[data-passkey-autofill]"))

var _ = registry.RegisterBehavior("passkey-manage", passkeyManageJS,
	registry.Requires("passkey"),
	registry.Markers("[data-passkey-register]", "[data-passkey-upgrade]", "[data-passkey-rename]",
		"[data-passkey-delete]", "[data-passkey-reauth]", "[data-passkey-list]"))

// The toasts passkey.js says, in the reader's language.
var (
	sayPasskeyAdded   = i18n.Define("auth.passkey_added", "Passkey added")
	sayPasskeyRemoved = i18n.Define("auth.passkey_removed", "Passkey removed")
	sayPasskeyFailed  = i18n.Define("auth.passkey_failed", "Passkey request failed")
	sayPasskeyRenamed = i18n.Define("auth.passkey_renamed", "Passkey renamed")
	// InvalidStateError: the authenticator already holds one of the
	// account's passkeys (excludeCredentials matched).
	sayPasskeyExistsHere = i18n.Define("auth.passkey_exists_here", "This device already holds a passkey for this account")
	// SecurityError: the page's origin does not fit the RP ID.
	sayPasskeyWrongSite = i18n.Define("auth.passkey_wrong_site", "Passkeys do not work on this site address")
)

func init() {
	runtime.RegisterStrings(sayPasskeyAdded, sayPasskeyRemoved, sayPasskeyFailed,
		sayPasskeyRenamed, sayPasskeyExistsHere, sayPasskeyWrongSite)
}

// PasskeyAutocomplete is the autocomplete value for the sign-in form's
// username field when PasskeyAutofillAttrs marks it: the browser then
// lists the device's passkeys beside saved usernames.
const PasskeyAutocomplete = "username webauthn"

// PasskeyReauthField is the form field a PasskeyReauthAttrs button
// fills with the step-up proof before it submits the form.
const PasskeyReauthField = "passkey_reauth"

// PasskeyAutofillAttrs marks the sign-in form's username field so the
// browser offers the device's passkeys in its autofill list
// (conditional UI); picking one signs in against basePath. Set the
// field's autocomplete to PasskeyAutocomplete as well (ui.Control owns
// that attribute); the module appends "webauthn" when it is missing.
// Browsers without conditional UI leave the field as it is.
func PasskeyAutofillAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-autofill": basePath}
}

// PasskeyUpgradeAttrs marks an element on a page the user reaches
// right after a password sign-in. Where the browser supports it, the
// module asks the password manager to save a passkey for the account
// without a prompt (conditional create); elsewhere it does nothing.
func PasskeyUpgradeAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-upgrade": basePath}
}

// PasskeyRenameAttrs marks a button that renames the signed-in
// account's passkey id to the value of the PasskeyNameAttrs field in
// the same form. The page re-renders after it is renamed.
func PasskeyRenameAttrs(basePath string, id []byte) html.Attrs {
	return html.Attrs{"data-passkey-rename": basePath, "data-passkey-id": PasskeyID(id), "hidden": ""}
}

// PasskeyReauthAttrs marks a button inside the form of a sensitive
// action: it runs a passkey step-up, puts the proof in the form's
// PasskeyReauthField and submits the form. The form's handler spends
// the proof with PasskeyPlugin.RedeemReauthProof.
func PasskeyReauthAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-reauth": basePath, "hidden": ""}
}

// PasskeyListAttrs marks the element around a server-drawn passkey
// list. When the page loads, the module tells the password manager
// which passkeys the account still has, so one removed elsewhere stops
// being offered, and what the account is called.
func PasskeyListAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-list": basePath}
}

// PasskeyLoginAttrs marks a button as "sign in with a passkey" against
// the auth routes mounted at basePath ("/auth"). Put them on a
// ui.Button's ExtraAttrs. The button renders hidden and the passkey
// module reveals it when the browser supports WebAuthn, so a page
// without script never shows a button that cannot work.
func PasskeyLoginAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-login": basePath, "hidden": ""}
}

// PasskeyRegisterAttrs marks a button as "add a passkey" for the
// signed-in account. When the button sits in a form, a field carrying
// PasskeyNameAttrs supplies the passkey's name; without one the server
// names it "Passkey". The page re-renders after a passkey is added.
func PasskeyRegisterAttrs(basePath string) html.Attrs {
	return html.Attrs{"data-passkey-register": basePath, "hidden": ""}
}

// PasskeyNameAttrs marks the text field PasskeyRegisterAttrs reads the
// new passkey's name from.
func PasskeyNameAttrs() html.Attrs {
	return html.Attrs{"data-passkey-name": ""}
}

// PasskeyDeleteAttrs marks a button that removes the signed-in
// account's passkey id (PasskeyCredential.ID, as PasskeyID encodes it).
// The page re-renders after it is removed.
func PasskeyDeleteAttrs(basePath string, id []byte) html.Attrs {
	return html.Attrs{"data-passkey-delete": basePath, "data-passkey-id": PasskeyID(id), "hidden": ""}
}

// PasskeyID is the text form of a credential id the passkey routes and
// markup use: unpadded base64url.
func PasskeyID(id []byte) string { return b64url.EncodeToString(id) }
