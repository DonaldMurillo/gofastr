package sessiontoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Audit additions (issue #136 slice). The original suite covers
// round-trip, wrong key, tampered id/created, expiry, far-future,
// skew, malformed shapes, key-required, and the VerifyAny rotation
// trio. These pin what it does not:
//
//   - the MAC itself being tamper-checked (every existing tamper test
//     mutates id/created and RELIES on the MAC check; none mutates MAC)
//   - separator canonicality: a dot smuggled into the id or created
//     part is structurally rejected even when the MAC is correctly
//     signed for that (non-canonical) interpretation — i.e. there is no
//     re-spelling of a token body that reproduces another token's
//     signing input
//   - the minKeyLen boundary (15 rejected / 16 accepted) on BOTH Mint
//     and Verify
//   - maxTokenLen rejecting a correctly-signed overlong token (the
//     original malformed test only feeds garbage past the length bound)
//   - the clockSkew boundary (exactly +2m accepted, +2m1s rejected)
//   - the format claim "the MAC covers everything the token carries":
//     a minted token is exactly id.created.mac, the MAC input is
//     exactly context\x00id\x00created, and Verify returns exactly the
//     signed id — nothing outside the signed bytes is read or trusted.
//
// Each guard below was made to fail by mutation before being trusted
// (CLAUDE.md hard rule 11); see the audit report for the mutation log.

// TestRejectsTamperedMAC mutates only the MAC segment. The original
// suite's tamper tests change id/created; this one proves the MAC
// comparison itself is load-bearing.
func TestRejectsTamperedMAC(t *testing.T) {
	tok, _, err := Mint(key, now)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	parts := strings.Split(tok, ".")
	mac := parts[2]
	// Flip the last base64url char to a different valid-alphabet char.
	last := mac[len(mac)-1]
	flipped := byte('A')
	if last == 'A' {
		flipped = 'B'
	}
	tampered := parts[0] + "." + parts[1] + "." + mac[:len(mac)-1] + string(flipped)
	if _, ok := Verify(key, tampered, now, maxAge); ok {
		t.Fatal("token with a flipped MAC byte verified")
	}
}

// TestRejectsDotInsideSignedParts proves the "." separator cannot be
// impersonated to relocate bytes between the id and created fields.
// Each case builds the token a signer-with-key would produce for the
// NON-canonical interpretation (MAC correctly signed for it), so the
// only thing that can reject these is the 3-part structural check —
// exactly the guard that stops separator smuggling.
func TestRejectsDotInsideSignedParts(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		created string
	}{
		{"dot in id", "sess-a.b", "1800000000"},
		{"dot in created", "sess-abcabcabcabcabcabcab", "18.00000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok := tc.id + "." + tc.created + "." + sign(key, tc.id, tc.created)
			if _, ok := Verify(key, tok, now, maxAge); ok {
				t.Fatalf("non-canonical token %q verified: the separator is not structural", tok)
			}
		})
	}
}

// TestNoRespellingSharesASigningInput sweeps every alternative split of
// a minted token's body (id.created re-cut at every dot-insertion
// point) and asserts each distinct (id, created) pair yields a distinct
// MAC. That is the executed half of "two different token bodies cannot
// produce the same signing input": within the finite re-spelling set of
// a real token there is no collision, so an attacker cannot take MAC(X)
// and present it as MAC(Y) for a different field split of the same
// bytes.
func TestNoRespellingSharesASigningInput(t *testing.T) {
	tok, _, err := Mint(key, now)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	parts := strings.Split(tok, ".")
	id, created := parts[0], parts[1]
	body := id + "." + created
	seen := map[string]string{} // mac -> "id|created" that produced it
	for i := range len(body) {
		// A re-spelling consumes the single "." at a different offset:
		// id' = body[:i], created' = body[i+1:].
		altID, altCreated := body[:i], body[i+1:]
		if altID == id && altCreated == created {
			continue // the canonical split
		}
		mac := sign(key, altID, altCreated)
		if owner, dup := seen[mac]; dup {
			t.Fatalf("signing-input collision: (%s) and (%s) share MAC %s", owner, altID+"|"+altCreated, mac)
		}
		seen[mac] = altID + "|" + altCreated
		if mac == parts[2] {
			t.Fatalf("SECURITY: re-spelling (%s|%s) reproduces the canonical MAC — forgery by re-split", altID, altCreated)
		}
	}
}

// TestKeyLenBoundary pins minKeyLen exactly: 15 bytes is a
// misconfiguration on both sides (Mint refuses, Verify refuses), 16
// works. The original tests only use nil and a 5-byte key.
func TestKeyLenBoundary(t *testing.T) {
	short := []byte("123456789012345")  // 15 bytes
	exact := []byte("1234567890123456") // 16 bytes
	if _, _, err := Mint(short, now); err == nil {
		t.Fatal("Mint accepted a 15-byte key")
	}
	tok, id, err := Mint(exact, now)
	if err != nil {
		t.Fatalf("Mint rejected a 16-byte key: %v", err)
	}
	if _, ok := Verify(exact, tok, now, maxAge); !ok {
		t.Fatal("Verify rejected a token signed with a 16-byte key")
	}
	// A short VERIFY key must also refuse, not fall back to accepting.
	if _, ok := Verify(short, tok, now, maxAge); ok {
		t.Fatal("Verify accepted a 15-byte key")
	}
	_ = id
}

// TestMaxTokenLenRejectsSignedOverlongToken feeds a syntactically
// valid, correctly-signed token whose id segment pushes it past
// maxTokenLen. Only the length bound can reject it.
func TestMaxTokenLenRejectsSignedOverlongToken(t *testing.T) {
	longID := "sess-" + strings.Repeat("a", 300)
	created := strconv.FormatInt(now.Unix(), 10)
	tok := longID + "." + created + "." + sign(key, longID, created)
	if len(tok) <= maxTokenLen {
		t.Fatalf("premise broken: test token is only %d bytes", len(tok))
	}
	if _, ok := Verify(key, tok, now, maxAge); ok {
		t.Fatal("overlong but correctly-signed token verified")
	}
}

// TestClockSkewBoundary pins the exact skew edge: a token minted
// exactly clockSkew in the future is accepted (drifting replicas), one
// second further is rejected.
func TestClockSkewBoundary(t *testing.T) {
	edge, _, err := Mint(key, now.Add(clockSkew))
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if _, ok := Verify(key, edge, now, maxAge); !ok {
		t.Fatal("token minted exactly clockSkew in the future was rejected")
	}
	past, _, err := Mint(key, now.Add(clockSkew+time.Second))
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if _, ok := Verify(key, past, now, maxAge); ok {
		t.Fatal("token minted clockSkew+1s in the future was accepted")
	}
}

// TestMintTokenCarriesExactlyWhatMACCovers pins the token format end to
// end: the token is exactly id.created.mac, the MAC is exactly the
// HMAC over macContext\x00id\x00created (recomputed here via the same
// package's sign), the created field is exactly the mint time, and
// Verify returns exactly the signed id. There is no field carried
// outside the signed bytes for an attacker to edit.
func TestMintTokenCarriesExactlyWhatMACCovers(t *testing.T) {
	tok, id, err := Mint(key, now)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want exactly 3: %q", len(parts), tok)
	}
	if parts[0] != id {
		t.Fatalf("token id part %q != returned id %q", parts[0], id)
	}
	if want := strconv.FormatInt(now.Unix(), 10); parts[1] != want {
		t.Fatalf("created part %q != mint time %q", parts[1], want)
	}
	// The expected MAC is recomputed from primitives, not via sign(): if
	// sign changed its context string, delimiter, hash, or encoding, a
	// sign-based expectation would drift with it and this assertion would
	// pass vacuously. This pins the exact bytes on the wire.
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(macContext))
	mac.Write([]byte{0})
	mac.Write([]byte(parts[0]))
	mac.Write([]byte{0})
	mac.Write([]byte(parts[1]))
	if want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)); parts[2] != want {
		t.Fatal("MAC segment is not HMAC-SHA256(macContext\\x00id\\x00created) in raw-URL base64 — the token format drifted")
	}
	got, ok := Verify(key, tok, now, maxAge)
	if !ok || got != parts[0] {
		t.Fatalf("Verify = %q, %v; want exactly the signed id %q", got, ok, parts[0])
	}
}
