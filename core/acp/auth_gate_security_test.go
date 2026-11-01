package acp_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

func errCode(t *testing.T, f map[string]any) float64 {
	t.Helper()
	e, ok := f["error"].(map[string]any)
	if !ok {
		return 0
	}
	code, _ := e["code"].(float64)
	return code
}

// gatedAgent counts every embedder entry point a session method reaches.
func gatedAgent(minted, loaded *atomic.Int64) *fakeLoadingAgent {
	a := &fakeLoadingAgent{}
	a.newFn = func(ctx context.Context, cwd string) (acp.Session, error) {
		minted.Add(1)
		return &fakeSession{id: "sess_1"}, nil
	}
	a.loadFn = func(ctx context.Context, id, cwd string, out *acp.Client) (acp.Session, error) {
		loaded.Add(1)
		return &fakeSession{id: id}, nil
	}
	return a
}

// TestSessionsRequireAuthenticate: an agent that advertises auth
// methods set its gate in Authenticate, yet session/new, session/load
// and session/prompt checked only that initialize ran. A client that
// skipped authenticate, or whose attempt failed, got sessions anyway.
// Each now answers auth-required (-32000) until authenticate succeeds.
func TestSessionsRequireAuthenticate(t *testing.T) {
	for _, attempt := range []string{"skipped", "failed"} {
		t.Run(attempt, func(t *testing.T) {
			var minted, loaded atomic.Int64
			opts := &acp.Options{
				AuthMethods:  []acp.AuthMethod{{ID: "agent-login", Name: "Agent login"}},
				Authenticate: func(context.Context, string) error { return errors.New("bad credentials") },
			}
			d := startDialog(t, gatedAgent(&minted, &loaded), opts)
			d.initialize()
			if attempt == "failed" {
				d.request(5, "authenticate", map[string]any{"methodId": "agent-login"})
				if got := errCode(t, d.untilResponseID(5)); got != acp.ErrAuthRequired {
					t.Fatalf("failed authenticate answered code %v, want %d", got, acp.ErrAuthRequired)
				}
			}
			d.request(6, "session/new", map[string]any{"cwd": "/tmp", "mcpServers": []any{}})
			if got := errCode(t, d.untilResponseID(6)); got != acp.ErrAuthRequired {
				t.Errorf("SECURITY: [acp-auth-gate] session/new with authenticate %s answered code %v, want %d", attempt, got, acp.ErrAuthRequired)
			}
			d.request(7, "session/load", map[string]any{"sessionId": "sess_1", "cwd": "/tmp", "mcpServers": []any{}})
			if got := errCode(t, d.untilResponseID(7)); got != acp.ErrAuthRequired {
				t.Errorf("SECURITY: [acp-auth-gate] session/load with authenticate %s answered code %v, want %d", attempt, got, acp.ErrAuthRequired)
			}
			d.request(8, "session/prompt", map[string]any{"sessionId": "sess_1", "prompt": []any{map[string]any{"type": "text", "text": "hi"}}})
			if got := errCode(t, d.untilResponseID(8)); got != acp.ErrAuthRequired {
				t.Errorf("SECURITY: [acp-auth-gate] session/prompt with authenticate %s answered code %v, want %d", attempt, got, acp.ErrAuthRequired)
			}
			if minted.Load() != 0 || loaded.Load() != 0 {
				t.Errorf("SECURITY: [acp-auth-gate] embedder reached without authentication: NewSession %d, LoadSession %d", minted.Load(), loaded.Load())
			}
		})
	}
}

// TestSessionsOpenAfterAuthenticate: a successful authenticate opens
// the session methods; an agent with no auth methods never gates.
func TestSessionsOpenAfterAuthenticate(t *testing.T) {
	var minted, loaded atomic.Int64
	opts := &acp.Options{
		AuthMethods:  []acp.AuthMethod{{ID: "agent-login", Name: "Agent login"}},
		Authenticate: func(context.Context, string) error { return nil },
	}
	d := startDialog(t, gatedAgent(&minted, &loaded), opts)
	d.initialize()
	d.request(5, "authenticate", map[string]any{"methodId": "agent-login"})
	respResult(t, d.untilResponseID(5))
	d.request(6, "session/new", map[string]any{"cwd": "/tmp", "mcpServers": []any{}})
	if id := respResult(t, d.untilResponseID(6))["sessionId"]; id != "sess_1" {
		t.Errorf("session/new after authenticate = %v, want sess_1", id)
	}

	open := startDialog(t, gatedAgent(&minted, &loaded), nil)
	if id := open.newSession("/tmp"); id == "" {
		t.Error("session/new on an agent with no auth methods returned no id")
	}
}
