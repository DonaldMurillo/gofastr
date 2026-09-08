// Package xraw pins the RawMessage arm: an envelope strict-decoded in
// this package whose Params bytes are then decoded lax — the strict
// walk stopped at the envelope (core/acp shape).
package xraw

import (
	"encoding/json"
)

// envelope is the wire frame: decoded strictly at the read loop.
type envelope struct {
	Verb   string          `json:"verb"`
	Params json.RawMessage `json:"params"`
	ID     json.RawMessage `json:"id"`
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

// handle is the fire: the strict walk stopped at the envelope, and a
// struct destination resolves payload keys last-wins.
func handle(f envelope) error {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	return json.Unmarshal(f.Params, &p) // want `lax decode of Params, a json.RawMessage field of envelope`
}

// handleAny is the any spelling of the same fire: an object payload
// lands in a map that keeps the last duplicate.
func handleAny(f envelope) error {
	var v any
	return json.Unmarshal(f.Params, &v) // want `lax decode of Params, a json.RawMessage field of envelope`
}

// scalarID is quiet: an int64 destination resolves no keys, so an
// ambiguous object id cannot survive it (core/acp frame.ID).
func scalarID(f envelope) (int64, error) {
	var id int64
	err := json.Unmarshal(f.ID, &id)
	return id, err
}

// gauge is a RawMessage field of a type that is never strict-decoded
// here: no envelope contract, quiet.
type gauge struct {
	Params json.RawMessage `json:"params"`
}

func gaugeRead(g gauge) error {
	var v map[string]any
	return json.Unmarshal(g.Params, &v)
}
