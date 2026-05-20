package webhook

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// SecretCodec encodes and decodes subscriber secrets when they're
// persisted by a Store. Implementations must be safe for concurrent
// use.
//
// Encode is called at write time (AddSubscriber); Decode is called at
// read time (GetSubscriber / ListSubscribers). The codec is purely a
// storage concern — the Manager only ever sees plaintext.
type SecretCodec interface {
	Encode(plaintext string) (string, error)
	Decode(encoded string) (string, error)
}

// NoopSecretCodec is the default — it stores secrets as-is. Use it
// only when the database is itself encrypted at rest or the threat
// model doesn't include a DB-snapshot attacker.
type NoopSecretCodec struct{}

// Encode returns plaintext unchanged.
func (NoopSecretCodec) Encode(p string) (string, error) { return p, nil }

// Decode returns encoded unchanged.
func (NoopSecretCodec) Decode(e string) (string, error) { return e, nil }

// secretEncodingPrefix marks ciphertexts produced by this package so
// the codec can distinguish them from legacy unencrypted values and
// future algorithm versions.
const secretEncodingPrefix = "wbenc:v1:"

// aesGCMSecretCodec encrypts secrets with AES-GCM. The encoded
// format is "wbenc:v1:" + base64(nonce || ciphertext) — the nonce
// is generated fresh for every Encode call so identical plaintexts
// produce different ciphertexts.
//
// Unprefixed inputs decode to themselves, which lets a deployment
// roll the codec on a table that previously stored plaintext without
// a one-shot rewrite job. Re-saving any subscriber persists the
// encrypted form going forward.
type aesGCMSecretCodec struct {
	aead cipher.AEAD
}

// NewAESGCMSecretCodec constructs a SecretCodec backed by AES-GCM
// using the supplied key. Key length must be 16, 24, or 32 bytes
// (AES-128 / AES-192 / AES-256).
//
// Treat the key as a critical secret: rotate it by re-encrypting
// subscribers through a transitional codec (decode-from-old,
// encode-to-new); the package does not bundle a key-ring abstraction
// to keep this primitive small.
func NewAESGCMSecretCodec(key []byte) (SecretCodec, error) {
	switch len(key) {
	case 16, 24, 32:
	default:
		return nil, fmt.Errorf("webhook: AES-GCM key must be 16/24/32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("webhook: AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("webhook: GCM mode: %w", err)
	}
	return &aesGCMSecretCodec{aead: aead}, nil
}

func (c *aesGCMSecretCodec) Encode(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("webhook: nonce: %w", err)
	}
	ct := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
	combined := append(nonce, ct...)
	return secretEncodingPrefix + base64.StdEncoding.EncodeToString(combined), nil
}

func (c *aesGCMSecretCodec) Decode(encoded string) (string, error) {
	if !strings.HasPrefix(encoded, secretEncodingPrefix) {
		// Legacy / pre-encryption value — pass through so deployments
		// can roll the codec on existing rows without a migration step.
		return encoded, nil
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, secretEncodingPrefix))
	if err != nil {
		return "", fmt.Errorf("webhook: decode base64: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(payload) < ns+1 {
		return "", errors.New("webhook: ciphertext truncated")
	}
	nonce, ct := payload[:ns], payload[ns:]
	plain, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("webhook: AES-GCM open (auth failure?): %w", err)
	}
	return string(plain), nil
}
