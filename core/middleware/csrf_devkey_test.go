package middleware

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestDevCSRFKeyFromFile_GeneratesAndPersists pins V3 #5: first call
// creates a stable key on disk; second call reads the SAME bytes back.
// Persistence across restarts is the entire point — without it, every
// dev-server reload rotates the key and every open tab 403s.
func TestDevCSRFKeyFromFile_GeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "dev-csrf.key")

	first, err := DevCSRFKeyFromFile(path)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(first) != 32 {
		t.Fatalf("want 32 bytes, got %d", len(first))
	}
	// File mode should be 0600 — the key is a signing secret.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("key file perm = %o, want 600", mode)
	}

	second, err := DevCSRFKeyFromFile(path)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("second call returned a different key — persistence broken")
	}
}

// TestDevCSRFKeyFromFile_RejectsWrongLength guards against a partial
// write or human-edited key file: a 12-byte file isn't a key, and
// reading it as one would silently produce signatures shorter than
// the algorithm expects.
func TestDevCSRFKeyFromFile_RejectsWrongLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.key")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := DevCSRFKeyFromFile(path)
	if err == nil {
		t.Fatal("expected error for wrong-length key file")
	}
}

// TestDevCSRFKeyFromFile_IntegratesWithCSRFMiddleware proves the
// helper actually plugs into the middleware: a token minted under the
// persisted key validates on a fresh middleware instance constructed
// later (simulating a restart with the same on-disk key).
func TestDevCSRFKeyFromFile_IntegratesWithCSRFMiddleware(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")

	key1, err := DevCSRFKeyFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := generateSignedCSRFToken(key1)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate restart: brand-new "process" loads the same on-disk key.
	key2, err := DevCSRFKeyFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !verifySignedCSRFToken(tok, key2) {
		t.Error("token minted by first key did not verify under reloaded key — defeats the purpose of persistence")
	}
}
