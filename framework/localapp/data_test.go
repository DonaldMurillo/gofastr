package localapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

// TestOpenReusesWhatItMinted: the first Open creates the directory and
// every file owner-only; the second returns the same secret and identity.
func TestOpenReusesWhatItMinted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close() })
	filepermtest.AssertOwnerOnly(t, dir, 0o700)
	for _, f := range []string{DBFile, SecretFile, IdentityFile} {
		filepermtest.AssertOwnerOnly(t, filepath.Join(dir, f), 0o600)
	}
	var one int
	if err := first.DB.QueryRow("SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("SELECT 1 = (%d, %v)", one, err)
	}
	if len(first.Secret) < 40 || !Valid(first.User.GetID()) {
		t.Fatalf("secret %d chars, identity %q", len(first.Secret), first.User.GetID())
	}

	second, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	if second.Secret != first.Secret || second.User.GetID() != first.User.GetID() {
		t.Fatal("a second Open minted a new secret or identity")
	}
}

func TestOpenRefusesARelativeDir(t *testing.T) {
	for _, dir := range []string{"", "data", "./data"} {
		if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "absolute") {
			t.Errorf("Open(%q) = %v, want an absolute-path refusal", dir, err)
		}
	}
}

// A damaged secret file is refused, not used to sign sessions and not
// silently replaced (replacing it signs every session out).
func TestSecretRefusesADamagedFile(t *testing.T) {
	for name, content := range map[string]string{
		"empty":      "",
		"newline":    "abc\n",
		"truncated":  "abc",
		"short":      strings.Repeat("A", 40),
		"not base64": strings.Repeat("*", 43),
		// base64 decoding skips CR and LF, so a full-length secret with a
		// line ending decodes to 32 bytes; signing with it would use a
		// different key from the one minted.
		"full plus newline": strings.Repeat("A", 43) + "\n",
		"full plus CRLF":    strings.Repeat("A", 43) + "\r\n",
		"inner newline":     strings.Repeat("A", 20) + "\n" + strings.Repeat("A", 23),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, SecretFile), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if s, err := LoadOrMintSecret(dir); err == nil {
				t.Fatalf("LoadOrMintSecret = %q, want a refusal", s)
			}
		})
	}
}

// OpenDB opens app.db at the path it seeded owner-only even when the
// directory name holds a character a SQLite URI treats specially: '#'
// starts a fragment, '?' the parameters, '%' an escape.
func TestOpenDBInAnOddlyNamedDir(t *testing.T) {
	for _, name := range []string{"my#app", "my?app", "my%41pp"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Skipf("mkdir %q: %v", name, err)
			}
			db, err := OpenDB(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec("CREATE TABLE marker (x INTEGER)"); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Dir(dir))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Name() != name {
					t.Errorf("SQLite wrote %q beside %q", e.Name(), name)
				}
			}
			again, err := OpenDB(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer again.Close()
			var n int
			if err := again.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'marker'").Scan(&n); err != nil || n != 1 {
				t.Fatalf("app.db in %q lost its table: n=%d err=%v", name, n, err)
			}
		})
	}
}
