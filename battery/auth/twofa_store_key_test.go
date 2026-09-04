package auth

import (
	"bytes"
	"testing"
)

// testTwoFAStoreConfig is the sealing key every entity 2FA store test
// passes; the constructor refuses to run without one.
func testTwoFAStoreConfig() EntityTwoFAStoreConfig {
	return EntityTwoFAStoreConfig{EncryptionKey: bytes.Repeat([]byte{0x2a}, 32)} // not-a-secret: fixed test key
}

func TestTwoFAStoreRefusesEmptyKey(t *testing.T) {
	if _, err := NewEntityTwoFAStore(nil, "auth_twofa", EntityTwoFAStoreConfig{}); err == nil {
		t.Fatal("an entity 2FA store with no EncryptionKey must be refused: the TOTP seed is a credential")
	}
}
