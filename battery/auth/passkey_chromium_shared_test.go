//go:build chromium

package auth

import (
	"context"
	"testing"

	"github.com/chromedp/cdproto/webauthn"
	"github.com/chromedp/chromedp"
)

// enableVirtualAuthenticator turns on Chrome's CTAP2 virtual
// authenticator with resident key and automatic user verification —
// the wire shape the passkey browser tests drive both ceremonies
// through. It is the shared setup of the passkey chromium suites.
func enableVirtualAuthenticator(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx, webauthn.Enable(), chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := webauthn.AddVirtualAuthenticator(&webauthn.VirtualAuthenticatorOptions{
			Protocol: webauthn.AuthenticatorProtocolCtap2, Ctap2version: webauthn.Ctap2versionCtap21,
			Transport: webauthn.AuthenticatorTransportInternal, HasResidentKey: true,
			HasUserVerification: true, IsUserVerified: true, AutomaticPresenceSimulation: true,
		}).Do(ctx)
		return err
	})); err != nil {
		t.Fatalf("virtual authenticator: %v", err)
	}
}
