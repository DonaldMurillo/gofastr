package embed

import (
	"errors"
	"testing"
	"time"
)

// VerifyNonce must reject a signed nonce whose required identifying fields are
// empty, even though its signature and expiry are valid.
//
// A nonce with no Surface, ID or Origin is structurally unusable: the burn
// store keys on ID, the exchange keys the grant on Surface, and the frame
// check keys on Origin. MintNonce always sets all three, so the only way one
// arrives empty is a tampered or hand-crafted payload that nonetheless carries
// a valid MAC (e.g. a key compromise, or a future minter bug). Signature
// verification alone cannot catch it; the explicit malformed check does, and
// deleting it lets the empty fields flow into a verified Nonce.
func TestVerifyNonceRejectsAClaimsWithARequiredFieldMissing(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	base := nonceClaims{
		Surface: "dashboard",
		Subject: "u-1",
		Scopes:  []string{"read"},
		Origin:  "https://acme.com",
		ID:      "nonce-id-abc",
		Expires: now.Add(time.Minute).Unix(),
	}
	for _, tc := range []struct {
		name  string
		blank func(*nonceClaims)
	}{
		{"empty surface", func(c *nonceClaims) { c.Surface = "" }},
		{"empty id", func(c *nonceClaims) { c.ID = "" }},
		{"empty origin", func(c *nonceClaims) { c.Origin = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.blank(&c)
			tok, err := sign(NoncePrefix, testNonceKey, c)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			if _, err := VerifyNonce(testNonceKey, tok, now); !errors.Is(err, ErrMalformed) {
				t.Fatalf("VerifyNonce of a nonce with %s: err = %v, want ErrMalformed", tc.name, err)
			}
		})
	}
}
