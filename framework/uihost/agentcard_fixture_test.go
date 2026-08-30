package uihost

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/jcs"
)

// The committed fixture pair is the only artefact pinning the exact bytes a
// conformant verifier sees. testdata/signing/verify-card.mjs checks it under
// Node WebCrypto, but nothing runs that file, and the fixture cannot be
// drift-checked by regenerating it: ES256 signatures are randomised, so a
// fresh run always differs. This verifies the committed card against the
// committed JWKS instead, pinning the canonicalization, the detached-JWS
// construction and the r||s encoding together. Regenerate with:
//
//	go run ./framework/uihost/testdata/signing/make-fixture.go
//	node framework/uihost/testdata/signing/verify-card.mjs
func TestSignedCardFixtureVerifies(t *testing.T) {
	dir := filepath.Join("testdata", "signing")

	var card map[string]any
	readFixtureJSON(t, filepath.Join(dir, "card.json"), &card)
	var jwks struct {
		Keys []map[string]any `json:"keys"`
	}
	readFixtureJSON(t, filepath.Join(dir, "jwks.json"), &jwks)

	sigsRaw, ok := card["signatures"].([]any)
	if !ok || len(sigsRaw) == 0 {
		t.Fatalf("fixture card carries no signatures")
	}
	// The payload is the card WITHOUT its signatures, canonicalized.
	delete(card, "signatures")
	payload, err := jcs.Canonicalize(card)
	if err != nil {
		t.Fatalf("canonicalize fixture card: %v", err)
	}

	byKID := map[string]map[string]any{}
	for _, k := range jwks.Keys {
		kid, _ := k["kid"].(string)
		byKID[kid] = k
	}

	verified := 0
	for i, raw := range sigsRaw {
		sig, _ := raw.(map[string]any)
		protectedB64, _ := sig["protected"].(string)
		signatureB64, _ := sig["signature"].(string)
		if protectedB64 == "" || signatureB64 == "" {
			t.Fatalf("signature %d is missing protected or signature", i)
		}
		var hdr struct {
			Alg string `json:"alg"`
			Kid string `json:"kid"`
		}
		if err := json.Unmarshal(decodeFixtureB64(t, protectedB64), &hdr); err != nil {
			t.Fatalf("signature %d protected header: %v", i, err)
		}
		key, ok := byKID[hdr.Kid]
		if !ok {
			t.Fatalf("signature %d names kid %q, absent from the committed JWKS", i, hdr.Kid)
		}
		signingInput := []byte(protectedB64 + "." + base64.RawURLEncoding.EncodeToString(payload))
		sigBytes := decodeFixtureB64(t, signatureB64)

		switch hdr.Alg {
		case "EdDSA":
			pub := ed25519.PublicKey(decodeFixtureB64(t, key["x"].(string)))
			if !ed25519.Verify(pub, signingInput, sigBytes) {
				t.Errorf("signature %d (kid %q, EdDSA) does not verify against the committed JWKS", i, hdr.Kid)
				continue
			}
		case "ES256":
			if len(sigBytes) != 64 {
				t.Fatalf("signature %d: ES256 signature is %d bytes, want 64 (r||s)", i, len(sigBytes))
			}
			pub := &ecdsa.PublicKey{
				Curve: elliptic.P256(),
				X:     new(big.Int).SetBytes(decodeFixtureB64(t, key["x"].(string))),
				Y:     new(big.Int).SetBytes(decodeFixtureB64(t, key["y"].(string))),
			}
			sum := sha256.Sum256(signingInput)
			r := new(big.Int).SetBytes(sigBytes[:32])
			s := new(big.Int).SetBytes(sigBytes[32:])
			if !ecdsa.Verify(pub, sum[:], r, s) {
				t.Errorf("signature %d (kid %q, ES256) does not verify against the committed JWKS", i, hdr.Kid)
				continue
			}
		default:
			t.Fatalf("signature %d uses unexpected alg %q", i, hdr.Alg)
		}
		verified++
	}
	if verified != len(sigsRaw) {
		t.Fatalf("verified %d of %d committed signatures", verified, len(sigsRaw))
	}
}

// The published key set must never carry a private member. The JWKS handler
// has its own test; this pins the committed fixture, the file a reader is
// most likely to copy.
func TestSigningFixtureJWKSHasNoPrivateMembers(t *testing.T) {
	var jwks struct {
		Keys []map[string]any `json:"keys"`
	}
	readFixtureJSON(t, filepath.Join("testdata", "signing", "jwks.json"), &jwks)
	for _, k := range jwks.Keys {
		for _, private := range []string{"d", "p", "q", "dp", "dq", "qi", "k"} {
			if _, found := k[private]; found {
				t.Errorf("committed JWKS key %v carries private member %q", k["kid"], private)
			}
		}
	}
}

func readFixtureJSON(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func decodeFixtureB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64url %q: %v", s, err)
	}
	return b
}
