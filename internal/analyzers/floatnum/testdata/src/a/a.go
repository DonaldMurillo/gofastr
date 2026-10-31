// Package a holds the floatnum fixture reduced from the 2026-10-26 bug
// bash sites: typed hooks round-tripping a field through `var decoded
// any`, MCP tool arguments decoded into map[string]any, and semantic
// metadata decoded into a struct with an `any` field. Each rounded an
// integer past 2^53.
package a

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/DonaldMurillo/gofastr/core/handler"

	"dec"
)

// Pre-fix typed hooks: the round-trip value lands in an interface.
func roundTrip(b []byte) (any, error) {
	var decoded any
	err := json.Unmarshal(b, &decoded) // want `JSON decoded into any without UseNumber`
	return decoded, err
}

// Pre-fix MCP arguments.
func args(r io.Reader) (map[string]any, error) {
	var m map[string]any
	err := json.NewDecoder(r).Decode(&m) // want `JSON decoded into map\[string\]any without UseNumber`
	return m, err
}

type meta struct {
	ID    string         `json:"id"`
	Extra map[string]any `json:"extra"`
}

// Pre-fix semantic metadata: the interface sits inside a struct.
func readMeta(b []byte) (meta, error) {
	var m meta
	dec := json.NewDecoder(bytes.NewReader(b))
	err := dec.Decode(&m) // want `JSON decoded into meta without UseNumber`
	return m, err
}

// The fix: the same decoder calls UseNumber.
func argsFixed(r io.Reader) (map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(r)
	dec.UseNumber()
	err := dec.Decode(&m)
	return m, err
}

// No interface anywhere in the target: numbers keep their Go types.
type typed struct {
	N int64   `json:"n"`
	F float64 `json:"f"`
	L []int   `json:"l"`
}

func readTyped(b []byte) (typed, error) {
	var t typed
	err := json.Unmarshal(b, &t)
	return t, err
}

// A type that decodes itself decides its own numbers.
type custom struct{ v any }

func (c *custom) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return dec.Decode(&c.v)
}

func readCustom(b []byte) (custom, error) {
	var c custom
	err := json.Unmarshal(b, &c)
	return c, err
}

// json.RawMessage keeps the bytes; no number is read.
func readRaw(b []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	err := json.Unmarshal(b, &m)
	return m, err
}

// A destination passed through as an interface is the caller's type.
func into(b []byte, dst any) error { // want into:"lossyParams\\[1\\]"
	return json.Unmarshal(b, dst)
}

// A marker names a decode whose numbers are never read.
func kind(b []byte) string {
	var m map[string]any
	//gofastr:allow(floatnum) only the "type" string is read
	_ = json.Unmarshal(b, &m)
	s, _ := m["type"].(string)
	return s
}

// The strict decoders: UseNumber() keeps numbers exact.
func strict(b []byte, r io.Reader) {
	var m map[string]any
	_ = handler.UnmarshalStrict(b, &m) // want `JSON decoded into map\[string\]any without UseNumber`
	_ = handler.UnmarshalStrict(b, &m, handler.UseNumber())
	_ = handler.DecodeStrict(r, &m) // want `JSON decoded into map\[string\]any without UseNumber`
}

var pkgStrictOpts = []handler.StrictOption{handler.UseNumber()}

var pkgPlainOpts = []handler.StrictOption{}

// I3: handler.UseNumber() passed through a variable, a slice spread, an
// append, or a package-level slice keeps numbers exact too.
func strictViaVar(b []byte) {
	var m map[string]any
	opt := handler.UseNumber()
	_ = handler.UnmarshalStrict(b, &m, opt)
	opts := []handler.StrictOption{handler.UseNumber()}
	_ = handler.UnmarshalStrict(b, &m, opts...)
	var more []handler.StrictOption
	more = append(more, handler.UseNumber())
	_ = handler.UnmarshalStrict(b, &m, more...)
	_ = handler.UnmarshalStrict(b, &m, pkgStrictOpts...)
}

// An option variable that never holds UseNumber is still the lossy decode.
func strictViaOtherVar(b []byte) {
	var m map[string]any
	var opt handler.StrictOption
	_ = handler.UnmarshalStrict(b, &m, opt)             // want `JSON decoded into map\[string\]any without UseNumber`
	_ = handler.UnmarshalStrict(b, &m, pkgPlainOpts...) // want `JSON decoded into map\[string\]any without UseNumber`
}

// An exported interface behind its own UnmarshalJSON is that type's call.
type selfDecoding struct{ V any }

func (s *selfDecoding) UnmarshalJSON(b []byte) error { return nil }

func readSelf(b []byte) (selfDecoding, error) {
	var s selfDecoding
	err := json.Unmarshal(b, &s)
	return s, err
}

// encoding/json never fills an unexported field.
type hidden struct {
	ID string `json:"id"`
	v  any
}

func readHidden(b []byte) (hidden, error) {
	var h hidden
	err := json.Unmarshal(b, &h)
	return h, err
}

// A non-empty interface field cannot hold a decoded number.
type withMethod struct {
	S interface{ M() } `json:"s"`
}

func readMethod(b []byte) (withMethod, error) {
	var w withMethod
	err := json.Unmarshal(b, &w)
	return w, err
}

// An embedded unexported struct's exported fields are promoted.
type inner struct {
	Extra any `json:"extra"`
}

type outer struct {
	inner
}

// A passthrough in this package (the A2A decodeParams shape): the report
// lands on the call that names the destination.
func decodeParams(b []byte, v any) error { return json.Unmarshal(b, v) } // want decodeParams:"lossyParams\\[1\\]"

// A wrapper that hands its own interface parameter on inherits it.
func wrap(b []byte, v any) error { return decodeParams(b, v) } // want wrap:"lossyParams\\[1\\]"

func passthrough(b []byte) {
	var m map[string]any
	_ = decodeParams(b, &m) // want `JSON decoded into map\[string\]any through decodeParams`
	_ = wrap(b, &m)         // want `JSON decoded into map\[string\]any through wrap`
	var t typed
	_ = decodeParams(b, &t)
	_ = dec.Into(b, &m) // want `JSON decoded into map\[string\]any through dec.Into`
	_ = dec.Exact(b, &m)
	_ = dec.Marked(b, &m)
	var e dec.Entry
	_ = e.Decode(&m) // want `JSON decoded into map\[string\]any through e.Decode`
}

func readOuter(b []byte) (outer, error) {
	var o outer
	err := json.Unmarshal(b, &o) // want `JSON decoded into outer without UseNumber`
	return o, err
}
