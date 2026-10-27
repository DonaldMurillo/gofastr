// Package dec holds passthrough decoders another package calls.
package dec

import (
	"bytes"
	"encoding/json"
)

// Into decodes lossily into its caller's destination.
func Into(b []byte, v any) error { return json.Unmarshal(b, v) }

// Exact keeps numbers exact.
func Exact(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

// Marked is a passthrough on purpose; its callers are not flagged.
func Marked(b []byte, v any) error {
	//gofastr:allow(floatnum) callers decode only strings through it
	return json.Unmarshal(b, v)
}

// Entry is kiln's journal shape: a method decoding a stored payload.
type Entry struct{ Payload []byte }

func (e Entry) Decode(v any) error { return json.Unmarshal(e.Payload, v) }
