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
