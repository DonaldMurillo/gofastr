package ui

import (
	"strings"
	"testing"
)

// Property: values seeded from the URL (the modal deeplink params
// src/alt/caption/group are attacker-supplied by construction — anyone
// can hand-craft ?…caption=<img onerror>… at a Lightbox) must only ever
// land in runtime bindings that neutralise them: text-mode regions or
// the URL-guarded attribute set. The runtime's attr-mode writer guards
// href/src/action/xlink:href/formaction against dangerous schemes
// (runtime.js _isUnsafeSignalUrl, added for exactly the Lightbox
// AllowDownload anchor), and its html mode refuses untrusted seeds.
// The widget side of the contract: Lightbox must not bind any deeplink
// signal to an html-mode region, and its URL bindings must stay on the
// guarded attrs (src for the image, href for the download anchor).
//
// This test renders the slot directly (same harness as the Modal
// security tests via confirmDialogSlot).
func TestLightboxCaptionStaysTextMode(t *testing.T) {
	payload := `"><img src=x onerror=alert(1)>`
	s := &lightboxSlot{
		name:          "lb",
		label:         payload,
		navArrows:     true,
		showCaption:   true,
		allowDownload: true,
	}
	h := string(s.Render())

	if strings.Contains(h, `data-fui-signal-mode="html"`) {
		t.Errorf("SECURITY: a Lightbox deeplink-bound region uses html mode; a URL-seeded caption value would render as markup:\n%s", h)
	}
	// URL-bearing bindings must be exactly the guarded attrs.
	if !strings.Contains(h, `data-fui-signal-attr="src"`) {
		t.Errorf("Lightbox image must bind its src through the guarded attr mode:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-signal-attr="href"`) {
		t.Errorf("Lightbox download anchor must bind its href through the guarded attr mode:\n%s", h)
	}
	// The accessible label is config-supplied but must still be escaped.
	if strings.Contains(h, payload) || strings.Contains(h, "<img src=x") {
		t.Errorf("SECURITY: Lightbox label rendered unescaped:\n%s", h)
	}
	if !strings.Contains(h, "&lt;img") {
		t.Errorf("Lightbox label should appear escaped:\n%s", h)
	}
}
