package a2a

import (
	"crypto/rand"
	"fmt"
)

// newUUID returns a random RFC 4122 version-4 UUID string, the id shape
// task, context, message, and push-config creation needs. There is no
// error return: crypto/rand.Read cannot fail on a supported platform
// (the idiom used across the repo, e.g. core/fanout.NewNodeID and
// framework/outbox.newID), and an ungeneratable id has no fallback.
// google/uuid exists only as an indirect dependency; importing it from
// here would promote it in go.mod, so the 6 lines live here instead.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("a2a: crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
