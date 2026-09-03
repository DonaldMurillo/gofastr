//go:build red

// RED TESTS — open findings, 2026-09-02 adversarial pass round 3 (tests-only;
// no fix applied). One header block per finding.

package acp_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// TestAcpRedAuthenticateRequiresReady
// Property: every session-scoped method the server dispatches is gated on
// requireReady — the state machine initialize installs — and authenticate is
// the credential-establishment entrypoint, so it must not run before the
// handshake either.
// Surfaces: core/acp/server.go handleRequest authenticate arm (:380-381) →
// handleAuthenticate (:462-481); the gate its sibling session methods enforce
// at :528 (session/new), :551 (session/load), :582 (session/prompt).
// Finding: handleAuthenticate has no requireReady() check, so a client that
// never sent initialize drives Options.Authenticate — the embedder's auth
// callback, which for protocol-driven methods is where credentials get
// established — with no capability negotiation or version agreement. session/
// new answers ErrInvalidRequest on the same connection for exactly this
// state; authenticate is dispatched anyway.
// Severity: production-facing — the auth hook can mint/modify credential
// state, and pre-handshake reachability means an unversioned peer can drive
// it (round-trip cost only, but it is the server's privileged callback).
// Fix direction: open handleAuthenticate with the same
// `if !st.requireReady() { return st.errResp(ErrInvalidRequest, "authenticate
// before initialize: ...") }` gate the session methods use.
func TestAcpRedAuthenticateRequiresReady(t *testing.T) {
	var called atomic.Bool
	opts := &acp.Options{
		AuthMethods: []acp.AuthMethod{{ID: "agent-login", Name: "Agent login"}},
		Authenticate: func(context.Context, string) error {
			called.Store(true)
			return nil
		},
	}
	d := startDialog(t, &fakeAgent{}, opts)

	// No initialize: the connection is not ready.
	d.request(7, "authenticate", map[string]any{"methodId": "agent-login"})
	f := d.untilResponseID(7)

	if called.Load() {
		t.Errorf("SECURITY: [acp-auth-ready] Options.Authenticate ran on a connection that never sent initialize — " +
			"the authenticate dispatch (server.go:380-381) skips the requireReady gate session/new (:528), " +
			"session/load (:551) and session/prompt (:582) all enforce, so the embedder's credential hook is " +
			"reachable without capability negotiation or version agreement")
	}
	if f["error"] == nil {
		t.Errorf("SECURITY: [acp-auth-ready] authenticate before initialize was accepted: %v — want an "+
			"ErrInvalidRequest refusal like every other session method on a not-ready connection", f)
	}
}

// TestAcpRedRequestPermissionFailClosed
// Property: RequestPermissionOutcome is a two-value protocol type — per
// protocol.go:228-230 the outcome MUST be OutcomeCancelled or OutcomeSelected
// with an OptionID — so a client answer that carries neither (null result,
// empty object) must surface as cancellation or an error, never as a nil-error
// zero-value Outcome that reads as "no objection" downstream.
// Surfaces: core/acp/server.go Client.RequestPermission result handling
// (:203-208); protocol.go:228-234 documents the invariant being violated.
// Finding: len(resp.Result) == 0 skips the decode entirely and a null/{} result
// decodes as a no-op, so both malformed answers below return the zero Outcome
// {Outcome:"", OptionID:""} with nil error. An embedder that branches on
// `err != nil` → deny and `Outcome == OutcomeCancelled` → deny has no arm for
// the zero value; whatever falls through runs the tool call the user never
// approved (the approval surface is exactly what RequestPermission guards).
// Severity: production-facing — the client is the untrusted peer on this wire
// (a hostile/broken ACP client controls its response frames), and the outcome
// is the agent-side authorization decision for a tool call.
// Fix direction: after the decode arm, fail closed — if out.Outcome is neither
// OutcomeSelected (with non-empty OptionID) nor OutcomeCancelled, return the
// zero outcome with an error (or coerce to OutcomeCancelled), mirroring the
// protocol.go doc contract.
func TestAcpRedRequestPermissionFailClosed(t *testing.T) {
	// Two malformed-answer shapes: result explicitly null (decode arm
	// skipped, len==0) and result {} (decode runs, unmarshals to zero).
	for _, shape := range []struct {
		name   string
		answer map[string]any
	}{
		{"null result", map[string]any{"jsonrpc": "2.0", "result": nil}},
		{"empty object result", map[string]any{"jsonrpc": "2.0", "result": map[string]any{}}},
	} {
		t.Run(shape.name, func(t *testing.T) {
			outcomeCh := make(chan acp.RequestPermissionOutcome, 1)
			errCh := make(chan error, 1)
			agent := &fakeAgent{newFn: func(ctx context.Context, cwd string) (acp.Session, error) {
				return &fakeSession{id: "s1", promptFn: func(ctx context.Context, prompt []acp.ContentBlock, out *acp.Client) (string, error) {
					outcome, err := out.RequestPermission(ctx,
						acp.ToolCallUpdate{ToolCallID: "c1", Title: new("approve plan")},
						[]acp.PermissionOption{
							{OptionID: "allow-once", Name: "Allow once", Kind: acp.PermissionAllowOnce},
							{OptionID: "reject-once", Name: "Reject", Kind: acp.PermissionRejectOnce},
						})
					outcomeCh <- outcome
					errCh <- err
					return acp.StopEndTurn, nil
				}}, nil
			}}
			d := startDialog(t, agent, nil)
			id := d.newSession("/tmp/p")
			d.request(5, "session/prompt", map[string]any{
				"sessionId": id,
				"prompt":    []any{map[string]any{"type": "text", "text": "go"}},
			})

			ask := d.frame()
			if ask["method"] != "session/request_permission" {
				t.Fatalf("setup: first frame = %v, want session/request_permission", ask)
			}
			shape.answer["id"] = int(ask["id"].(float64))
			d.send(shape.answer)

			select {
			case got := <-outcomeCh:
				err := <-errCh
				if err == nil && got.Outcome != acp.OutcomeCancelled {
					t.Errorf("SECURITY: [acp-perm-failclosed] client answered %s and RequestPermission returned "+
						"{%+v} with nil error — protocol.go:228-230 says the outcome MUST be cancelled or "+
						"selected+optionId; the zero Outcome is neither, so a malformed/unanswered approval flows "+
						"on as if the user raised no objection", shape.name, got)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("setup: permission outcome never delivered to the session")
			}
		})
	}
}
