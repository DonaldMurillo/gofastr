// Package xown strict-decodes a type it declares itself, the core/mcp
// shape: the transport package owns *Request and decodes it through
// handler.UnmarshalStrict. The strictness fact must still name the type
// for importers, though the type is this package's own.
package xown

import "encoding/json"

// Request is the envelope this package owns and decodes strictly.
type Request struct {
	Method string `json:"method"`
}

// UnmarshalStrict is the strict decoder, by name.
func UnmarshalStrict(data []byte, dst any) error {
	return json.Unmarshal(data, dst)
}

// Serve is the strict transport.
func Serve(raw []byte) error {
	var r Request
	return UnmarshalStrict(raw, &r)
}
