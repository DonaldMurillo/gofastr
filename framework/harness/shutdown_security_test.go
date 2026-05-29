package harness

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/profile"
)

func newShutdownTestHarness(t *testing.T) *Harness {
	t.Helper()
	dir := t.TempDir()
	p, err := profile.Parse(strings.NewReader(`
schema_version = 1
name = "default"
default_model = "zai:glm-5.1"
prompt_header = ""
context_sources = []
tool_packs = ["fs"]
permissions = "preset/default.toml"
allow_project_hooks = false
`))
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(Config{
		Profile:       p,
		WorkingDir:    dir,
		XDGConfig:     filepath.Join(dir, "config"),
		XDGState:      filepath.Join(dir, "state"),
		CredstorePass: "pp",
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// waitGoroutinesSettle polls until the goroutine count drops to at or
// below target, or the deadline passes. Returns the last observed count.
func waitGoroutinesSettle(target int, d time.Duration) int {
	deadline := time.Now().Add(d)
	for {
		runtime.GC()
		n := runtime.NumGoroutine()
		if n <= target || time.Now().After(deadline) {
			return n
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Shutdown must release every per-session persistLoop goroutine and
// unregister every engine. Each CreateSession spawns a goroutine bound
// to a context; if Shutdown doesn't cancel them, they leak for the
// process lifetime.
func TestShutdownReleasesSessions(t *testing.T) {
	h := newShutdownTestHarness(t)

	baseline := waitGoroutinesSettle(0, time.Second)

	const n = 8
	for i := 0; i < n; i++ {
		h.CreateSession(h.Providers[0], "openrouter:test")
	}

	// Sanity: sessions are registered and goroutines are up.
	if got := runtime.NumGoroutine(); got <= baseline {
		t.Fatalf("expected goroutine count to rise after %d sessions; baseline=%d got=%d", n, baseline, got)
	}

	if err := h.Shutdown(); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	// After Shutdown the persistLoop goroutines must exit. Allow a
	// generous settle window for the bus close cascade.
	settled := waitGoroutinesSettle(baseline+2, 2*time.Second)
	if settled > baseline+2 {
		t.Errorf("goroutine leak after Shutdown: baseline=%d settled=%d (per-session persistLoop goroutines not released)", baseline, settled)
	}
}

// Shutdown must unregister engines from the Mux so a post-shutdown
// lookup returns nothing (no dangling engine references).
func TestShutdownUnregistersEngines(t *testing.T) {
	h := newShutdownTestHarness(t)
	sess := h.CreateSession(h.Providers[0], "openrouter:test")
	if h.Mux.EngineFor(sess) == nil {
		t.Fatal("engine not registered after CreateSession")
	}
	if err := h.Shutdown(); err != nil {
		t.Fatalf("Shutdown error: %v", err)
	}
	if h.Mux.EngineFor(sess) != nil {
		t.Error("engine still registered with mux after Shutdown (dangling reference)")
	}
}
