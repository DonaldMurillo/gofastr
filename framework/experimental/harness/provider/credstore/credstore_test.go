package credstore

import (
	"errors"
	"path/filepath"
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
