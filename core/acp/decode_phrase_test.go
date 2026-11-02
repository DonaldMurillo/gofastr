package acp_test

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// goInternals is what encoding/json's own text carried into an ACP error
// message: the server's struct names and Go type spellings.
var goInternals = regexp.MustCompile(`Go struct|Go value|of type|wireRequest|sessionSetupParams|json: |cannot unmarshal`)

// A frame of the wrong shape answered with encoding/json's text, naming
// the agent's Go types ("json: cannot unmarshal number into Go struct
// field sessionSetupParams.cwd of type string"), the leak core/mcp's
// params decodes closed in 1a4d9e2a. The envelope and the session params
// now answer handler.JSONErrorDetail's phrase: the JSON path and type.
func TestDecodeErrorsNameNoGoTypes(t *testing.T) {
	d := startDialog(t, &fakeAgent{newFn: func(context.Context, string) (acp.Session, error) {
		return &fakeSession{id: "sess_phrase"}, nil
	}}, nil)
	d.initialize()

	message := func(f map[string]any) string {
		t.Helper()
		e, _ := f["error"].(map[string]any)
		if e == nil {
			t.Fatalf("no error in %v", f)
		}
		m, _ := e["message"].(string)
		return m
	}

	d.send(json.RawMessage(`{"jsonrpc":"2.0","id":7,"method":5}`))
	if m := message(d.untilResponseID(7)); goInternals.MatchString(m) {
		t.Errorf("envelope type error: message %q carries Go internals", m)
	}

	d.send(json.RawMessage(`{"jsonrpc":"2.0","id":8,"method":"session/new","params":{"cwd":5,"mcpServers":[]}}`))
	m := message(d.untilResponseID(8))
	if goInternals.MatchString(m) {
		t.Errorf("session/new params type error: message %q carries Go internals", m)
	}
	if !regexp.MustCompile(`session/new: cwd`).MatchString(m) {
		t.Errorf("session/new params type error: message %q does not name the method and the JSON path", m)
	}
}
