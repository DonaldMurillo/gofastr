package fanout

import (
	"bytes"
	"testing"
)

// Property: an envelope lifted off the bus is hostile until decoded — every
// subscriber on a shared topic (Redis pub/sub, LISTEN/NOTIFY) receives what
// any other publisher put there, so Unwrap must reject every malformed
// shape with an error and never panic, and must accept only a well-formed
// envelope carrying a non-empty node id (the loop guard depends on it).
//
// Surfaces: Unwrap on non-JSON bytes, structurally-valid JSON that is not
// an envelope (array/string/number/bool/null), an object missing or
// emptying the node id, and valid JSON with trailing garbage.
func TestUnwrapHostileEnvelopeShapes(t *testing.T) {
	hostile := []string{
		"",                          // empty
		"not json",                  // garbage bytes
		"null",                      // JSON null
		"[]",                        // array, not an object
		`["node-a","body"]`,         // array shaped like the envelope
		`"node-a"`,                  // bare string
		"123",                       // number
		"true",                      // bool
		`{}`,                        // object with no node id
		`{"b":"body"}`,              // body without a node id
		`{"n":"","b":"body"}`,       // empty node id
		`{"n":"a","b":"x"}trailing`, // valid envelope + trailing garbage
	}
	for _, raw := range hostile {
		if _, _, err := Unwrap([]byte(raw)); err == nil {
			t.Errorf("SECURITY: [fanout] Unwrap accepted hostile envelope %q. "+
				"Attack: a crafted bus message becomes a loop-guard bypass or a phantom originator id.", raw)
		}
	}

	// Duplicate keys: Go's decoder takes the last value. That must still
	// land on a non-empty node id — an all-empty dup pair must be rejected
	// rather than decoding to a usable-but-empty originator.
	if _, _, err := Unwrap([]byte(`{"n":"a","b":"x","n":""}`)); err == nil {
		t.Errorf("[fanout] duplicate-key envelope with an empty final n decoded; the empty-node-id rule must hold after last-wins")
	}
}

// TestWrapUnwrapBinaryRoundTrip pins payload fidelity under adversarial
// CONTENT: the body is an arbitrary broadcast payload — serialized
// entities, hashes, upload chunks — not text, so every byte must survive
// the JSON-string envelope exactly (Unwrap's documented contract is "the
// original body").
//
// Valid-UTF-8 control content (quotes, newlines, NUL, DEL) round-trips
// today; the invalid-UTF-8 byte is the shape that breaks: json.Marshal
// silently replaces it with U+FFFD, so Unwrap hands back a DIFFERENT
// payload than the publisher broadcast — silent corruption at the
// envelope boundary, not a decode error the caller could detect.
func TestWrapUnwrapBinaryRoundTrip(t *testing.T) {
	payloads := map[string][]byte{
		"quote and markup": []byte(`quote" angle<> amp& back\\ slash`),
		"control bytes":    []byte("nl\n tab\t nul\x00 del\x7f cr\r"),
		"invalid utf-8":    []byte("prefix\xff\xffsuffix"), // 0xff is never valid UTF-8
	}
	for name, body := range payloads {
		node, got, err := Unwrap(Wrap("node-A", body))
		switch {
		case err != nil:
			t.Errorf("[fanout] %s: Unwrap(Wrap(x)) errored on a well-formed envelope: %v", name, err)
		case node != "node-A":
			t.Errorf("[fanout] %s: node id mangled: %q", name, node)
		case !bytes.Equal(got, body):
			t.Errorf("SECURITY: [fanout] %s: envelope round-trip corrupted the body: in %x out %x. "+
				"Attack: a broadcast payload with non-UTF-8 bytes (hash, id, serialized blob) is silently "+
				"rewritten (json.Marshal's U+FFFD replacement) instead of delivered byte-for-byte; "+
				"Unwrap's documented contract is \"the original body\".", name, body, got)
		}
	}

	// The envelope must stay a single line: json.Marshal escapes the
	// body's newline, so no raw '\n' byte can corrupt a line-oriented
	// transport carrying the envelope.
	if env := Wrap("node-A", []byte("a\nb")); bytes.IndexByte(env, '\n') >= 0 {
		t.Errorf("SECURITY: [fanout] envelope contains a raw newline despite a newline in the body; " +
			"a line-oriented transport would split the envelope mid-message.")
	}
}
