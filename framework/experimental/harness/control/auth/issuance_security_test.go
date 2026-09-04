package auth

import (
	"os"
	"strings"
	"testing"
)

// Issuer.Confirm compares the user-supplied 6-digit confirmation code
// with a constant-time primitive, never a raw ==/!=: every other
// client-supplied credential compare in the auth family is
// constant-time (setup token + TOTP: subtle.ConstantTimeCompare; oauth
// state, webhook signatures, the bearer Decoder: hmac.Equal), and both
// sides here are fixed-width %06d strings. The delete-before-compare
// burn of a wrong attempt is the rate limit and stays.
func TestConfirmComparesCodeConstantTime(t *testing.T) {
	raw, err := os.ReadFile("issuance.go")
	if err != nil {
		t.Fatalf("read issuance.go (tests run from the package dir): %v", err)
	}
	src := string(raw)

	idx := strings.Index(src, "func (i *Issuer) Confirm(mintID, code string)")
	if idx < 0 {
		t.Fatalf("Issuer.Confirm anchor not found in issuance.go — the mint confirmation flow moved; update this pin")
	}
	end := strings.Index(src[idx:], "\nfunc ")
	if end < 0 {
		end = len(src) - idx
	}
	body := src[idx : idx+end]

	hasRaw := strings.Contains(body, "code != p.code")
	hasCT := strings.Contains(body, "subtle.ConstantTimeCompare") || strings.Contains(body, "hmac.Equal")
	if hasRaw || !hasCT {
		t.Errorf("SECURITY: [mint-code-raw-compare] Issuer.Confirm compares the user-supplied 6-digit " +
			"confirmation code with a raw `code != p.code` (short-circuit string !=) instead of a " +
			"constant-time primitive — family parity with battery/setup, battery/auth, oauth state, " +
			"webhook signatures, and this package's own bearer Decoder; both sides are fixed-width " +
			"%%06d strings, so subtle.ConstantTimeCompare applies directly")
	}
}
