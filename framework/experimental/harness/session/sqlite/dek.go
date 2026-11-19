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

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// DEKHeader is the on-disk JSON of an AES-GCM-wrapped data-encryption key.
type DEKHeader struct {
	Version  int    `json:"version"`
	Nonce    []byte `json:"nonce"`
	Wrapped  []byte `json:"wrapped"`
	Metadata string `json:"meta,omitempty"`
}

const dekHeaderVersion = 1

// LoadOrCreateDEK unwraps the DEK at <dbPath>.dek with the 32-byte kek, creating one if the header cannot be read, and returns it with the header path.
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
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, "", err
	}
	if err := writeDEKHeader(headerPath, kek, dek, "kek=machine"); err != nil {
		return nil, "", err
	}
	return dek, headerPath, nil
}

// RotateKEK re-wraps the DEK under newKEK without touching the database; oldKEK no longer unwraps it.
func RotateKEK(dbPath string, oldKEK, newKEK []byte) error {
	dek, headerPath, err := LoadOrCreateDEK(dbPath, oldKEK)
	if err != nil {
		return err
	}
	return writeDEKHeader(headerPath, newKEK, dek, "kek=rotated")
}

// ExportDEK writes the DEK to outPath, owner-only, wrapped under a 32-byte symmetric recipientKey for escrow.
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
	tmp := outPath + ".tmp"
	if err := fileperm.WriteOwnerOnly(tmp, data); err != nil {
		return err
	}
	return os.Rename(tmp, outPath)
}

// ImportDEK unwraps an ExportDEK file with recipientKey and installs it as dbPath's DEK under localKEK.
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
	if err := fileperm.WriteOwnerOnly(tmp, data); err != nil {
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

// OpenWithKEK opens dbPath with EncryptionAtRest under the DEK that kek unwraps, creating the DEK on first use.
func OpenWithKEK(dbPath string, kek []byte) (*Store, error) {
	dek, _, err := LoadOrCreateDEK(dbPath, kek)
	if err != nil {
		return nil, err
	}
	return OpenEncrypted(dbPath, EncryptionAtRest, dek)
}
