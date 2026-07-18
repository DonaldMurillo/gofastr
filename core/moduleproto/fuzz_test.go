package moduleproto

import (
	"bytes"
	"encoding/json"
	"testing"
)

// FuzzDecodeFrame feeds arbitrary bytes into the codec's read loop via
// [Frame.UnmarshalJSON]. The contract is that NO input crashes or panics:
// malformed bytes are reported as a decode error ([ErrInvalidFrame]); valid
// bytes produce a well-formed Frame. Oversized/interleaved garbage must not
// corrupt state.
//
// Run: go test -fuzz=FuzzDecodeFrame -fuzztime=30s ./core/moduleproto/
func FuzzDecodeFrame(f *testing.F) {
	// Seed corpus: valid + adversarial shapes.
	seeds := []string{
		`{"jsonrpc":"2.0","id":1,"method":"x"}`,
		`{"jsonrpc":"2.0","id":0,"method":"x"}`,     // id:0 — rejected
		`{"jsonrpc":"1.0","id":1,"method":"x"}`,     // bad version
		`{"jsonrpc":"2.0","method":"n"}`,            // notification
		`{"jsonrpc":"2.0","id":1,"result":{"a":1}}`, // response
		`{"jsonrpc":"2.0","id":1,"error":{"code":-1,"message":"x"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"x","result":{}}`, // request+result
		`{}`,       // empty
		`not-json`, // garbage
		``,         // empty
		`{"jsonrpc":"2.0","id":18446744073709551615,"method":"x"}`, // max uint64
		`{"jsonrpc":"2.0","id":-1,"method":"x"}`,                   // negative id
		`{"jsonrpc":"2.0","id":"string-id","method":"x"}`,          // string id
		`{"jsonrpc":"2.0","id":1,"method":"x","params":null}`,      // null params
		`{"jsonrpc":"2.0","id":1.5,"method":"x"}`,                  // float id
		`{"jsonrpc":"2.0","id":[]}`,                                // array id
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Add(string(bytes.Repeat([]byte("a"), 1<<16))) // 64 KiB garbage

	f.Fuzz(func(t *testing.T, in string) {
		// Must not panic.
		var fr Frame
		_ = json.Unmarshal([]byte(in), &fr)
		// Re-marshal the decoded frame if decode succeeded; must round-trip
		// without error.
		if fr.JSONRPC == "2.0" { // best-effort: only if it survived decode
			if _, err := json.Marshal(fr); err != nil {
				t.Errorf("re-marshal failed for decoded frame: %v", err)
			}
		}
	})
}

// FuzzCodecReadLoop drives the codec ReadFrame with arbitrary newline-bounded
// byte streams (malformed, oversized, interleaved). The contract: no crash,
// no panic, and a returned error (or a valid *Frame) on every call.
//
// Run: go test -fuzz=FuzzCodecReadLoop -fuzztime=30s ./core/moduleproto/
func FuzzCodecReadLoop(f *testing.F) {
	// Seed with concatenated newline-delimited frames — valid + adversarial.
	seeds := []string{
		// Single valid frame.
		`{"jsonrpc":"2.0","id":1,"method":"x"}` + "\n",
		// Two valid frames back-to-back.
		`{"jsonrpc":"2.0","id":1,"method":"x"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"y"}` + "\n",
		// Garbage line.
		"not-json\n",
		// id:0 sentinel test.
		`{"jsonrpc":"2.0","id":0,"method":"x"}` + "\n",
		// Empty line.
		"\n",
		// Truncated (no newline) — scanner should not block forever here in
		// isolation (it would in a real reader, but we feed a closed buffer).
		`{"jsonrpc":"2.0","id":1`,
		// Extra fields.
		`{"jsonrpc":"2.0","id":1,"method":"x","unknown":"field","extra":[1,2,3]}` + "\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		buf := bytes.NewBufferString(in)
		c, err := NewCodec(buf, &bytes.Buffer{}, 0)
		if err != nil {
			return // construction failure is not a fuzz target
		}
		// Drain the codec. Each ReadFrame must either return a frame or an
		// error — never panic, never hang (bytes.Buffer is finite).
		for i := 0; i < 64; i++ {
			fr, err := c.ReadFrame()
			if err != nil {
				break // any error ends the stream — that's the contract
			}
			if fr == nil {
				t.Errorf("ReadFrame returned nil frame with nil error")
				break
			}
		}
	})
}
