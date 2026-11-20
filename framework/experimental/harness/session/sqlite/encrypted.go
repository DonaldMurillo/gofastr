package sqlite

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// EncryptionMode controls how the session log file is protected.
type EncryptionMode int

const (
	// EncryptionNone stores a plaintext SQLite file.
	EncryptionNone EncryptionMode = iota

	// EncryptionAtRest keeps the file AES-GCM encrypted as <path>.enc while the store is closed.
	EncryptionAtRest
)

// OpenEncrypted opens the log like Open; with EncryptionAtRest and a 32-byte key it first decrypts <path>.enc to an owner-only path.
func OpenEncrypted(path string, mode EncryptionMode, key []byte) (*Store, error) {
	switch mode {
	case EncryptionNone:
		return Open(path)
	case EncryptionAtRest:
		if len(key) != 32 {
			return nil, fmt.Errorf("sqlite: encryption key must be 32 bytes (got %d)", len(key))
		}
		encPath := path + ".enc"
		if _, err := os.Stat(encPath); err == nil {
			if err := decryptFile(encPath, path, key); err != nil {
				return nil, fmt.Errorf("sqlite: decrypt %s: %w", encPath, err)
			}
		}
		s, err := Open(path)
		if err != nil {
			return nil, err
		}
		s.encMode = mode
		s.encKey = append([]byte(nil), key...)
		return s, nil
	}
	return nil, fmt.Errorf("sqlite: unknown encryption mode %d", mode)
}

// CloseEncrypted closes the store and, under EncryptionAtRest, writes <path>.enc and removes the plaintext file.
func (s *Store) CloseEncrypted() error {
	if s.encMode != EncryptionAtRest {
		return s.Close()
	}
	if err := s.Close(); err != nil {
		return err
	}
	encPath := s.path + ".enc"
	if err := encryptFile(s.path, encPath, s.encKey); err != nil {
		return err
	}
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	for i := range s.encKey {
		s.encKey[i] = 0
	}
	return nil
}

func encryptFile(inPath, outPath string, key []byte) error {
	plain, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
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
	ct := gcm.Seal(nonce, nonce, plain, nil)
	tmp := outPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
		return err
	}
	if err := fileperm.WriteOwnerOnly(tmp, ct); err != nil {
		return err
	}
	return os.Rename(tmp, outPath)
}

func decryptFile(encPath, outPath string, key []byte) error {
	ct, err := os.ReadFile(encPath)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	if len(ct) < gcm.NonceSize() {
		return errors.New("sqlite: encrypted file too short")
	}
	nonce, body := ct[:gcm.NonceSize()], ct[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
		return err
	}
	tmp := outPath + ".tmp"
	if err := fileperm.WriteOwnerOnly(tmp, plain); err != nil {
		return err
	}
	return os.Rename(tmp, outPath)
}
