package acp_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// Property: every client-driven method that can reach embedder code is
// gated on requireReady — authenticate establishes credentials, so a
// connection that never sent initialize (no version agreement, no
// capability negotiation) must not reach Options.Authenticate.
// session/new, session/load and session/prompt hold this gate; the
// authenticate arm must too, or an unversioned peer drives the server's
// privileged credential callback.
func TestAuthenticateRequiresReady(t *testing.T) {
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
			"the authenticate dispatch skips the requireReady gate session/new, session/load and session/prompt " +
			"all enforce, so the embedder's credential hook is reachable without capability negotiation or " +
			"version agreement")
	}
	if f["error"] == nil {
		t.Errorf("SECURITY: [acp-auth-ready] authenticate before initialize was accepted: %v — want an "+
			"ErrInvalidRequest refusal like every other session method on a not-ready connection", f)
	}
}
