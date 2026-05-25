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
)

// EncryptionMode controls how the session log file is protected.
type EncryptionMode int

const (
	// EncryptionNone — plaintext SQLite file. The default for v0.1.
	EncryptionNone EncryptionMode = iota

	// EncryptionAtRest — the SQLite file is encrypted in place
	// using file-level AES-GCM whenever the harness is not running.
	// On Open(), the file is decrypted to a sidecar; on Close(), the
	// sidecar is re-encrypted and the decrypted copy is removed.
	//
	// The architecture doc notes that whole-file encryption + VACUUM
	// is a problem for very large logs; this mode accepts that
	// limitation in exchange for a real defense against laptop theft.
	// Page-level encryption requires a custom SQLite VFS and is a
	// genuinely larger sub-project (acknowledged in the spec but not
	// blocking v0.1).
	EncryptionAtRest
)

// OpenEncrypted opens a SQLite session log with the requested
// encryption mode. The key (32 bytes) protects the at-rest file when
// mode == EncryptionAtRest.
//
//   - mode = EncryptionNone: behaves exactly like Open.
//   - mode = EncryptionAtRest: if `<path>.enc` exists, decrypt to
//     `path` first; then open. The encrypted form is written on Close.
//
// The unencrypted `path` is created with mode 0600 and lives in the
// XDG state dir, which is owner-readable only on every supported OS.
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

// CloseEncrypted closes the Store and re-encrypts the file when
// EncryptionAtRest is active. The plaintext file is removed
// afterwards.
func (s *Store) CloseEncrypted() error {
	if s.encMode != EncryptionAtRest {
		return s.Close()
	}
	if err := s.Close(); err != nil {
		return err
	}
	// Best effort: encrypt the file in place.
	encPath := s.path + ".enc"
	if err := encryptFile(s.path, encPath, s.encKey); err != nil {
		return err
	}
	// Remove the plaintext file. The user keeps only the .enc.
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Zero the key in memory.
	for i := range s.encKey {
		s.encKey[i] = 0
	}
	return nil
}

// encryptFile encrypts inPath → outPath using AES-GCM. The first
// 12 bytes of outPath are the nonce, followed by ciphertext+tag.
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
	// Atomic write via .tmp + rename.
	tmp := outPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, ct, 0o600); err != nil {
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
	return os.WriteFile(outPath, plain, 0o600)
}
