package embed

import (
	"testing"
	"time"
)

// A grant minted under the previous app secret keeps verifying during the
// rotation drain window. Without this, rotating GOFASTR_SECRET breaks every
// live embedded frame immediately — the mass-invalidation that rotation
// support exists to prevent — and refresh cannot migrate the grant to the
// new key because the first verification already rejected it.
func TestHost_VerifiesGrantFromPreviousKey(t *testing.T) {
	oldKey := []byte("old-grant-key-aaaaaaaaaaaaaaaaaaaaaaaa")
	newKey := []byte("new-grant-key-bbbbbbbbbbbbbbbbbbbbbbbb")
	now := time.Now()
	n := Nonce{Surface: "s", Subject: "u", Origin: "https://example.test", ID: "n1", Expires: now.Add(time.Minute)}

	token, err := MintGrant(oldKey, n, 15*time.Minute, now.Add(12*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}

	h := &Host{}
	h.SetKeys([]byte("nonce-key-cccccccccccccccccccccccccc"), newKey)
	if _, err := h.verifyGrant(token, now); err == nil {
		t.Fatal("grant verified before the previous key was installed")
	}

	h.SetPreviousKeys(nil, [][]byte{oldKey})
	if _, err := h.verifyGrant(token, now); err != nil {
		t.Fatalf("grant from the previous key failed during rotation: %v", err)
	}

	// A grant signed by neither key stays rejected.
	bogus, err := MintGrant([]byte("unrelated-key-dddddddddddddddddddddddd"), n, time.Minute, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.verifyGrant(bogus, now); err == nil {
		t.Fatal("a grant signed by an unknown key verified")
	}
}

// The same contract for handshake nonces.
func TestHost_VerifiesNonceFromPreviousKey(t *testing.T) {
	oldKey := []byte("old-nonce-key-aaaaaaaaaaaaaaaaaaaaaaaa")
	newKey := []byte("new-nonce-key-bbbbbbbbbbbbbbbbbbbbbbbb")
	now := time.Now()

	token, err := MintNonce(oldKey, "s", "u", "https://example.test", nil, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}

	h := &Host{}
	h.SetKeys(newKey, []byte("grant-key-cccccccccccccccccccccccccc"))
	if _, err := h.verifyNonce(token, now); err == nil {
		t.Fatal("nonce verified before the previous key was installed")
	}

	h.SetPreviousKeys([][]byte{oldKey}, nil)
	if _, err := h.verifyNonce(token, now); err != nil {
		t.Fatalf("nonce from the previous key failed during rotation: %v", err)
	}
}
