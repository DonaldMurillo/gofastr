package fanout

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

type envelopeJSON struct {
	N string `json:"n"`
	B string `json:"b,omitempty"`
	X string `json:"x,omitempty"`
}

// NewNodeID returns a random node id, 16 bytes hex-encoded.
func NewNodeID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// Wrap returns the JSON envelope that stamps body with its originator.
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

// Unwrap returns the originator and body of a [Wrap] envelope, and errors
// on a malformed envelope or an empty node id.
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

// UnwrapForeign is [Unwrap] with ok false when raw is malformed or self sent it.
func UnwrapForeign(self string, raw []byte) (origin string, body []byte, ok bool) {
	origin, body, err := Unwrap(raw)
	if err != nil || origin == self {
		return "", nil, false
	}
	return origin, body, true
}
