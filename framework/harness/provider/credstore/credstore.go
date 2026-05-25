// Package credstore implements credential storage.
//
// Per § Credentials: encrypted-file is the primary store (AES-GCM
// with a passphrase- or machine-bound key). OS keychain integrations
// are opt-in build-tagged plugins per platform — those live next to
// this file with `_darwin.go` / `_linux.go` / `_windows.go` suffixes
// and call back into Store.
package credstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/pbkdf2"
)

// Store is the credential store interface. Tokens are addressed by a
// {provider, account} key pair — most providers have one account per
// install (the API key); Copilot's auth flow has an explicit
// account-per-GitHub-user model.
type Store interface {
	Get(provider, account string) (string, error)
	Put(provider, account, secret string) error
	Delete(provider, account string) error
	List() ([]Entry, error)
}

// Entry describes a stored credential without its secret value.
type Entry struct {
	Provider string
	Account  string
}

// EncryptedFileStore is the primary implementation: an AES-GCM
// encrypted JSON file at the configured path.
//
// File layout (post-decryption):
//
//	{
//	  "entries": {
//	    "openrouter|default": "<api-key>",
//	    "zai|default":        "<api-key>"
//	  }
//	}
type EncryptedFileStore struct {
	path string
	key  []byte // 32 bytes

	mu     sync.Mutex
	loaded bool
	data   storeData
}

type storeData struct {
	Entries map[string]string `json:"entries"`
}

// NewEncryptedFileStore returns a Store backed by an encrypted file at
// the given path. The 32-byte key is derived elsewhere (DeriveKey
// from passphrase, or machine-bound key from /etc/machine-id +
// hostname).
func NewEncryptedFileStore(path string, key []byte) (*EncryptedFileStore, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("credstore: key must be 32 bytes (got %d)", len(key))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	cp := make([]byte, 32)
	copy(cp, key)
	return &EncryptedFileStore{path: path, key: cp}, nil
}

// DeriveKey returns a 32-byte key from a passphrase using PBKDF2-SHA256.
//
// salt should be a stable per-install value (e.g. a UUID generated on
// first run and stored in `~/.config/gofastr/harness/salt`).
//
// The PBKDF2 iteration count is intentionally high (200k) since
// unlocking is rare — once per harness boot for interactive use.
func DeriveKey(passphrase, salt []byte) []byte {
	return pbkdf2.Key(passphrase, salt, 200_000, 32, sha256.New)
}

// MachineKey derives a 32-byte key from machine-stable inputs without
// any user passphrase. Used for headless/CI where there's no human to
// type a passphrase. The threat model accepts that anyone with root
// on the machine can re-derive the same key — that's expected for the
// CI use case.
func MachineKey(hostname string, extra []byte) []byte {
	h := sha256.New()
	h.Write([]byte("gofastr-harness-machine-key-v1\x00"))
	h.Write([]byte(hostname))
	h.Write([]byte{0})
	h.Write(extra)
	return h.Sum(nil)
}

func (s *EncryptedFileStore) Get(provider, account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return "", err
	}
	key := provider + "|" + account
	v, ok := s.data.Entries[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *EncryptedFileStore) Put(provider, account, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	if s.data.Entries == nil {
		s.data.Entries = make(map[string]string)
	}
	s.data.Entries[provider+"|"+account] = secret
	return s.saveLocked()
}

func (s *EncryptedFileStore) Delete(provider, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	delete(s.data.Entries, provider+"|"+account)
	return s.saveLocked()
}

func (s *EncryptedFileStore) List() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(s.data.Entries))
	for k := range s.data.Entries {
		// Split "provider|account"
		for i := 0; i < len(k); i++ {
			if k[i] == '|' {
				out = append(out, Entry{Provider: k[:i], Account: k[i+1:]})
				break
			}
		}
	}
	return out, nil
}

func (s *EncryptedFileStore) loadLocked() error {
	if s.loaded {
		return nil
	}
	s.loaded = true
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.data = storeData{Entries: make(map[string]string)}
		return nil
	}
	if err != nil {
		return fmt.Errorf("credstore: read %s: %w", s.path, err)
	}
	plain, err := decrypt(s.key, raw)
	if err != nil {
		return fmt.Errorf("credstore: decrypt: %w", err)
	}
	if len(plain) == 0 {
		s.data = storeData{Entries: make(map[string]string)}
		return nil
	}
	if err := json.Unmarshal(plain, &s.data); err != nil {
		return fmt.Errorf("credstore: parse: %w", err)
	}
	if s.data.Entries == nil {
		s.data.Entries = make(map[string]string)
	}
	return nil
}

func (s *EncryptedFileStore) saveLocked() error {
	plain, err := json.Marshal(&s.data)
	if err != nil {
		return err
	}
	enc, err := encrypt(s.key, plain)
	if err != nil {
		return err
	}
	// Atomic write: write to <path>.tmp then rename.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, enc, 0o600); err != nil {
		return fmt.Errorf("credstore: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("credstore: rename: %w", err)
	}
	return nil
}

// File format on disk: base64(<nonce 12><ciphertext><tag>).
func encrypt(key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nonce, nonce, plain, nil)
	// base64-wrap so the file is human-inspectable as text and
	// survives Windows file editors that mangle binary.
	encoded := base64.StdEncoding.EncodeToString(ct)
	return []byte(encoded), nil
}

func decrypt(key, b64 []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(string(b64))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("credstore: ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// ErrNotFound is returned by Get when the (provider, account) pair has no entry.
var ErrNotFound = errors.New("credstore: not found")
