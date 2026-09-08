// Package xrawok pins the arm-b credit: the package walks the
// envelope field's keys at the chokepoint (core/mcp protocol.go
// pattern), so every downstream lax decode of the same field in the
// package is quiet.
package xrawok

import (
	"encoding/json"
)

// envelope is the wire frame: decoded strictly at the read loop.
type envelope struct {
	Verb   string          `json:"verb"`
	Params json.RawMessage `json:"params"`
}

// UnmarshalStrict is the strict decoder, by name (core/handler in the
// tree).
func UnmarshalStrict(data []byte, dst any) error {
	return json.Unmarshal(data, dst)
}

// CheckObjectKeys walks every object at every depth (core/handler in
// the tree).
func CheckObjectKeys(data []byte, fold func(string) string) error {
	var v any
	return json.Unmarshal(data, &v)
}

// readLoop is the strict site.
func readLoop(line []byte) (envelope, error) {
	var f envelope
	if err := UnmarshalStrict(line, &f); err != nil {
		return f, err
	}
	return f, nil
}

// dispatch is the chokepoint: one check at the dispatcher covers every
// method that decodes params.
func dispatch(f envelope) error {
	if len(f.Params) > 0 {
		if err := CheckObjectKeys(f.Params, nil); err != nil {
			return err
		}
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	return json.Unmarshal(f.Params, &p)
}

// downstream decodes the same field in another function: quiet, the
// chokepoint owns the walk.
func downstream(f envelope) error {
	var v map[string]any
	return json.Unmarshal(f.Params, &v)
}
