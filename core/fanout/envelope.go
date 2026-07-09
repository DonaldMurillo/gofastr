package fanout

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// envelopeJSON is the on-the-wire shape produced by [Wrap] and consumed by
// [Unwrap]. "n" is the originator's node id (see [NewNodeID]); "b" is the raw
// message body encoded as a JSON string so the whole envelope is itself valid
// JSON and decodable without a custom binary framing convention.
type envelopeJSON struct {
	N string `json:"n"`
	B string `json:"b"`
}

// NewNodeID returns 16 random bytes hex-encoded (32 chars). Random rather
// than counter-based avoids both global-contention and the assumption that
// every replica is built from the same source — two replicas that happen to
// both start at counter zero would loop on each other's broadcasts.
//
// crypto/rand.Read on the default Reader does not fail on a supported
// platform (getrandom/SecRandomCopyBytes/RtlGenRandom), so the error is
// ignored — the modern idiom used elsewhere in the framework (e.g.
// core/middleware request ids).
func NewNodeID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// Wrap stamps body with the originator nodeID and returns the JSON envelope
// to publish to a [Fanout]. nodeID should come from [NewNodeID].
//
// json.Marshal of a struct of two string fields cannot fail, so the error is
// ignored; the returned envelope is always valid JSON decodable by [Unwrap].
func Wrap(nodeID string, body []byte) []byte {
	out, _ := json.Marshal(envelopeJSON{N: nodeID, B: string(body)})
	return out
}

// Unwrap decodes an envelope produced by [Wrap], returning the originator's
// nodeID and the original body. It errors if raw is not a valid envelope or
// carries an empty node id.
func Unwrap(raw []byte) (nodeID string, body []byte, err error) {
	var env envelopeJSON
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", nil, fmt.Errorf("fanout: invalid envelope: %w", err)
	}
	if env.N == "" {
		return "", nil, fmt.Errorf("fanout: envelope missing node id")
	}
	return env.N, []byte(env.B), nil
}
