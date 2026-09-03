//go:build red

package auth

// RED TEST — open finding, 2026-09-03 adversarial pass round 5 (tests-only;
// no fix applied).
//
// Property (source contract, readSrc style — the runtime_security_test.go
// harness adapted to Go): a client-supplied credential is compared with a
// constant-time primitive, never a raw ==/!=. The repo holds this
// everywhere else in the auth family: battery/setup tokenEqual and
// battery/auth TOTP use subtle.ConstantTimeCompare over fixed-width
// codes; oauth state, webhook signatures, uihost session tokens, and this
// package's own bearer Decoder use hmac.Equal.
//
// Surfaces: control/auth/issuance.go Issuer.Confirm — the user-supplied
// 6-digit out-of-band mint code is compared with `code != p.code`, a
// short-circuit byte-at-a-time compare on an auth path.
//
// Finding severity: negligible, labeled honestly. Verified behaviorally
// this round (scratch probe, not pinned as green here): Confirm deletes
// the pending mint BEFORE comparing, so a wrong code burns the attempt
// and a second Confirm(correct) fails with ErrUnknownMint — exactly one
// guess per mint, 60s TTL, ~20-bit code space, TTY-only display channel.
// The timing oracle is therefore worthless as shipped; this pin is the
// parity gap, not an exploit. The burn IS the defense — do NOT add an
// attempt counter or rate limit to fix this.
//
// Fix direction: compare with subtle.ConstantTimeCompare over the two
// fixed-width %06d strings (both sides are exactly 6 chars), matching
// battery/setup/token.go and battery/auth/twofa.go.

import (
	"os"
	"strings"
	"testing"
)

func TestMintCodeRedConstantTimeCompare(t *testing.T) {
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
		t.Errorf("SECURITY: [mint-code-raw-compare] Issuer.Confirm compares the user-supplied 6-digit confirmation code with a raw `code != p.code` (short-circuit string !=) instead of a constant-time primitive — every other client-supplied credential compare in the auth family is constant-time (setup token + TOTP: subtle.ConstantTimeCompare; oauth state, webhook, this package's bearer Decoder: hmac.Equal). Both sides are fixed-width %%06d strings, so subtle.ConstantTimeCompare applies directly. Negligible severity as shipped (delete-before-compare burns the mint on a wrong attempt — verified; that burn is the defense, keep it); pinned for family parity")
	}
}
