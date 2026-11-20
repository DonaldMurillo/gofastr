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

var _ = registry.RegisterBehavior("passkey", passkeyJS,
	registry.Markers("[data-passkey-login]", "[data-passkey-register]", "[data-passkey-delete]"))

var (
	sayPasskeyAdded   = i18n.Define("auth.passkey_added", "Passkey added")
	sayPasskeyRemoved = i18n.Define("auth.passkey_removed", "Passkey removed")
	sayPasskeyFailed  = i18n.Define("auth.passkey_failed", "Passkey request failed")
)

func init() { runtime.RegisterStrings(sayPasskeyAdded, sayPasskeyRemoved, sayPasskeyFailed) }

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
