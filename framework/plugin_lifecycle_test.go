package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// countingPlugin tracks how many times Init was called. Used by tests
// that pin the InitPlugins-idempotency contract: calling InitPlugins
// twice (e.g. once manually pre-Start, once again by Start itself)
// must init each plugin exactly once.
type countingPlugin struct {
	name  string
	count int
}

func (p *countingPlugin) Name() string      { return p.name }
func (p *countingPlugin) Init(_ *App) error { p.count++; return nil }

// stampPlugin tags every response with X-Plugin-Stamp from Init by
// calling app.Use. Used to prove that a plugin's middleware wraps
// routes registered BEFORE the plugin was even registered — the
// router-late-binding contract.
type stampPlugin struct {
	name string
}

func (p *stampPlugin) Name() string { return p.name }
func (p *stampPlugin) Init(app *App) error {
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Plugin-Stamp", p.name)
			next.ServeHTTP(w, r)
		})
	})
	return nil
}

// conflictRoutePlugin registers a route inside Init. Used together
// with another plugin registering the same route to exercise the
// attribution-on-panic path.
type conflictRoutePlugin struct {
	name    string
	pattern string
}

func (p *conflictRoutePlugin) Name() string { return p.name }
func (p *conflictRoutePlugin) Init(app *App) error {
	app.Router().Get(p.pattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	return nil
}

// TestInitPluginsIsIdempotent guards the documented "callable manually
// from tests, also called by Start" contract. Without a guard, two
// calls double-register everything (routes panic on the mux's dup
// check, middleware double-attaches, sinks leak).
func TestInitPluginsIsIdempotent(t *testing.T) {
	app := NewApp()
	p := &countingPlugin{name: "counter"}
	app.RegisterPlugin(p)

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("first InitPlugins: %v", err)
	}
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("second InitPlugins: %v", err)
	}
	if p.count != 1 {
		t.Fatalf("plugin Init was called %d times, want 1", p.count)
	}
}

// TestLatePluginWrapsExistingRoutes pins the central architectural
// contract: a plugin registered AFTER routes can still contribute
// middleware that wraps them. Before the router late-binding refactor
// + collapsed plugin interfaces, plugin middleware never fired because
// the router wrapped at registration time and plugins ran later.
func TestLatePluginWrapsExistingRoutes(t *testing.T) {
	app := NewApp()

	app.Router().Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	app.RegisterPlugin(&stampPlugin{name: "stamper"})

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/probe")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Plugin-Stamp"); got != "stamper" {
		t.Fatalf("plugin middleware did not wrap pre-registered route; X-Plugin-Stamp = %q", got)
	}
}

// TestHarnessAutoInitsPlugins pins the contract that TestHarness wires
// plugins automatically — without it, RegisterPlugin'd state silently
// doesn't apply under the harness and tests pass for the wrong reason.
func TestHarnessAutoInitsPlugins(t *testing.T) {
	app := NewApp()
	app.RegisterPlugin(&stampPlugin{name: "harness-stamper"})
	app.Router().Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	h := TestHarness(t, app)
	r := h.Get("/probe").AssertStatus(t, http.StatusOK)
	if got := r.recorder.Header().Get("X-Plugin-Stamp"); got != "harness-stamper" {
		t.Fatalf("plugin middleware did not run under TestHarness; X-Plugin-Stamp = %q", got)
	}
}

// TestRegisterPluginAfterInitPanics pins the contract that a plugin
// registered after InitPlugins is a contract violation (the new
// plugin's Init would never fire under the idempotency guard).
func TestRegisterPluginAfterInitPanics(t *testing.T) {
	app := NewApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("RegisterPlugin after InitPlugins did not panic")
		}
	}()
	app.RegisterPlugin(&countingPlugin{name: "too-late"})
}

// retryPlugin fails on its first Init call and succeeds afterward.
type retryPlugin struct {
	name  string
	calls int
}

func (p *retryPlugin) Name() string { return p.name }
func (p *retryPlugin) Init(_ *App) error {
	p.calls++
	if p.calls == 1 {
		return errInitFailureSim
	}
	return nil
}

var errInitFailureSim = &initFailureSim{}

type initFailureSim struct{}

func (e *initFailureSim) Error() string { return "simulated init failure" }

// TestPluginNameRejected covers invalid Plugin / Battery names. Empty,
// whitespace-only, control-char, and oversized names show up confusingly
// in errors and structured log entries; rejecting them at Register time
// is friendlier than waiting for a runtime panic.
func TestPluginNameRejected(t *testing.T) {
	cases := []struct {
		name string
		bad  string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"control char", "name\x00with\x00nul"},
		{"too long", strings.Repeat("x", 200)},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			app := NewApp()
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("RegisterPlugin(%q) did not panic on invalid name", c.bad)
				}
			}()
			app.RegisterPlugin(&countingPlugin{name: c.bad})
		})
	}
}

// TestInitPluginsRetrySkipsSucceeded pins that a failed InitPlugins
// can be retried without re-running plugins that already applied side
// effects. Without per-module tracking the rollback latch would re-run
// the successful plugin's Init and panic on ServeMux's duplicate-pattern
// check.
func TestInitPluginsRetrySkipsSucceeded(t *testing.T) {
	app := NewApp()
	good := &countingPlugin{name: "good"}
	bad := &retryPlugin{name: "bad"}
	app.RegisterPlugin(good)
	app.RegisterPlugin(bad)

	if err := app.InitPlugins(); err == nil {
		t.Fatal("InitPlugins should have returned bad's first-call error")
	}
	if good.count != 1 {
		t.Fatalf("good.count = %d after first InitPlugins, want 1", good.count)
	}

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins retry: %v", err)
	}
	if good.count != 1 {
		t.Fatalf("good was re-initialized on retry: count went 1 → %d", good.count)
	}
	if bad.calls != 2 {
		t.Errorf("bad.calls = %d, want 2 (failed then succeeded)", bad.calls)
	}
}

// TestPluginRouteConflictAttributed pins that a duplicate-pattern
// panic from net/http.ServeMux gets attributed to the offending
// plugin instead of surfacing as a raw bare panic.
func TestPluginRouteConflictAttributed(t *testing.T) {
	app := NewApp()
	app.RegisterPlugin(&conflictRoutePlugin{name: "first", pattern: "/dup"})
	app.RegisterPlugin(&conflictRoutePlugin{name: "second", pattern: "/dup"})

	err := app.InitPlugins()
	if err == nil {
		t.Fatal("InitPlugins did not return an error for duplicate route")
	}
	if !strings.Contains(err.Error(), "second") {
		t.Errorf("error does not name the offending plugin: %v", err)
	}
}
