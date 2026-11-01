package harness

import (
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// mustSession is CreateSession for a test that expects it to succeed.
func mustSession(t *testing.T, h *Harness, prov provider.Provider, model string) ids.SessionID {
	t.Helper()
	sess, err := h.CreateSession(prov, model)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sess
}

// A CreateSession that ran after Shutdown had taken its snapshot of the
// sessions registered a session nobody would tear down, writing to the
// store Shutdown had closed (2026-10-31 review). Once Shutdown has begun,
// CreateSession registers nothing.
func TestCreateSessionAfterShutdownRefused(t *testing.T) {
	h := newShutdownTestHarness(t)
	if err := h.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if _, err := h.CreateSession(h.Providers[0], "openrouter:test"); !errors.Is(err, ErrShutdown) {
		t.Errorf("CreateSession after Shutdown: err = %v, want ErrShutdown", err)
	}
	h.sessionMu.Lock()
	defer h.sessionMu.Unlock()
	if n := len(h.sessionRuns); n != 0 {
		t.Fatalf("%d session(s) registered after Shutdown, want none", n)
	}
}
