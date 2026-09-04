package acp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// Property: a panic in embedder-supplied code — Agent.NewSession,
// SessionLoader.LoadSession, Session.Prompt — is recovered into a
// well-formed internal-error response for that one request. Serve is a
// bare read loop (and the prompt turn runs on its own goroutine) with
// no per-request recover net, so an escaped panic would kill the
// process and every other in-flight connection with it. runNewSession,
// runLoadSession and runPrompt hold the guard runAuthenticate
// established for the auth callback.
func TestEmbedderPanicBecomesInternalError(t *testing.T) {
	const secret = "super-secret-agent-detail"
	t.Run("session/new", func(t *testing.T) {
		agent := &fakeAgent{newFn: func(context.Context, string) (acp.Session, error) {
			panic(secret)
		}}
		d := startDialog(t, agent, nil)
		d.initialize()
		d.request(2, "session/new", map[string]any{"cwd": "/tmp/p"})
		f := d.untilResponseID(2)
		if f["error"] == nil {
			t.Fatalf("SECURITY: [panic-isolation] panicking NewSession was not refused: %v", f)
		}
		if msg, _ := f["error"].(map[string]any)["message"].(string); containsSecret(msg, secret) {
			t.Fatalf("SECURITY: [panic-isolation] panic value leaked to the client: %q", msg)
		}
		// The connection survives: a later request is still answered.
		d.request(3, "authenticate", map[string]any{"methodId": "none"})
		if f := d.untilResponseID(3); f["error"] == nil {
			t.Fatalf("connection unusable after recovered panic: %v", f)
		}
	})
	t.Run("session/load", func(t *testing.T) {
		agent := &fakeLoadingAgent{loadFn: func(context.Context, string, string, *acp.Client) (acp.Session, error) {
			panic(secret)
		}}
		d := startDialog(t, agent, nil)
		d.initialize()
		d.request(2, "session/load", map[string]any{"sessionId": "s1", "cwd": "/tmp/p"})
		f := d.untilResponseID(2)
		if f["error"] == nil {
			t.Fatalf("SECURITY: [panic-isolation] panicking LoadSession was not refused: %v", f)
		}
		if msg, _ := f["error"].(map[string]any)["message"].(string); containsSecret(msg, secret) {
			t.Fatalf("SECURITY: [panic-isolation] panic value leaked to the client: %q", msg)
		}
	})
	t.Run("session/prompt", func(t *testing.T) {
		calls := 0
		agent := &fakeAgent{newFn: func(context.Context, string) (acp.Session, error) {
			return &fakeSession{id: "s1", promptFn: func(context.Context, []acp.ContentBlock, *acp.Client) (string, error) {
				calls++
				if calls == 1 {
					panic(secret)
				}
				return acp.StopEndTurn, nil
			}}, nil
		}}
		d := startDialog(t, agent, nil)
		id := d.newSession("/tmp/p")
		prompt := map[string]any{
			"sessionId": id,
			"prompt":    []any{map[string]any{"type": "text", "text": "go"}},
		}
		d.request(5, "session/prompt", prompt)
		f := d.untilResponseID(5)
		if f["error"] == nil {
			t.Fatalf("SECURITY: [panic-isolation] panicking Prompt was not refused: %v", f)
		}
		if msg, _ := f["error"].(map[string]any)["message"].(string); containsSecret(msg, secret) {
			t.Fatalf("SECURITY: [panic-isolation] panic value leaked to the client: %q", msg)
		}
		// The session must not stay wedged busy: the next turn runs.
		d.request(6, "session/prompt", prompt)
		f = d.untilResponseID(6)
		if f["error"] != nil {
			t.Fatalf("session unusable after a recovered prompt panic (busy flag leaked): %v", f["error"])
		}
		if calls != 2 {
			t.Fatalf("second prompt turn never ran the implementation (calls=%d)", calls)
		}
	})
}

// containsSecret reports whether the panic detail survived into a wire
// error message.
func containsSecret(msg, secret string) bool {
	return strings.Contains(msg, secret)
}
