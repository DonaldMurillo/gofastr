package acp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// Pins: every params object of an agent-facing JSON-RPC frame refuses
// duplicate and case-folded keys at any depth, not just the envelope
// (2026-09-06/07 adversarial round 5, phase 2).
// Property: every params object of an agent-facing JSON-RPC frame refuses
// duplicate and case-folded keys at any depth, not just the envelope. The
// envelope decode in Serve is strict (pinned by TestServeRejectsDuplicate-
// EnvelopeKeys) and the permission-outcome decode is strict, but those guards
// stop at the frame boundary: frame.Params is decoded by plain json.Unmarshal
// in every handler, so stdlib's silent last-key-wins operates on the very
// object that carries cwd, session ids, and prompt content. The pinned twins
// are a2a decodeParams (core/a2a/server.go:473-489) and the core/mcp
// tools/call params chokepoint (core/mcp/protocol.go:74), which already apply
// the no-ambiguity rule to params bodies.
// Surfaces: core/acp/server.go::decodeSessionParams :612-621 (session/new,
// session/load), ::handleAuthenticate :566-571, ::startPrompt :700-707,
// ::handleInitialize :458-468, ::handleNotification session/cancel :429-443.
// Finding: session/new with params {"cwd":"/tmp/safe","CWD":"/etc"} passes the
// strict envelope (the ambiguity is one level down), then decodeSessionParams
// folds CWD onto the cwd field with last-wins, and NewSession runs with
// cwd="/etc" while any first-occurrence reader of the frame (proxy, logger,
// audit trail) saw "/tmp/safe". The same fold reaches prompt content blocks
// and authenticate methodId.
// Fix direction: decode every agent-facing frame.Params through
// handler.UnmarshalStrict / handler.CheckObjectKeys (the core/mcp chokepoint
// grammar) instead of json.Unmarshal, at the five sites above.

func TestACPParamsRedRefuseFoldedKeys(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params string
	}{
		{"case-folded cwd keys", `{"cwd":"/tmp/safe","CWD":"/etc","mcpServers":[]}`},
		{"duplicate cwd keys", `{"cwd":"/tmp/safe","cwd":"/etc","mcpServers":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invoked := make(chan string, 4)
			d := startDialog(t, &fakeAgent{newFn: func(_ context.Context, cwd string) (acp.Session, error) {
				select {
				case invoked <- cwd:
				default:
				}
				return &fakeSession{id: "sess_red_smuggled"}, nil
			}}, nil)
			d.initialize()
			d.send(json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":` + tc.params + `}`))
			f := d.untilResponseID(2)
			if f["error"] == nil {
				t.Errorf("SECURITY: [acp-params-lenient] ambiguous session/new params dispatched behind the strict envelope "+
					"(response=%v): the params object carries two keys folding onto cwd and the frame was not refused", f)
			}
			select {
			case cwd := <-invoked:
				t.Errorf("SECURITY: [acp-params-lenient] agent NewSession invoked with the folded cwd %q: the params decode must refuse the frame before the embedder runs", cwd)
			default:
			}
		})
	}

	// Depth leg: the fold sits inside a prompt content block, two levels below
	// the envelope the strict decoder vouches for.
	t.Run("case-folded prompt block keys", func(t *testing.T) {
		prompted := make(chan string, 1)
		d := startDialog(t, &fakeAgent{newFn: func(_ context.Context, _ string) (acp.Session, error) {
			return &fakeSession{id: "sess_red_prompt", promptFn: func(_ context.Context, _ []acp.ContentBlock, _ *acp.Client) (string, error) {
				select {
				case prompted <- "ran":
				default:
				}
				return acp.StopEndTurn, nil
			}}, nil
		}}, nil)
		sid := d.newSession("/tmp/safe")
		d.send(json.RawMessage(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"` + sid + `","prompt":[{"Type":"text","type":"text","text":"hi"}]}}`))
		f := d.untilResponseID(3)
		if f["error"] == nil {
			t.Errorf("SECURITY: [acp-params-lenient] ambiguous session/prompt params dispatched (response=%v): "+
				"a content block carries two keys folding onto type and the turn was started anyway", f)
		}
		select {
		case <-prompted:
			t.Errorf("SECURITY: [acp-params-lenient] session Prompt ran on a block decoded from folded keys: the params decode must refuse the frame first")
		default:
		}
	})

	// GREEN-guard: unambiguous params still mint a session, so the strict
	// decode cannot simply refuse everything.
	t.Run("clean session/new still mints", func(t *testing.T) {
		invoked := make(chan string, 1)
		d := startDialog(t, &fakeAgent{newFn: func(_ context.Context, cwd string) (acp.Session, error) {
			select {
			case invoked <- cwd:
			default:
			}
			return &fakeSession{id: "sess_red_clean"}, nil
		}}, nil)
		d.initialize()
		d.send(json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/tmp/safe","mcpServers":[]}}`))
		f := d.untilResponseID(2)
		res, _ := f["result"].(map[string]any)
		if f["error"] != nil || res == nil || res["sessionId"] != "sess_red_clean" {
			t.Errorf("SECURITY: [acp-params-lenient] clean params refused (response=%v): strictness must reject ambiguity, not unambiguous frames", f)
		}
		select {
		case cwd := <-invoked:
			if cwd != "/tmp/safe" {
				t.Errorf("SECURITY: [acp-params-lenient] clean params decoded to the wrong cwd %q", cwd)
			}
		default:
			t.Errorf("SECURITY: [acp-params-lenient] clean params never reached NewSession")
		}
	})
}
