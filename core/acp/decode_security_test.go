package acp_test

import (
	"encoding/json"
	"testing"
)

// Property: the JSON-RPC envelope decoded by Serve refuses duplicate
// and case-folded top-level keys, so no first-occurrence parser —
// proxy, logger, audit trail — can disagree with the dispatcher's
// last-key-wins decode. The same contract handler.UnmarshalStrict pins
// for the a2a and MCP HTTP envelopes. The connection is initialized
// first so the smuggled session/new would genuinely dispatch (mint a
// session) if the duplicate key were silently resolved.
func TestServeRejectsDuplicateEnvelopeKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{"duplicate method key", `{"jsonrpc":"2.0","id":2,"method":"initialize","method":"session/new","params":{"cwd":"/tmp/p"}}`},
		{"case-folded method key", `{"jsonrpc":"2.0","id":2,"Method":"initialize","method":"session/new","params":{"cwd":"/tmp/p"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := startDialog(t, &fakeAgent{}, nil)
			d.initialize()
			d.send(json.RawMessage(tc.line))
			// A parse error carries no id (the frame never parsed), so
			// read the next frame rather than waiting for id 2.
			f := d.frame()
			if f["result"] != nil {
				t.Fatalf("SECURITY: [strict-json] ambiguous envelope dispatched (result=%v): stdlib json's silent "+
					"last-key-wins minted a session behind the duplicate method key while a first-occurrence validator saw initialize", f["result"])
			}
			if f["error"] == nil {
				t.Fatalf("ambiguous envelope was neither refused nor answered: %v", f)
			}
		})
	}
}
