package sessiontoken

import (
	"strings"
	"testing"
	"time"
)

var (
	key      = []byte("0123456789abcdef0123456789abcdef")
	otherKey = []byte("fedcba9876543210fedcba9876543210")
	now      = time.Unix(1_800_000_000, 0)
	maxAge   = 30 * 24 * time.Hour
)

func TestMintVerifyRoundtrip(t *testing.T) {
	tok, id, err := Mint(key, now)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if !strings.HasPrefix(id, "sess-") {
		t.Fatalf("id %q lacks sess- prefix", id)
	}
	got, ok := Verify(key, tok, now.Add(time.Hour), maxAge)
	if !ok || got != id {
		t.Fatalf("Verify = %q, %v; want %q, true", got, ok, id)
	}
}

func TestIDsAreUnique(t *testing.T) {
	_, a, _ := Mint(key, now)
	_, b, _ := Mint(key, now)
	if a == b {
		t.Fatalf("two mints returned the same id %q", a)
	}
}

func TestRejectsWrongKey(t *testing.T) {
	tok, _, _ := Mint(key, now)
	if _, ok := Verify(otherKey, tok, now, maxAge); ok {
		t.Fatal("token verified under a different key")
	}
}

func TestRejectsTamperedID(t *testing.T) {
	tok, _, _ := Mint(key, now)
	parts := strings.SplitN(tok, ".", 3)
	tampered := "sess-AAAAAAAAAAAAAAAAAAAAAA" + "." + parts[1] + "." + parts[2]
	if _, ok := Verify(key, tampered, now, maxAge); ok {
		t.Fatal("tampered id verified")
	}
}

func TestRejectsTamperedCreated(t *testing.T) {
	tok, _, _ := Mint(key, now)
	parts := strings.SplitN(tok, ".", 3)
	tampered := parts[0] + ".9999999999." + parts[2]
	if _, ok := Verify(key, tampered, now, maxAge); ok {
		t.Fatal("tampered created verified")
	}
}

func TestRejectsExpired(t *testing.T) {
	tok, _, _ := Mint(key, now)
	if _, ok := Verify(key, tok, now.Add(maxAge+time.Hour), maxAge); ok {
		t.Fatal("expired token verified")
	}
}

func TestRejectsFarFuture(t *testing.T) {
	tok, _, _ := Mint(key, now.Add(time.Hour))
	if _, ok := Verify(key, tok, now, maxAge); ok {
		t.Fatal("future-dated token verified beyond skew")
	}
}

func TestAllowsSmallSkew(t *testing.T) {
	tok, _, _ := Mint(key, now.Add(30*time.Second))
	if _, ok := Verify(key, tok, now, maxAge); !ok {
		t.Fatal("token within clock skew rejected")
	}
}

func TestRejectsMalformed(t *testing.T) {
	for _, tok := range []string{
		"", "sess-abc", "sess-abc.123", "a.b.c.d", "sess-abc..mac",
		"sess-abc.notanumber.mac", strings.Repeat("x", 4096),
		"sess-abc.123.", ".123.mac",
	} {
		if _, ok := Verify(key, tok, now, maxAge); ok {
			t.Fatalf("malformed token %q verified", tok)
		}
	}
}

func TestMintRequiresKey(t *testing.T) {
	if _, _, err := Mint(nil, now); err == nil {
		t.Fatal("Mint with nil key succeeded")
	}
	if _, _, err := Mint([]byte("short"), now); err == nil {
		t.Fatal("Mint with short key succeeded")
	}
}

func TestVerifyRequiresKey(t *testing.T) {
	tok, _, _ := Mint(key, now)
	if _, ok := Verify(nil, tok, now, maxAge); ok {
		t.Fatal("Verify with nil key succeeded")
	}
}

// TestVerifyAnyAcceptsPreviousKey pins graceful rotation: a token
// signed by the OLD key still verifies once that key is listed in the
// previous-keys set alongside the new current key. Without this, every
// GOFASTR_SECRET rotation logs every user out at once.
func TestVerifyAnyAcceptsPreviousKey(t *testing.T) {
	tok, id, err := Mint(otherKey, now)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	got, ok := VerifyAny(key, [][]byte{otherKey}, tok, now, maxAge)
	if !ok || got != id {
		t.Fatalf("VerifyAny = %q, %v; want %q, true (token signed by previous key should verify during rotation)", got, ok, id)
	}
}

// TestVerifyAnyRejectsWhenPreviousMissing ensures a token signed by a
// key that is neither current nor listed as previous is rejected —
// rotation is a drain window, not a permanent backdoor.
func TestVerifyAnyRejectsWhenPreviousMissing(t *testing.T) {
	tok, _, _ := Mint(otherKey, now)
	if _, ok := VerifyAny(key, nil, tok, now, maxAge); ok {
		t.Fatal("VerifyAny accepted a token whose key is neither current nor listed as previous")
	}
}

// TestVerifyAnySignsWithCurrent ensures the rotation completes: a
// freshly-minted token (signed by the current key) verifies against the
// current key ALONE — it must not depend on a previous key being
// present, otherwise the operator could never drop the old key.
func TestVerifyAnySignsWithCurrent(t *testing.T) {
	tok, id, err := Mint(key, now)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	// A freshly-minted token verifies against the current key ALONE —
	// it must not depend on a previous key being present, otherwise the
	// operator could never drop the old key.
	got, ok := VerifyAny(key, nil, tok, now, maxAge)
	if !ok || got != id {
		t.Fatalf("freshly-minted token did not verify against current key alone: %q, %v", got, ok)
	}
	// And it must NOT verify once its signing key has been fully retired
	// — neither the new current nor any listed previous — so the drain
	// window actually closes.
	third := []byte("cccccccccccccccccccccccccccccccc")
	if _, ok := VerifyAny(otherKey, [][]byte{third}, tok, now, maxAge); ok {
		t.Fatal("current-signed token verified after its key was fully retired from current+previous")
	}
}

// TestVerifyAnyMultiplePreviousKeys covers the loop boundary: two prior
// keys (a long rollout) must each still verify.
func TestVerifyAnyMultiplePreviousKeys(t *testing.T) {
	keyA := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	keyB := []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	for _, old := range [][]byte{keyA, keyB} {
		tok, id, err := Mint(old, now)
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		got, ok := VerifyAny(key, [][]byte{keyA, keyB}, tok, now, maxAge)
		if !ok || got != id {
			t.Fatalf("token signed by a previous key in the multi-key set was rejected: %q, %v", got, ok)
		}
	}
}
