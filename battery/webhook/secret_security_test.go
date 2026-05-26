package webhook

import (
	"testing"
)

// TestSecretAESGCMSecretCodec_RoundTrip verifies that the AES-GCM codec
// correctly encrypts and decrypts secrets.
func TestSecretAESGCMSecretCodec_RoundTrip(t *testing.T) {
	key := make([]byte, 32) // 256-bit key
	for i := range key {
		key[i] = byte(i)
	}

	codec, err := NewAESGCMSecretCodec(key)
	if err != nil {
		t.Fatalf("NewAESGCMSecretCodec: %v", err)
	}

	plaintext := "super-secret-webhook-key"
	encoded, err := codec.Encode(plaintext)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if encoded == plaintext {
		t.Errorf("SECURITY: [secret] encoded value equals plaintext. Attack: secrets stored in cleartext.")
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded != plaintext {
		t.Errorf("decoded = %q, want %q", decoded, plaintext)
	}
}

// TestSecretAESGCMSecretCodec_DifferentCiphertexts verifies that encoding
// the same plaintext twice produces different ciphertexts (random nonce).
// Attack: pattern analysis via identical ciphertexts.
func TestSecretAESGCMSecretCodec_DifferentCiphertexts(t *testing.T) {
	key := make([]byte, 32)
	codec, _ := NewAESGCMSecretCodec(key)

	e1, _ := codec.Encode("same-secret")
	e2, _ := codec.Encode("same-secret")

	if e1 == e2 {
		t.Errorf("SECURITY: [secret] two encryptions of same plaintext produce identical ciphertext. Attack: nonce reuse enables pattern analysis.")
	}
}

// TestSecretAESGCMSecretCodec_WrongKeyRejected verifies that decryption
// with the wrong key fails. Attack: brute-forcing encryption keys.
func TestSecretAESGCMSecretCodec_WrongKeyRejected(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	key2[0] = 1 // different key

	codec1, _ := NewAESGCMSecretCodec(key1)
	codec2, _ := NewAESGCMSecretCodec(key2)

	encoded, _ := codec1.Encode("secret")

	_, err := codec2.Decode(encoded)
	if err == nil {
		t.Errorf("SECURITY: [secret] decryption with wrong key succeeded. Attack: key confusion.")
	}
}

// TestSecretAESGCMSecretCodec_InvalidKeyLength verifies that invalid key
// lengths are rejected. Attack: weak encryption via short keys.
func TestSecretAESGCMSecretCodec_InvalidKeyLength(t *testing.T) {
	_, err := NewAESGCMSecretCodec([]byte("short"))
	if err == nil {
		t.Errorf("SECURITY: [secret] short key accepted for AES-GCM. Attack: weak encryption key.")
	}
}

// TestSecretNoopCodec_NoEncryption verifies that NoopSecretCodec stores
// plaintext. Document: this should only be used in dev/test.
func TestSecretNoopCodec_NoEncryption(t *testing.T) {
	codec := NoopSecretCodec{}
	encoded, _ := codec.Encode("secret")
	if encoded != "secret" {
		t.Errorf("NoopSecretCodec should not encrypt")
	}

	// Document: NoopSecretCodec must NEVER be used in production
	t.Logf("NOTE: [secret] NoopSecretCodec stores plaintext — NEVER use in production")
}

// TestSecretAESGCMSecretCodec_TamperedCiphertextRejected verifies that
// tampered ciphertext is rejected. Attack: bit-flipping attack.
func TestSecretAESGCMSecretCodec_TamperedCiphertextRejected(t *testing.T) {
	key := make([]byte, 32)
	codec, _ := NewAESGCMSecretCodec(key)

	encoded, _ := codec.Encode("secret")

	// Flip a bit in the encoded string
	tampered := encoded[:len(encoded)-4] + "XXXX"
	_, err := codec.Decode(tampered)
	if err == nil {
		t.Errorf("SECURITY: [secret] tampered ciphertext accepted. Attack: bit-flipping on AES-GCM ciphertext.")
	}
}
