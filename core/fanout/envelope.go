package fanout

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// envelopeJSON is the on-the-wire shape produced by [Wrap] and consumed by
// [Unwrap]. "n" is the originator's node id (see [NewNodeID]); "b" is the
// message body encoded as a JSON string so the whole envelope is itself
// valid JSON and decodable without a custom binary framing convention.
//
// "x" carries the body base64-encoded and appears only when the body is
// not valid UTF-8: a JSON string cannot hold arbitrary bytes, and
// json.Marshal silently rewrites each invalid byte as U+FFFD, so routing
// such a body through "b" corrupted it at the envelope boundary (the
// broadcast payload is serialized entities, hashes, ids — not text).
// Splitting the encoding this way keeps the "b" wire shape byte-identical
// for every existing UTF-8 payload, so old and new binaries interoperate
// through a rolling deploy.
type envelopeJSON struct {
	N string `json:"n"`
	B string `json:"b,omitempty"`
	X string `json:"x,omitempty"`
}

// NewNodeID returns 16 random bytes hex-encoded (32 chars). Random rather
// than counter-based avoids both global-contention and the assumption that
// every replica is built from the same source. Two replicas that happen to
// both start at counter zero would loop on each other's broadcasts.
//
// crypto/rand.Read on the default Reader does not fail on a supported
// platform (getrandom/SecRandomCopyBytes/RtlGenRandom), so the error is
// ignored, the modern idiom used elsewhere in the framework (e.g.
// core/middleware request ids).
func NewNodeID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// Wrap stamps body with the originator nodeID and returns the JSON envelope
// to publish to a [Fanout]. nodeID should come from [NewNodeID].
//
// Every byte of body survives [Unwrap] exactly, including invalid-UTF-8
// bytes (see envelopeJSON's "x" field).
func Wrap(nodeID string, body []byte) []byte {
	env := envelopeJSON{N: nodeID}
	if utf8.Valid(body) {
		env.B = string(body)
	} else {
		env.X = base64.StdEncoding.EncodeToString(body)
	}
	out, _ := json.Marshal(env)
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
	if env.X != "" {
		decoded, dErr := base64.StdEncoding.DecodeString(env.X)
		if dErr != nil {
			return "", nil, fmt.Errorf("fanout: invalid envelope body: %v", dErr)
		}
		return env.N, decoded, nil
	}
	return env.N, []byte(env.B), nil
}
