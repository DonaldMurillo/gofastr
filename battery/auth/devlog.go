package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

// hashedIdentifier returns the first 12 hex chars of SHA-256(s). Used to
// emit dev-mode log lines that are greppable for the operator who knows
// the input (recompute the hash, compare prefix) but useless to anyone
// reading the logs without context — full emails and live tokens stay
// out of the log stream even in DevMode. 48 bits of prefix is enough to
// disambiguate while staying out of the rainbow-table sweet spot.
func hashedIdentifier(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}
