package acp_test

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// Property: a malformed session/request_permission answer — a null or
// empty result, anything that is neither OutcomeSelected with an
// OptionID nor OutcomeCancelled — is coerced to OutcomeCancelled, never
// returned as the zero Outcome with nil error. The client is the
// untrusted peer on this wire and the outcome is the agent-side
// authorization for the tool call being approved, so a malformed answer
// must read as "no approval" (protocol.go: RequestPermissionOutcome).
func TestRequestPermissionFailClosed(t *testing.T) {
	// Two malformed-answer shapes: result explicitly null (decode arm
	// skipped, len==0) and result {} (decode runs, unmarshals to zero).
	for _, shape := range []struct {
		name   string
		answer map[string]any
	}{
		{"null result", map[string]any{"jsonrpc": "2.0", "result": nil}},
		{"empty object result", map[string]any{"jsonrpc": "2.0", "result": map[string]any{}}},
		{"selected with no optionId", map[string]any{"jsonrpc": "2.0", "result": map[string]any{
			"outcome": "selected",
		}}},
		{"unknown outcome", map[string]any{"jsonrpc": "2.0", "result": map[string]any{
			"outcome": "defer",
		}}},
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
						"{%+v} with nil error — the outcome MUST be cancelled or selected+optionId; the zero "+
						"Outcome is neither, so a malformed/unanswered approval flows on as if the user raised "+
						"no objection", shape.name, got)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("setup: permission outcome never delivered to the session")
			}
		})
	}
}
