package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Pins: a request body decodes only when it is exactly ONE valid JSON value
// (contract resolved 2026-09-07: yes, the strict doctrine extends to
// exactly-one-value bodies — after a successful Decode a second Decode must
// return io.EOF, anything else is a 400).
// Property: a request body decodes only when it is exactly ONE valid JSON
// value — trailing garbage or a second concatenated value is a 400
// (bind.go doc: "If the body is present but not valid JSON, a 400 error is
// returned").
// Surfaces: core/handler/bind.go bindBody — single Decode, no EOF re-check;
// core/handler/decode_strict.go UnmarshalStrict — same shape.
// Finding: '{"a":1}{"b":2}', '{"a":1} trailing', and '{"a":1}\n{"b":2}'
// each decode their first value and return nil — the remainder of the body
// is never examined, so two parsers (or a parser and a re-reading consumer)
// disagree on what the body said. The ambiguity decode_strict exists to
// refuse is accepted silently on both paths.
// Fix direction: after Decode succeeds, a second Decode must return io.EOF;
// anything else is Errorf(400, ...) on both bindBody and UnmarshalStrict.

func TestBindRedTrailingJSONRefused(t *testing.T) {
	bodies := []struct {
		name string
		raw  string
	}{
		{"concatenated objects", `{"a":1}{"b":2}`},
		{"trailing garbage", `{"a":1} trailing`},
		{"second value after newline", "{\"a\":1}\n{\"b\":2}"},
	}

	for _, tc := range bodies {
		t.Run("Bind struct / "+tc.name, func(t *testing.T) {
			var dst struct {
				A int `json:"a"`
			}
			req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(tc.raw))
			req.Header.Set("Content-Type", "application/json")
			err := Bind(req, &dst)
			if err == nil {
				t.Errorf("SECURITY: [bind-trailing-json] Bind accepted body %q with content after "+
					"the first JSON value (decoded A=%d, err=nil). Attack: a body that reads two "+
					"ways is the ambiguity decode_strict refuses; Bind must demand exactly one "+
					"value, not silently pick the first.", tc.raw, dst.A)
			}
		})
		t.Run("UnmarshalStrict map / "+tc.name, func(t *testing.T) {
			var m map[string]any
			err := UnmarshalStrict([]byte(tc.raw), &m)
			if err == nil {
				t.Errorf("SECURITY: [bind-trailing-json] UnmarshalStrict accepted body %q with "+
					"content after the first JSON value (m=%v, err=nil). Attack: strict decode "+
					"exists to refuse bodies that read two ways; a swallowed second value is the "+
					"same ambiguity the duplicate-key rules exist to kill.", tc.raw, m)
			}
		})
	}

	// Positive control: a well-formed single object decodes on both paths.
	var dst struct {
		A int `json:"a"`
	}
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	if err := Bind(req, &dst); err != nil || dst.A != 1 {
		t.Errorf("SECURITY: [bind-trailing-json] positive control: well-formed single object "+
			"must decode via Bind (A=%d, err=%v)", dst.A, err)
	}
	var m map[string]any
	if err := UnmarshalStrict([]byte(`{"a":1}`), &m); err != nil || m["a"] != float64(1) {
		t.Errorf("SECURITY: [bind-trailing-json] positive control: well-formed single object "+
			"must decode via UnmarshalStrict (m=%v, err=%v)", m, err)
	}
}

// TestUnmarshalStrictRedTrailingRefused is the surface extension of
// bind-trailing-json onto UnmarshalStrict ALONE: the bindBody arm stays
// pinned by TestBindRedTrailingJSONRefused, so a fix landing on one
// decoder flips only its own test.
//
// Pins (surface extension of bind-trailing-json onto UnmarshalStrict alone):
// bytes decode only when they are exactly ONE valid JSON value.
// Property: bytes decode only when they are exactly ONE valid JSON value —
// trailing garbage or a second concatenated value is a 400.
// Surface: core/handler/decode_strict.go:58 — UnmarshalStrict Decodes once
// and never re-checks for EOF, so the remainder of the buffer is never
// examined.
// Finding: '{"a":1}{"b":2}', '{"a":1} trailing', and '{"a":1}\n{"b":2}'
// each decode their first value and return nil; the ambiguity
// decode_strict exists to refuse is accepted silently. Fix direction:
// after Decode succeeds, a second Decode must return io.EOF; anything
// else is Errorf(400, ...).
func TestUnmarshalStrictRedTrailingRefused(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"concatenated objects", `{"a":1}{"b":2}`},
		{"trailing garbage", `{"a":1} trailing`},
		{"second value after newline", "{\"a\":1}\n{\"b\":2}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := UnmarshalStrict([]byte(tc.raw), &m); err == nil {
				t.Errorf("SECURITY: [unmarshalstrict-trailing-json] UnmarshalStrict accepted %q with content after the first JSON value (m=%v, err=nil). Attack: strict decode exists to refuse bytes that read two ways; a swallowed second value is the same ambiguity the duplicate-key rules exist to kill (decode_strict.go:58 decodes once and never re-checks EOF).", tc.raw, m)
			}
		})
	}

	// Positive control: a well-formed single object still decodes.
	var m map[string]any
	if err := UnmarshalStrict([]byte(`{"a":1}`), &m); err != nil || m["a"] != float64(1) {
		t.Errorf("SECURITY: [unmarshalstrict-trailing-json] positive control: well-formed single object must decode via UnmarshalStrict (m=%v, err=%v)", m, err)
	}
}
