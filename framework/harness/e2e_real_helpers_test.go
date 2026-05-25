//go:build e2e_real

package harness

import (
	"os"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/secrets"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// TestMain auto-loads .harness-secrets/env before any e2e test runs.
// Env vars set in the shell still win — the file is the fallback.
func TestMain(m *testing.M) {
	_, _ = secrets.LoadRepo()
	os.Exit(m.Run())
}

// newEmptyToolRegistry returns a tool registry with no tools — the
// real-provider tests don't exercise tool-use, just text + usage.
func newEmptyToolRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	return tool.NewRegistry()
}

// newRealMux constructs a multiplexer bound to the given engine, with
// no auth — the test runs in-process and never touches the network
// transports.
func newRealMux(t *testing.T, eng *engine.Engine) *multiplex.Mux {
	t.Helper()
	m := multiplex.New()
	m.RegisterEngine(eng)
	return m
}
