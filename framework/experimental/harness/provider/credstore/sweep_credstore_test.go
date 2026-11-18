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
	s, err := NewEncryptedFileStore(path, DeriveKey([]byte("p"), []byte("s")))
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
	if strings.Contains(string(raw), "plain-secret") || strings.Contains(string(raw), "zai") {
		t.Fatalf("store file holds plaintext: %q", raw)
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
