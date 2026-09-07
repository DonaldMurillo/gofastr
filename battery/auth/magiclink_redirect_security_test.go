package auth

import (
	"testing"
)

// Pins: a redirect validator that documents "same-origin path" enforces
// the battery's one safe-relative grammar (isSafeRelativePath): no
// backslash, no percent-encoded smuggling, no C0 bytes — safeRedirectURL
// is implemented on it. Found by the 2026-09-06/07 adversarial round 5,
// phase 2 (family enumeration, tier T3).
// CONTRACT-QUESTION: the input is host-app config (OnSuccessURL, set by
// the developer — not request data), so is developer-input leniency the
// contract here? The function's own doc answers no: safeRedirectURL
// claims it "prevents open-redirect attacks by ensuring the URL is a
// same-origin path", and the battery's own twin isSafeRelativePath
// (form_decode.go:268-304, ~40 lines away in the same package family)
// defines the full grammar the claim implies — raw backslash, percent-
// decoded re-check, C0 refusal. Probe-verified divergence: the weak
// validator passes shapes the twin rejects. This test pins the claim the
// doc makes, not a hypothetical request-facing sink.
// Property: a redirect validator that documents "same-origin path"
// enforces the battery's own safe-relative grammar: no backslash (browsers
// normalize it to slash → //evil.com goes cross-origin), no percent-
// encoded smuggling (browser decodes %5C then normalizes), no C0 bytes
// (CRLF lands in the Location header).
// Surfaces: magiclink.go:702-710 — safeRedirectURL checks only
// u[0]=='/' && u[1]!='/' (no backslash, no C0, no decoded re-check) →
// verifyHandler :687 emits http.Redirect(w, r, safeRedirectURL(...), 302).
// Finding: OnSuccessURL = "/\evil.com/x", "/%5Cevil.com", or
// "/x\r\nSet-Cookie: pwn=1" is returned verbatim and lands in Location:
// arms 1-2 send the just-authenticated session cross-origin after a
// magic-link click, arm 3 forges a header line. isSafeRelativePath
// rejects all three shapes today.
// Fix direction: implement safeRedirectURL on isSafeRelativePath (both
// live in battery/auth), keeping the "/" fallback for failures and the
// "/dashboard"-style positive round-trip.

// TestSafeRedirectRedFullGrammar: safeRedirectURL enforces the battery's
// full safe-relative grammar, not just the first two bytes.
func TestSafeRedirectRedFullGrammar(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		attack string // "" = control leg
	}{
		{"backslash_bypass", "/\\evil.com/x", "/", "browsers normalize backslash to slash, so /\\evil.com/x goes cross-origin"},
		{"encoded_backslash", "/%5Cevil.com", "/", "browser decodes %5C to backslash, then normalizes cross-origin"},
		{"crlf_injection", "/x\r\nSet-Cookie: pwn=1", "/", "raw CRLF lands in the Location header and forges a line"},
		{"protocol_relative", "//evil.com", "/", ""},       // control: refused today
		{"valid_relative", "/dashboard", "/dashboard", ""}, // control: positive round-trip
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := safeRedirectURL(tc.in)
			if got != tc.want {
				if tc.attack != "" {
					t.Errorf("SECURITY: [magiclink-saferedirect-weak] safeRedirectURL(%q) = %q (want %q). Attack: %s. "+
						"isSafeRelativePath (form_decode.go:268) enforces the full grammar — backslash, percent-decoded "+
						"re-check, C0 refusal — for the same package's form redirects; safeRedirectURL's own doc claims it "+
						"'prevents open-redirect attacks by ensuring the URL is a same-origin path'",
						tc.in, got, tc.want, tc.attack)
				} else {
					t.Errorf("safeRedirectURL(%q) = %q (want %q) — control leg regressed", tc.in, got, tc.want)
				}
			}
		})
	}
}
