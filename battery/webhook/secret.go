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

// SecretCodec seals subscriber secrets and credentials at rest;
// implementations must be safe for concurrent use.
type SecretCodec interface {
	Encode(plaintext string) (string, error)
	Decode(encoded string) (string, error)
}

// NoopSecretCodec stores secrets as-is.
type NoopSecretCodec struct{}

// Encode returns plaintext unchanged.
func (NoopSecretCodec) Encode(p string) (string, error) { return p, nil }

// Decode returns encoded unchanged.
func (NoopSecretCodec) Decode(e string) (string, error) { return e, nil }

const secretEncodingPrefix = "wbenc:v1:"

type aesGCMSecretCodec struct {
	aead cipher.AEAD
}

// NewAESGCMSecretCodec returns an AES-GCM SecretCodec for a 16, 24 or 32 byte
// key; unprefixed values decode as themselves.
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
