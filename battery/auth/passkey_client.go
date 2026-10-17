package auth

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed passkey.js
var passkeyJS string

// The browser half of the passkey plugin loads when a marked button is
// on the page. Markers are string literals so the runtime's marker gate
// can read them.
var _ = registry.RegisterBehavior("passkey", passkeyJS,
	registry.Markers("[data-passkey-login]", "[data-passkey-register]", "[data-passkey-delete]"))

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
