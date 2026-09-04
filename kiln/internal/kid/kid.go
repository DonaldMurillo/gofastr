// Package kid mints kiln's unpredictable identifiers.
//
// Journal entry ids, tool-call ids, and ACP session ids double as
// capabilities (session/load replays a whole conversation on the bare
// id), so they carry 128 unpredictable bits from crypto/rand. A
// wall-clock or counter-derived shape is enumerable: every other
// credential this repo mints already holds the same bar.
package kid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// Hex returns n random bytes as a hex string (2n characters), the same
// 16-byte shape the repo's generateAPITokenID/generateUserID helpers use.
//
// A crypto/rand failure panics: minting an identifier is not optional on
// any path that reaches here, and a timestamp fallback would silently
// reintroduce the enumerability this package exists to remove.
func Hex(n int) string {
	if n <= 0 {
		panic(fmt.Sprintf("kid: Hex(%d): want a positive byte count", n))
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("kid: crypto/rand unavailable: %v", err))
	}
	return hex.EncodeToString(b)
}
