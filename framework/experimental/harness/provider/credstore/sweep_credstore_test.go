package credstore

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

func TestSweepStoreFileIsOwnerOnlyCiphertext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.enc")
	if _, err := NewEncryptedFileStore(path, make([]byte, 31)); err == nil {
		t.Fatal("a 31-byte key was accepted")
	}
	key := DeriveKey([]byte("p"), []byte("s"))
	s, err := NewEncryptedFileStore(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("zai", "default", "plain-secret"); err != nil {
		t.Fatal(err)
	}
	filepermtest.AssertOwnerOnly(t, path, 0o600)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "plain-secret") {
		t.Fatalf("store file holds plaintext: %q", raw)
	}
	reloaded, _ := NewEncryptedFileStore(path, key)
	if got, err := reloaded.Get("zai", "default"); err != nil || got != "plain-secret" {
		t.Fatalf("reload with the right key = %q, %v", got, err)
	}
	wrong, _ := NewEncryptedFileStore(path, DeriveKey([]byte("q"), []byte("s")))
	if got, err := wrong.Get("zai", "default"); err == nil {
		t.Fatalf("reload with a wrong key decrypted %q", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}
}

func TestSweepKeyDerivationIsStable(t *testing.T) {
	if got := hex.EncodeToString(MachineKey("host-a", []byte("aux"))); got != "b1439339fd4d73c280e309e67004cf301f2fed9e69fedd6ea380e65e441a26ab" {
		t.Errorf("MachineKey changed: %s; existing stores would no longer decrypt", got)
	}
	if got := hex.EncodeToString(DeriveKey([]byte("pass"), []byte("salt"))); got != "bda23529b56a51a4289dd0fae622d6dcbd4c15316dd3f4b12846dbb57f7c9b1b" {
		t.Errorf("DeriveKey changed: %s; existing stores would no longer decrypt", got)
	}
}
