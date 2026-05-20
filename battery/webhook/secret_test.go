package webhook

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
)

// ----- SecretCodec contract --------------------------------------------------

func TestNoopSecretCodec_PassesThrough(t *testing.T) {
	c := NoopSecretCodec{}
	out, err := c.Encode("hunter2")
	if err != nil || out != "hunter2" {
		t.Fatalf("Encode: got (%q, %v)", out, err)
	}
	dec, err := c.Decode("hunter2")
	if err != nil || dec != "hunter2" {
		t.Fatalf("Decode: got (%q, %v)", dec, err)
	}
}

func TestAESGCMSecretCodec_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	c, err := NewAESGCMSecretCodec(key)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	const plain = "topsecret-subscriber-key"

	enc, err := c.Encode(plain)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if enc == plain {
		t.Fatalf("encoded form must differ from plaintext")
	}
	if !strings.HasPrefix(enc, "wbenc:v1:") {
		t.Fatalf("expected version-tagged ciphertext, got %q", enc)
	}
	dec, err := c.Decode(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec != plain {
		t.Fatalf("round-trip lost data: got %q want %q", dec, plain)
	}
}

func TestAESGCMSecretCodec_DecodeRejectsTamper(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	c, _ := NewAESGCMSecretCodec(key)
	enc, _ := c.Encode("hello")
	// Flip a byte in the middle of the payload.
	bad := []byte(enc)
	bad[len(bad)-5] ^= 0x01
	if _, err := c.Decode(string(bad)); err == nil {
		t.Fatalf("tampered ciphertext must NOT decode successfully")
	}
}

func TestAESGCMSecretCodec_DecodeAcceptsLegacyPlaintext(t *testing.T) {
	// Rows written before encryption rolled out have no prefix; the
	// codec must still hand them back unchanged so deployments can
	// migrate without a rewrite job.
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	c, _ := NewAESGCMSecretCodec(key)
	got, err := c.Decode("legacy-plaintext-secret")
	if err != nil || got != "legacy-plaintext-secret" {
		t.Fatalf("unprefixed legacy value should pass through, got (%q, %v)", got, err)
	}
}

func TestAESGCMSecretCodec_RejectsShortKey(t *testing.T) {
	if _, err := NewAESGCMSecretCodec([]byte("too-short")); err == nil {
		t.Fatal("expected error on non-AES key length")
	}
}

// ----- SQL store with codec stores ciphertext, returns plaintext ------------

func TestSQLStore_WithSecretCodecEncryptsAtRest(t *testing.T) {
	db, _ := openSQLStore(t)

	key := make([]byte, 32)
	_, _ = rand.Read(key)
	codec, err := NewAESGCMSecretCodec(key)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	store, err := NewSQLStore(db, WithSQLSecretCodec(codec))
	if err != nil {
		t.Fatalf("sql store: %v", err)
	}

	const plain = "shared-with-customer"
	ctx := context.Background()
	if err := store.AddSubscriber(ctx, Subscriber{
		ID: "abc", URL: "https://x.example.com/h", Secret: plain,
		Events: []string{"*"}, Active: true,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Direct DB read — the column MUST NOT contain the plaintext.
	var rawSecret string
	row := db.QueryRow("SELECT secret FROM webhook_subscribers WHERE id = ?", "abc")
	if err := row.Scan(&rawSecret); err != nil {
		t.Fatalf("raw scan: %v", err)
	}
	if rawSecret == plain {
		t.Fatalf("secret persisted as plaintext: %q", rawSecret)
	}
	if !strings.HasPrefix(rawSecret, "wbenc:") {
		t.Fatalf("expected version-prefixed ciphertext at rest, got %q", rawSecret)
	}

	// Round-trip through the store — caller sees plaintext.
	got, err := store.GetSubscriber(ctx, "abc")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.Secret != plain {
		t.Fatalf("Store must decrypt on read; got %q want %q", got.Secret, plain)
	}

	// ListSubscribers also decrypts.
	all, err := store.ListSubscribers(ctx)
	if err != nil || len(all) != 1 || all[0].Secret != plain {
		t.Fatalf("list: %v %+v", err, all)
	}
}
