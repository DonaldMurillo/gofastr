package sqlite

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// r5ModeKey returns a fresh 32-byte key (own helper; phase-1 red files
// may add same-shaped ones).
func r5ModeKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("setup broken: rand: %v", err)
	}
	return k
}

// Pins: secret-material writes in the DEK scheme are owner-only on
// CONTRACT-QUESTION: ExportDEK's output is a wrapped data-encryption
// key — the one file that decrypts the whole session DB. writeDEKHeader
// (dek.go:180-184, 50 lines below) treats identical bytes as
// temp(0600)+rename atomic material; is the exported copy at an
// operator-chosen outPath exempt from both the atomicity and the
// mode-tightening half of that grammar?
// Property: secret-material writes in the DEK scheme are owner-only on
// overwrite (and atomic, per the in-family twin).
// Surfaces: dek.go::ExportDEK :130 — direct os.WriteFile(outPath, data,
// 0o600); the mode argument only applies at CREATE.
// Finding: a pre-existing 0644 outPath (operator re-exports over an
// old copy, restores a checkout, umask drift) is overwritten with the
// freshly-wrapped DEK but stays group/world-readable. writeDEKHeader
// does tmp+rename for the same payload class.
// Fix direction: reuse writeDEKHeader's tmp+rename spelling for the
// export path (which also tightens the mode only if combined with a
// 0600 pre-chmod or O_EXCL create — mirror cmd/gofastr pack.go::
// writeSecretFile's open→chmod-handle→write for the tighten half).
func TestExportDEKRedOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")
	kek := r5ModeKey(t)
	recipient := r5ModeKey(t)
	outPath := filepath.Join(dir, "exported.dek")

	if err := os.WriteFile(outPath, []byte("stale export\n"), 0o644); err != nil {
		t.Fatalf("setup broken: seed outPath: %v", err)
	}
	if err := os.Chmod(outPath, 0o644); err != nil {
		t.Fatalf("setup broken: chmod outPath: %v", err) // defeat umask so the precondition is exact
	}

	if err := ExportDEK(dbPath, kek, recipient, outPath); err != nil {
		t.Fatalf("setup broken: ExportDEK: %v", err)
	}

	// Non-vacuous: the file must carry this export's bytes, not the stale ones.
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("setup broken: read outPath: %v", err)
	}
	if !strings.Contains(string(body), "exported-for-recipient") {
		t.Fatalf("setup broken: outPath does not carry the fresh export — overwrite did not happen: %q", body)
	}

	fi, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("setup broken: stat outPath: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: [dek-export-atomicity] exported DEK %s is %v after overwrite — ExportDEK's os.WriteFile(outPath, ..., 0o600) (dek.go:130) only applies its mode at CREATE, so a pre-existing 0644 export keeps the wrapped data-encryption key group/world-readable; the twin writeDEKHeader (dek.go:180-184) writes the same payload class via temp+rename", outPath, fi.Mode().Perm())
	}
}

// Pins: decrypted plaintext session DB is written owner-only
// CONTRACT-QUESTION: decryptFile materializes the FULL plaintext session
// DB at the recurring `path` on every OpenEncrypted. Its mirror twin
// encryptFile (encrypted.go:116-124) writes the .enc via temp+rename
// ("Atomic write via .tmp + rename"); is the decrypt direction exempt
// from the atomic grammar and from mode-tightening of a pre-existing
// plaintext path?
// Property: decrypted plaintext session DB is written owner-only
// (0600) and atomically — OpenEncrypted's own doc (encrypted.go:43)
// promises "The unencrypted path is created with mode 0600".
// Surfaces: encrypted.go::decryptFile :151 — direct os.WriteFile(
// outPath, plain, 0o600) of the whole plaintext at the recurring path;
// mode applies at CREATE only.
// Finding: OpenEncrypted over a leftover/restored 0644 plaintext path
// (previous crash before CloseEncrypted's remove, restored backup,
// umask drift) decrypts the full DB into it and leaves it
// group/world-readable for the whole session lifetime.
// Fix direction: mirror encryptFile's tmp+rename, with a 0600-tightened
// handle (pack.go::writeSecretFile grammar) so a pre-existing weaker
// mode is corrected before plaintext lands.
func TestDecryptRedOwnerOnlyAtomic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.db")
	key := r5ModeKey(t)

	// Build a valid .enc the way the sibling round-trip test does:
	// open encrypted, append one event, close (writes path.enc,
	// removes plaintext).
	s, err := OpenEncrypted(path, EncryptionAtRest, key)
	if err != nil {
		t.Fatalf("setup broken: OpenEncrypted: %v", err)
	}
	env, err := control.EncodeEvent(1, control.TextDelta{Text: "secret-payload"}, ids.NewSessionID(), ids.NewClientID(), time.Now())
	if err != nil {
		t.Fatalf("setup broken: EncodeEvent: %v", err)
	}
	if err := s.AppendEvent(context.Background(), env); err != nil {
		t.Fatalf("setup broken: AppendEvent: %v", err)
	}
	if err := s.CloseEncrypted(); err != nil {
		t.Fatalf("setup broken: CloseEncrypted: %v", err)
	}
	if _, err := os.Stat(path + ".enc"); err != nil {
		t.Fatalf("setup broken: .enc missing after CloseEncrypted: %v", err)
	}

	// Plant a weak-mode plaintext at the recurring decrypt target.
	if err := os.WriteFile(path, []byte("stale plaintext\n"), 0o644); err != nil {
		t.Fatalf("setup broken: seed plaintext: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("setup broken: chmod plaintext: %v", err) // defeat umask so the precondition is exact
	}

	s2, err := OpenEncrypted(path, EncryptionAtRest, key)
	if err != nil {
		t.Fatalf("setup broken: reopen OpenEncrypted: %v", err)
	}
	defer func() { _ = s2.CloseEncrypted() }()

	// Non-vacuous: decryptFile must have overwritten the stale bytes
	// with the recovered SQLite plaintext.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("setup broken: read plaintext: %v", err)
	}
	if !bytes.HasPrefix(body, []byte("SQLite format 3")) {
		t.Fatalf("setup broken: plaintext does not carry the decrypted DB — overwrite did not happen: %q", body[:min(len(body), 32)])
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("setup broken: stat plaintext: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: [decrypt-direct-write] decrypted plaintext session DB %s is %v after OpenEncrypted — decryptFile's os.WriteFile(outPath, plain, 0o600) (encrypted.go:151) only applies its mode at CREATE, so a pre-existing 0644 plaintext path keeps the full decrypted transcript DB group/world-readable while OpenEncrypted's own doc (encrypted.go:43) promises mode 0600; the mirror twin encryptFile (encrypted.go:116-124) writes via temp+rename", path, fi.Mode().Perm())
	}
}
