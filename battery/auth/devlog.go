package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

func hashedIdentifier(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}
