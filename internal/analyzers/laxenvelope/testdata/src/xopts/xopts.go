// want package:"strict\\(xopts.frame, xopts.query\\)"

// Package xopts pins ledger I6 of the 2026-10-27 bug bash: a strict
// decode that passes options (handler.UnmarshalStrict(raw, &q,
// handler.UseNumber()), battery/semantic/routes.go) is still the strict
// site, so a lax decode of the same type elsewhere fires.
package xopts

import "encoding/json"

// Option is a strict-decode option (handler.StrictOption in the tree).
type Option func()

// UseNumber is an option (handler.UseNumber in the tree).
func UseNumber() Option { return nil }

// UnmarshalStrict is the strict decoder with trailing options.
func UnmarshalStrict(data []byte, dst any, opts ...Option) error {
	return json.Unmarshal(data, dst)
}

// query is decoded strictly with an option.
type query struct {
	Text string `json:"text"`
}

func routeIngest(raw []byte) (query, error) {
	var q query
	err := UnmarshalStrict(raw, &q, UseNumber())
	return q, err
}

// The lax sibling of the strict-with-options site fires.
func sideIngest(raw []byte) (query, error) {
	var q query
	err := json.Unmarshal(raw, &q) // want `lax decode of query, which this package also decodes strictly elsewhere`
	return q, err
}

// frame is an envelope strict-decoded with an option: its RawMessage
// field is the arm-b carrier all the same.
type frame struct {
	Verb   string          `json:"verb"`
	Params json.RawMessage `json:"params"`
}

func readFrame(line []byte) (frame, error) {
	var f frame
	err := UnmarshalStrict(line, &f, UseNumber())
	return f, err
}

func handleFrame(f frame) error {
	var p map[string]any
	return json.Unmarshal(f.Params, &p) // want `lax decode of Params, a json.RawMessage field of frame`
}
