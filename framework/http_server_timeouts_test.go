package framework

import (
	"io"
	"net/http"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// HTTPServerTimeoutsConfig fields are *time.Duration so that "unset" (nil →
// keep the framework default) is distinct from "explicit 0" (→ disable,
// net/http semantics). Call sites build the pointers with the Go 1.26
// new(expr) builtin, e.g. new(2 * time.Second) → *time.Duration.

// TestHTTPServerTimeoutsDefault pins unchanged default behavior: an app with
// no override starts the http.Server with the four deadlines the framework has
// always shipped. A regression that silently changed a default would fail here.
func TestHTTPServerTimeoutsDefault(t *testing.T) {
	app := NewApp()
	_, stop := startOnRandomPort(t, app)
	defer stop()

	app.serverMu.Lock()
	srv := app.server
	app.serverMu.Unlock()

	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s default", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 60*time.Second {
		t.Errorf("ReadTimeout = %v, want 60s default", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 60*time.Second {
		t.Errorf("WriteTimeout = %v, want 60s default", srv.WriteTimeout)
	}
	if srv.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s default", srv.IdleTimeout)
	}
}

// TestHTTPServerTimeoutsOverride sets every field and confirms each lands on
// the underlying http.Server. All four are set so any wiring miss surfaces.
func TestHTTPServerTimeoutsOverride(t *testing.T) {
	app := NewApp(WithHTTPServerTimeouts(HTTPServerTimeoutsConfig{
		ReadHeaderTimeout: new(3 * time.Second),
		ReadTimeout:       new(7 * time.Second),
		WriteTimeout:      new(8 * time.Second),
		IdleTimeout:       new(9 * time.Second),
	}))
	_, stop := startOnRandomPort(t, app)
	defer stop()

	app.serverMu.Lock()
	srv := app.server
	app.serverMu.Unlock()

	if srv.ReadHeaderTimeout != 3*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 3s", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 7*time.Second {
		t.Errorf("ReadTimeout = %v, want 7s", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 8*time.Second {
		t.Errorf("WriteTimeout = %v, want 8s", srv.WriteTimeout)
	}
	if srv.IdleTimeout != 9*time.Second {
		t.Errorf("IdleTimeout = %v, want 9s", srv.IdleTimeout)
	}
}

// TestHTTPServerTimeoutsExplicitZeroDisables: a pointer to 0 is NOT "keep
// default", it is an explicit disable (net/http: 0 = no timeout). This
// distinction is the entire reason the fields are pointers rather than plain
// time.Duration, where the zero value could only mean one thing.
func TestHTTPServerTimeoutsExplicitZeroDisables(t *testing.T) {
	app := NewApp(WithHTTPServerTimeouts(HTTPServerTimeoutsConfig{
		WriteTimeout: new(0 * time.Second),
	}))
	_, stop := startOnRandomPort(t, app)
	defer stop()

	app.serverMu.Lock()
	srv := app.server
	app.serverMu.Unlock()

	if srv.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %v, want 0 (explicit disable), not the 60s default", srv.WriteTimeout)
	}
	// An unset field keeps its default.
	if srv.ReadTimeout != 60*time.Second {
		t.Errorf("ReadTimeout = %v, want 60s default (unset field must keep default)", srv.ReadTimeout)
	}
}

// TestHTTPServerTimeoutsShortWriteCutsSlowHandler reproduces the framework's
// existing behavior, a handler that outlives the server write deadline is
// severed, using a short knob instead of the real 60s. The timeout middleware
// is removed (DisableRequestTimeout) so the server-level WriteTimeout is the
// only deadline in play, isolating exactly the knob under test.
func TestHTTPServerTimeoutsShortWriteCutsSlowHandler(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{
		DisableRequestTimeout: true,
		HTTPServerTimeouts: HTTPServerTimeoutsConfig{
			WriteTimeout: new(100 * time.Millisecond),
		},
	}))
	app.Router().Get("/slow", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(400 * time.Millisecond) // well past the 100ms deadline
		_, _ = w.Write([]byte("done"))
	}))
	addr, stop := startOnRandomPort(t, app)
	defer stop()

	cut := false
	resp, err := http.Get("http://" + addr + "/slow")
	if err != nil {
		cut = true // connection severed at the deadline before headers completed
	} else {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		// A truncated/empty response also counts as severed.
		cut = resp.StatusCode != http.StatusOK || string(body) != "done"
	}
	if !cut {
		t.Fatal("slow handler completed despite the 100ms WriteTimeout — the knob did not cut it")
	}
}

// TestHTTPServerTimeoutsRaisedLetsSlowHandlerThrough is the bidirectional
// counterpart to the cut test above: the SAME 400ms handler that a 100ms
// WriteTimeout severs completes cleanly under a raised 2s limit. The
// configured value governs the cut, not the fixed default.
func TestHTTPServerTimeoutsRaisedLetsSlowHandlerThrough(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{
		DisableRequestTimeout: true,
		HTTPServerTimeouts: HTTPServerTimeoutsConfig{
			WriteTimeout: new(2 * time.Second),
		},
	}))
	app.Router().Get("/slow", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(400 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	}))
	addr, stop := startOnRandomPort(t, app)
	defer stop()

	resp, err := http.Get("http://" + addr + "/slow")
	if err != nil {
		t.Fatalf("GET /slow under raised WriteTimeout: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "done" {
		t.Fatalf("slow handler did not complete under raised WriteTimeout: status=%d body=%q", resp.StatusCode, body)
	}
}

// TestHTTPServerTimeoutsBeatDisableRequestTimeout: an explicit
// HTTPServerTimeouts field is authoritative. DisableRequestTimeout zeroes only
// the read/write fields the host left unset. So a host can drop the timeout
// middleware (its other effect) yet keep a custom server-level write deadline.
// The existing TestDisableTimeoutRelaxesServer still holds: with no explicit
// override, DisableRequestTimeout keeps zeroing read/write.
func TestHTTPServerTimeoutsBeatDisableRequestTimeout(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{
		DisableRequestTimeout: true,
		HTTPServerTimeouts: HTTPServerTimeoutsConfig{
			WriteTimeout: new(5 * time.Second),
		},
	}))
	_, stop := startOnRandomPort(t, app)
	defer stop()

	app.serverMu.Lock()
	srv := app.server
	app.serverMu.Unlock()

	if srv.WriteTimeout != 5*time.Second {
		t.Fatalf("WriteTimeout = %v, want 5s (explicit override beats DisableRequestTimeout's zero)", srv.WriteTimeout)
	}
	if srv.ReadTimeout != 0 {
		t.Fatalf("ReadTimeout = %v, want 0 (DisableRequestTimeout zeros the unset read deadline)", srv.ReadTimeout)
	}
}
