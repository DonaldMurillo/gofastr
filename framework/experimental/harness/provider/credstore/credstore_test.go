package credstore

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *EncryptedFileStore {
	t.Helper()
	dir := t.TempDir()
	key := DeriveKey([]byte("test-passphrase"), []byte("test-salt-stable"))
	s, err := NewEncryptedFileStore(filepath.Join(dir, "creds.enc"), key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPutGet(t *testing.T) {
	s := newStore(t)
	if err := s.Put("openrouter", "default", "sk-12345"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("openrouter", "default")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-12345" {
		t.Errorf("got %q, want sk-12345", got)
	}
}

func TestGetMissing(t *testing.T) {
	s := newStore(t)
	if _, err := s.Get("nope", "default"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRoundTripAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	key := DeriveKey([]byte("test-passphrase"), []byte("test-salt-stable"))
	path := filepath.Join(dir, "creds.enc")

	s1, err := NewEncryptedFileStore(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Put("zai", "default", "z-secret"); err != nil {
		t.Fatal(err)
	}

	// New instance, same path + key: read back.
	s2, err := NewEncryptedFileStore(path, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get("zai", "default")
	if err != nil {
		t.Fatal(err)
	}
	if got != "z-secret" {
		t.Errorf("got %q", got)
	}
}

func TestWrongKeyFailsDecrypt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.enc")
	key := DeriveKey([]byte("right"), []byte("salt"))
	s, _ := NewEncryptedFileStore(path, key)
	_ = s.Put("zai", "default", "secret")

	wrongKey := DeriveKey([]byte("wrong"), []byte("salt"))
	bad, _ := NewEncryptedFileStore(path, wrongKey)
	if _, err := bad.Get("zai", "default"); err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestDelete(t *testing.T) {
	s := newStore(t)
	_ = s.Put("zai", "default", "x")
	_ = s.Delete("zai", "default")
	if _, err := s.Get("zai", "default"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("entry not deleted: err=%v", err)
	}
}

func TestList(t *testing.T) {
	s := newStore(t)
	_ = s.Put("openrouter", "default", "a")
	_ = s.Put("zai", "default", "b")
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("list len = %d, want 2", len(entries))
	}
}

func TestMachineKey(t *testing.T) {
	k1 := MachineKey("host-a", []byte("aux"))
	k2 := MachineKey("host-a", []byte("aux"))
	if string(k1) != string(k2) {
		t.Error("MachineKey not deterministic")
	}
	k3 := MachineKey("host-b", []byte("aux"))
	if string(k1) == string(k3) {
		t.Error("MachineKey same across hostnames")
	}
}

// TestLoadFailureNotLatched pins that a failed unlock keeps failing
// loudly instead of degrading into "no credentials stored".
//
// loadLocked sets s.loaded = true BEFORE attempting decrypt, so after
// one wrong-key (or corrupt-file) error every later Get takes the
// loaded fast path against zeroed data and returns ErrNotFound. Code
// that treats ErrNotFound as "no key configured, fall back to env"
// then silently downgrades the operator onto a different credential
// path after a single transient report.
//
// The latch is worse than a misreport: Put also takes the loaded fast
// path, so a write after a failed load silently re-encrypts the ENTIRE
// store under the wrong key (saveLocked overwrites creds.enc),
// destroying the original ciphertext for every correct-key reader.
// Pass-2 recon believed Put propagated the load error; the assertion
// below disproves that — observed: Put returns nil and the right-key
// instance can no longer decrypt the file. Fix: latch loaded only on
// success, so both Get and Put keep surfacing the decrypt error.
func TestLoadFailureNotLatched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.enc")
	key := DeriveKey([]byte("right"), []byte("salt"))
	s, _ := NewEncryptedFileStore(path, key)
	if err := s.Put("zai", "default", "secret"); err != nil {
		t.Fatal(err)
	}

	wrong := DeriveKey([]byte("wrong"), []byte("salt"))
	bad, _ := NewEncryptedFileStore(path, wrong)
	if _, err := bad.Get("zai", "default"); err == nil {
		t.Fatal("expected first wrong-key Get to fail")
	}

	// The pin: the decrypt failure must repeat, not latch into a
	// masquerading empty store.
	if _, err := bad.Get("zai", "default"); err == nil {
		t.Errorf("SECURITY: second wrong-key Get succeeded; store latched as loaded after a decrypt failure")
	} else if !containsDecrypt(err) {
		t.Errorf("second wrong-key Get = %v, want the decrypt error again (not %v)", err, ErrNotFound)
	}
	// Writes must stay fail-closed: no silent overwrite under the
	// wrong key.
	if err := bad.Put("zai", "default", "hijack"); err == nil {
		t.Errorf("SECURITY: Put after a failed load silently re-encrypted the store under the wrong key")
	}
	// The file itself is untouched: a correct-key instance still loads.
	good, _ := NewEncryptedFileStore(path, key)
	if v, err := good.Get("zai", "default"); err != nil || v != "secret" {
		t.Errorf("right-key instance should still load: got (%q, %v)", v, err)
	}
}

func containsDecrypt(err error) bool {
	return err != nil && strings.Contains(err.Error(), "decrypt")
}
