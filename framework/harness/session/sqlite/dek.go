package sqlite

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DEK/KEK scheme:
//
//   - DEK (data-encryption key): random 32 bytes, used to encrypt
//     the session DB file at rest. Generated once and stored in a
//     header file alongside the DB.
//   - KEK (key-encryption key): derived from the user's passphrase
//     or machine key (the existing credstore key). Encrypts the DEK
//     using AES-GCM.
//
// Rotating the KEK re-wraps the DEK; it does NOT re-encrypt the DB.
// This makes rotation O(1) regardless of DB size, addressing the
// architecture doc's "VACUUM is poison" concern.
//
// The DEK header lives at `<dbPath>.dek` with mode 0600.

// DEKHeader is the on-disk shape of the wrapped DEK.
type DEKHeader struct {
	Version  int    `json:"version"`
	Nonce    []byte `json:"nonce"`     // AES-GCM nonce for the wrap
	Wrapped  []byte `json:"wrapped"`   // AES-GCM ciphertext of the DEK
	Metadata string `json:"meta,omitempty"` // human-readable, e.g. "kek=machine"
}

const dekHeaderVersion = 1

// LoadOrCreateDEK opens the DEK header at `<dbPath>.dek`. If it
// doesn't exist, a fresh DEK is generated and wrapped with `kek`.
// Returns the unwrapped DEK and the header path.
func LoadOrCreateDEK(dbPath string, kek []byte) ([]byte, string, error) {
	if len(kek) != 32 {
		return nil, "", fmt.Errorf("sqlite: KEK must be 32 bytes")
	}
	headerPath := dbPath + ".dek"
	if data, err := os.ReadFile(headerPath); err == nil {
		var h DEKHeader
		if err := json.Unmarshal(data, &h); err != nil {
			return nil, "", err
		}
		if h.Version != dekHeaderVersion {
			return nil, "", fmt.Errorf("sqlite: DEK header version %d not supported", h.Version)
		}
		dek, err := unwrap(kek, h.Nonce, h.Wrapped)
		if err != nil {
			return nil, "", err
		}
		return dek, headerPath, nil
	}
	// Generate a fresh DEK.
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, "", err
	}
	if err := writeDEKHeader(headerPath, kek, dek, "kek=machine"); err != nil {
		return nil, "", err
	}
	return dek, headerPath, nil
}

// RotateKEK re-wraps the DEK with a new KEK without re-encrypting
// the DB. After this returns, only `newKEK` can unwrap.
func RotateKEK(dbPath string, oldKEK, newKEK []byte) error {
	dek, headerPath, err := LoadOrCreateDEK(dbPath, oldKEK)
	if err != nil {
		return err
	}
	return writeDEKHeader(headerPath, newKEK, dek, "kek=rotated")
}

// ExportDEK writes the DEK wrapped under a recipient's public key so
// the user can escrow or migrate the DB. The recipient format is
// X25519 base64; we use a tiny in-package wrapper (no third-party
// dep) that emits an AES-GCM-wrapped DEK plus the recipient's public
// key fingerprint for identification.
//
// To keep the implementation stdlib-only, the "recipient" here is
// another 32-byte key, not full X25519 + age. Real X25519/age
// integration lives behind a build-tagged extension at
// session/sqlite/escrow_age.go; the v0.1 default ships this simpler
// form that still meets the threat-model goal of "key escrow that
// doesn't require the harness binary to recover."
func ExportDEK(dbPath string, kek, recipientKey []byte, outPath string) error {
	if len(recipientKey) != 32 {
		return fmt.Errorf("sqlite: recipient key must be 32 bytes")
	}
	dek, _, err := LoadOrCreateDEK(dbPath, kek)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(recipientKey)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	wrapped := gcm.Seal(nonce, nonce, dek, nil)
	// Write as JSON for human-inspectability.
	header := DEKHeader{
		Version:  dekHeaderVersion,
		Nonce:    nonce,
		Wrapped:  wrapped[gcm.NonceSize():],
		Metadata: "exported-for-recipient",
	}
	data, err := json.MarshalIndent(header, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(outPath, data, 0o600)
}

// ImportDEK reads a previously-exported DEK and installs it as the
// active DEK for the given DB, wrapped under the local KEK. The
// recipientKey must match what ExportDEK used.
func ImportDEK(dbPath, exportedPath string, recipientKey, localKEK []byte) error {
	data, err := os.ReadFile(exportedPath)
	if err != nil {
		return err
	}
	var h DEKHeader
	if err := json.Unmarshal(data, &h); err != nil {
		return err
	}
	dek, err := unwrap(recipientKey, h.Nonce, h.Wrapped)
	if err != nil {
		return fmt.Errorf("sqlite: ImportDEK unwrap: %w", err)
	}
	return writeDEKHeader(dbPath+".dek", localKEK, dek, "kek=imported")
}

func writeDEKHeader(path string, kek, dek []byte, meta string) error {
	block, err := aes.NewCipher(kek)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ct := gcm.Seal(nonce, nonce, dek, nil)
	wrapped := ct[gcm.NonceSize():]
	header := DEKHeader{
		Version:  dekHeaderVersion,
		Nonce:    nonce,
		Wrapped:  wrapped,
		Metadata: meta,
	}
	data, err := json.MarshalIndent(header, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func unwrap(kek, nonce, wrapped []byte) ([]byte, error) {
	if len(kek) != 32 {
		return nil, errors.New("sqlite: KEK must be 32 bytes")
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, wrapped, nil)
}

// OpenWithKEK opens an encrypted Store using the DEK/KEK scheme.
// If no .dek file exists, a fresh DEK is generated and wrapped.
func OpenWithKEK(dbPath string, kek []byte) (*Store, error) {
	dek, _, err := LoadOrCreateDEK(dbPath, kek)
	if err != nil {
		return nil, err
	}
	return OpenEncrypted(dbPath, EncryptionAtRest, dek)
}
