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
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
	"golang.org/x/crypto/pbkdf2"
)

// Store holds secrets addressed by a (provider, account) pair.
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

// EncryptedFileStore is a Store kept as an AES-GCM encrypted JSON file.
type EncryptedFileStore struct {
	path string
	key  []byte

	mu     sync.Mutex
	loaded bool
	data   storeData
}

type storeData struct {
	Entries map[string]string `json:"entries"`
}

// NewEncryptedFileStore returns a store at path sealed with key, which must be 32 bytes (see DeriveKey and MachineKey).
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

// DeriveKey returns a 32-byte key from a passphrase and a stable per-install salt using PBKDF2-SHA256.
func DeriveKey(passphrase, salt []byte) []byte {
	return pbkdf2.Key(passphrase, salt, 200_000, 32, sha256.New)
}

// MachineKey derives a 32-byte key from machine-stable inputs, for runs with no passphrase.
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
	if v, ok := s.data.Entries[credentialKey(provider, account)]; ok {
		return v, nil
	}
	key := legacyCredentialKey(provider, account)
	v, ok := s.data.Entries[key]
	if !ok {
		return "", ErrNotFound
	}
	if legacyKeyAmbiguous(key) {
		return "", fmt.Errorf("%w: %q", ErrAmbiguousLegacyKey, key)
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
	s.data.Entries[credentialKey(provider, account)] = secret
	delete(s.data.Entries, legacyCredentialKey(provider, account))
	return s.saveLocked()
}

func (s *EncryptedFileStore) Delete(provider, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	delete(s.data.Entries, credentialKey(provider, account))
	delete(s.data.Entries, legacyCredentialKey(provider, account))
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
		if entry, ok := parseCredentialKey(k); ok {
			out = append(out, entry)
		} else if legacyKeyAmbiguous(k) {
			out = append(out, legacyCandidates(k)...)
		}
	}
	return out, nil
}

func credentialKey(provider, account string) string {
	enc := base64.RawURLEncoding
	return "v1:" + enc.EncodeToString([]byte(provider)) + ":" + enc.EncodeToString([]byte(account))
}

func legacyCredentialKey(provider, account string) string {
	return provider + "|" + account
}

func parseCredentialKey(key string) (Entry, bool) {
	if strings.HasPrefix(key, "v1:") {
		rest := strings.TrimPrefix(key, "v1:")
		provider, account, ok := strings.Cut(rest, ":")
		if ok {
			enc := base64.RawURLEncoding
			providerBytes, providerErr := enc.DecodeString(provider)
			accountBytes, accountErr := enc.DecodeString(account)
			if providerErr == nil && accountErr == nil {
				return Entry{Provider: string(providerBytes), Account: string(accountBytes)}, true
			}
		}
	}
	return parseLegacyCredentialKey(key)
}

func parseLegacyCredentialKey(key string) (Entry, bool) {
	provider, account, ok := strings.Cut(key, "|")
	if !ok || legacyKeyAmbiguous(key) {
		return Entry{}, false
	}
	return Entry{Provider: provider, Account: account}, true
}

func legacyKeyAmbiguous(key string) bool {
	return strings.Count(key, "|") > 1
}

func legacyCandidates(key string) []Entry {
	var out []Entry
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			out = append(out, Entry{Provider: key[:i], Account: key[i+1:]})
		}
	}
	return out
}

func (s *EncryptedFileStore) loadLocked() error {
	if s.loaded {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.data = storeData{Entries: make(map[string]string)}
		s.loaded = true
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
		s.loaded = true
		return nil
	}
	var data storeData
	if err := json.Unmarshal(plain, &data); err != nil {
		return fmt.Errorf("credstore: parse: %w", err)
	}
	if data.Entries == nil {
		data.Entries = make(map[string]string)
	}
	s.data = data
	s.loaded = true
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
	tmp := s.path + ".tmp"
	if err := fileperm.WriteOwnerOnly(tmp, enc); err != nil {
		return fmt.Errorf("credstore: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("credstore: rename: %w", err)
	}
	return nil
}

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

// ErrAmbiguousLegacyKey is returned by Get when the pair's only entry is a legacy key with several possible splits; Put or Delete on the right pair clears it.
var ErrAmbiguousLegacyKey = errors.New("credstore: ambiguous legacy credential key; re-enter the credential")
