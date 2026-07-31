package context

import (
	"crypto/sha256"
	"encoding/hex"
)

// sha256Hex returns the hex-encoded SHA-256 of data, used by the TOFU
// gate to decide whether re-acking is needed.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
