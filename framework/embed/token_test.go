package embed

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	testNonceKey = []byte("nonce-key-nonce-key-nonce-key-32")
	testGrantKey = []byte("grant-key-grant-key-grant-key-32")
)

func TestNonceRoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tok, err := MintNonce(testNonceKey, "dash", "user-7", "https://ACME.com/", []string{"read"}, time.Minute, now)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	if !strings.HasPrefix(tok, NoncePrefix) {
		t.Fatalf("nonce %q missing %q prefix", tok, NoncePrefix)
	}
	n, err := VerifyNonce(testNonceKey, tok, now)
	if err != nil {
		t.Fatalf("VerifyNonce: %v", err)
	}
	if n.Surface != "dash" || n.Subject != "user-7" {
		t.Fatalf("claims round-tripped wrong: %+v", n)
	}
	// The origin is stored NORMALIZED, so the burn-time comparison never has
	// to re-normalize a value an attacker could shape.
	if n.Origin != "https://acme.com" {
		t.Fatalf("origin = %q, want the normalized form", n.Origin)
	}
	if n.ID == "" {
		t.Fatal("nonce carries no id — nothing for the burn store to key on")
	}
}

func TestNonceIDsAreUnique(t *testing.T) {
	now := time.Now()
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		tok, err := MintNonce(testNonceKey, "dash", "u", "https://acme.com", nil, time.Minute, now)
		if err != nil {
			t.Fatalf("MintNonce: %v", err)
		}
		n, err := VerifyNonce(testNonceKey, tok, now)
		if err != nil {
			t.Fatalf("VerifyNonce: %v", err)
		}
		if seen[n.ID] {
			t.Fatalf("nonce id %q repeated — the burn store's uniqueness would reject a legitimate exchange", n.ID)
		}
		seen[n.ID] = true
	}
}

func TestNonceRejectsTamperingAndWrongKey(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tok, err := MintNonce(testNonceKey, "dash", "user-7", "https://acme.com", []string{"read"}, time.Minute, now)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}

	if _, err := VerifyNonce([]byte("some-other-key-some-other-key-32"), tok, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong key: err = %v, want ErrBadSignature", err)
	}

	// Flip one payload byte. Base64 keeps it decodable, so this exercises the
	// MAC rather than the decoder.
	body := strings.TrimPrefix(tok, NoncePrefix)
	dot := strings.LastIndexByte(body, '.')
	payload, sig := body[:dot], body[dot+1:]
	flipped := "A" + payload[1:]
	if flipped == payload {
		flipped = "B" + payload[1:]
	}
	if _, err := VerifyNonce(testNonceKey, NoncePrefix+flipped+"."+sig, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered payload: err = %v, want ErrBadSignature", err)
	}

	for _, bad := range []string{"", "emb_", "emb_nodot", "emb_a.", "emb_.b", tok[4:], "emg_" + body} {
		if _, err := VerifyNonce(testNonceKey, bad, now); err == nil {
			t.Errorf("VerifyNonce(%q) succeeded, want an error", bad)
		}
	}
}

func TestNonceExpires(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tok, err := MintNonce(testNonceKey, "dash", "u", "https://acme.com", nil, time.Minute, now)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	if _, err := VerifyNonce(testNonceKey, tok, now.Add(59*time.Second)); err != nil {
		t.Fatalf("inside the window: %v", err)
	}
	if _, err := VerifyNonce(testNonceKey, tok, now.Add(time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("at expiry: err = %v, want ErrExpired", err)
	}
	if _, err := VerifyNonce(testNonceKey, tok, now.Add(2*time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("past expiry: err = %v, want ErrExpired", err)
	}
}

// A nonce and a grant carry near-identical claims. Two independent barriers
// stop one being presented as the other: separate HKDF-derived keys, and the
// token prefix being fed into the MAC. This asserts BOTH, by trying the swap
// under the correct key for the other type as well as under a shared key.
func TestNonceAndGrantAreNotInterchangeable(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	nonceTok, err := MintNonce(testNonceKey, "dash", "u", "https://acme.com", nil, time.Minute, now)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	n, err := VerifyNonce(testNonceKey, nonceTok, now)
	if err != nil {
		t.Fatalf("VerifyNonce: %v", err)
	}
	grantTok, err := MintGrant(testGrantKey, n, time.Hour, now.Add(24*time.Hour), now)
	if err != nil {
		t.Fatalf("MintGrant: %v", err)
	}

	if _, err := VerifyGrant(testGrantKey, nonceTok, now); err == nil {
		t.Error("a nonce verified as a grant")
	}
	if _, err := VerifyNonce(testNonceKey, grantTok, now); err == nil {
		t.Error("a grant verified as a nonce")
	}

	// Same key for both purposes: only the in-MAC prefix separates them now.
	shared := []byte("shared-key-shared-key-shared-k32")
	sharedNonce, err := MintNonce(shared, "dash", "u", "https://acme.com", nil, time.Minute, now)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	swapped := GrantPrefix + strings.TrimPrefix(sharedNonce, NoncePrefix)
	if _, err := VerifyGrant(shared, swapped, now); err == nil {
		t.Error("re-prefixing a nonce produced a valid grant under a shared key — the prefix is not in the MAC")
	}
}

func TestGrantRoundTripAndScopes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	src := Nonce{Surface: "dash", Subject: "u-1", Scopes: []string{"read", "comment"}, Origin: "https://acme.com", ID: "abc"}
	tok, err := MintGrant(testGrantKey, src, time.Hour, now.Add(24*time.Hour), now)
	if err != nil {
		t.Fatalf("MintGrant: %v", err)
	}
	if !strings.HasPrefix(tok, GrantPrefix) {
		t.Fatalf("grant %q missing %q prefix", tok, GrantPrefix)
	}
	g, err := VerifyGrant(testGrantKey, tok, now)
	if err != nil {
		t.Fatalf("VerifyGrant: %v", err)
	}
	if g.Surface != "dash" || g.Subject != "u-1" || g.Origin != "https://acme.com" {
		t.Fatalf("claims round-tripped wrong: %+v", g)
	}
	if !g.HasScope("read") || !g.HasScope("comment") || g.HasScope("admin") {
		t.Fatalf("scopes wrong: %v", g.Scopes)
	}
	if _, err := VerifyGrant(testGrantKey, tok, now.Add(time.Hour)); !errors.Is(err, ErrExpired) {
		t.Fatalf("at expiry: err = %v, want ErrExpired", err)
	}
}

func TestMintRejectsBadOriginAndMissingKey(t *testing.T) {
	now := time.Now()
	if _, err := MintNonce(testNonceKey, "dash", "u", "https://acme.com/dash", nil, time.Minute, now); err == nil {
		t.Error("minting for an origin with a path succeeded")
	}
	if _, err := MintNonce(nil, "dash", "u", "https://acme.com", nil, time.Minute, now); err == nil {
		t.Error("minting with no key succeeded")
	}
	if _, err := MintGrant(nil, Nonce{Surface: "dash", Origin: "https://acme.com"}, time.Minute, now.Add(time.Hour), now); err == nil {
		t.Error("minting a grant with no key succeeded")
	}
}
