package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// Property: kiln's tool API is unauthenticated by design, so a loopback
// bind is the only thing separating it from the network. sameOrigin
// alone cannot hold that line. Under DNS rebinding the attacker owns
// both Origin and Host, so they match and the guard passes. Pinning
// Host to a literal loopback name is what refuses the rebound request.
//
// This matters more here than anywhere else in the repo: POST
// /kiln/agent chooses the argv of a spawned process, so reaching the
// tool API is equivalent to code execution.
func TestOriginGuardRejectsReboundHost(t *testing.T) {
	prev := kilnLoopbackBound
	kilnLoopbackBound = true
	defer func() { kilnLoopbackBound = prev }()

	guarded := originGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	call := func(method, host, origin string) int {
		r := httptest.NewRequest(method, "/kiln/agent", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		guarded.ServeHTTP(w, r)
		return w.Code
	}

	// The rebind: attacker's own name, and Origin agrees with it. This
	// is exactly the shape sameOrigin lets through.
	if got := call("POST", "evil.test:8765", "http://evil.test:8765"); got != http.StatusForbidden {
		t.Errorf("rebound POST got %d, want 403", got)
	}
	// GET is guarded too: /kiln/world and /kiln/status disclose the app.
	if got := call("GET", "evil.test:8765", ""); got != http.StatusForbidden {
		t.Errorf("rebound GET got %d, want 403", got)
	}
	// Legitimate loopback callers keep working, in both spellings.
	for _, h := range []string{"127.0.0.1:8765", "localhost:8765", "[::1]:8765"} {
		if got := call("POST", h, ""); got != http.StatusOK {
			t.Errorf("loopback Host %q got %d, want 200", h, got)
		}
	}
	// Cross-origin from a real loopback page is still refused.
	if got := call("POST", "127.0.0.1:8765", "http://evil.test"); got != http.StatusForbidden {
		t.Errorf("cross-origin POST got %d, want 403", got)
	}
}

// An operator who deliberately exposed kiln (--addr 0.0.0.0:8765) gets
// no Host pin, because the framework cannot know the intended public
// name. The banner's "unauthenticated" warning is the contract there.
func TestLoopbackBindDetection(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8765": true,
		"localhost:8765": true,
		"[::1]:8765":     true,
		"0.0.0.0:8765":   false,
		":8765":          false,
		"192.168.1.5:87": false,
	} {
		if got := isLoopbackBindAddr(addr); got != want {
			t.Errorf("isLoopbackBindAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

// Property: a request must not be able to choose the argv of a process
// kiln spawns. POST /kiln/agent name="custom" supplied the entire argv,
// so anything that could reach the unauthenticated tool API had
// arbitrary code execution. It is now opt-in.
func TestCustomAgentDisabledByDefault(t *testing.T) {
	prev := allowCustomAgent
	allowCustomAgent = false
	defer func() { allowCustomAgent = prev }()

	r := router.New()
	mountAgentRoutes(r, NewAdapterStore(Adapter{Name: "none"}), nil)

	body := bytes.NewBufferString(`{"name":"custom","custom":"sh -c id"}`)
	req := httptest.NewRequest(http.MethodPost, "/kiln/agent", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["ok"] == true {
		t.Fatalf("custom argv was accepted without opt-in: %s", rec.Body.String())
	}
}

// withCustomAgentRouter mounts the agent routes with
// --allow-custom-agent forced on and returns the router plus a restore
// func.
func withCustomAgentRouter() (*router.Router, func()) {
	prevAllow := allowCustomAgent
	allowCustomAgent = true
	store := NewAdapterStore(Adapter{})
	r := router.New()
	mountAgentRoutes(r, store, nil)
	return r, func() { allowCustomAgent = prevAllow }
}

// Property: duplicate and case-folded JSON keys are ambiguity, not
// data — POST /kiln/agent must reject a body carrying two spellings of
// the "custom" key, because that field becomes the entire argv of a
// process kiln spawns and last-wins would run a command the operator
// never saw.
func TestAgentEndpointRejectsDuplicateKeys(t *testing.T) {
	r, restore := withCustomAgentRouter()
	defer restore()
	body := `{"name":"custom","custom":"/bin/echo benign-first","custom":"/bin/echo attacker-second"}`
	req := httptest.NewRequest(http.MethodPost, "/kiln/agent", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /kiln/agent accepted duplicate-key body for the %q key (encoding/json resolves it last-wins, and the last value becomes the spawned-process argv): status %d, body %.200s — want 400", "custom", rec.Code, rec.Body.String())
	}
}

// TestAgentEndpointRejectsCaseFoldedKeys: "custom"/"Custom" fold onto
// the same struct field via stdlib json's tag-insensitive match — the
// folded spelling must not install a second argv.
func TestAgentEndpointRejectsCaseFoldedKeys(t *testing.T) {
	r, restore := withCustomAgentRouter()
	defer restore()
	body := `{"name":"custom","custom":"/bin/echo benign-first","Custom":"/bin/echo attacker-second"}`
	req := httptest.NewRequest(http.MethodPost, "/kiln/agent", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /kiln/agent accepted case-folded-key body for the %q key (encoding/json resolves it last-wins, and the last value becomes the spawned-process argv): status %d, body %.200s — want 400", "custom", rec.Code, rec.Body.String())
	}
}

// Property: an agent's working directory is owner-only and unique per
// turn. The spawned agents run with bash, so whatever controls their
// cwd controls what they read and trust; a pre-created or shared
// /tmp/kiln-* name must not become that cwd.
func TestAgentWorkDirOwnerOnlyUnique(t *testing.T) {
	// Registry grounding: every built-in that wants an isolated cwd
	// names a fixed path directly under os.TempDir(). If this loop ever
	// finds none, the registry side of the contract moved; the
	// spawn-site pin below still applies to whatever replaced it.
	fixedCount := 0
	for name, a := range adapters {
		if a.Dir == "" {
			continue
		}
		fixedCount++
		if filepath.Dir(a.Dir) != filepath.Clean(os.TempDir()) {
			t.Logf("adapter %q Dir no longer directly under TempDir: %s", name, a.Dir)
		}
	}
	if fixedCount == 0 {
		t.Fatalf("no built-in adapter carries a Dir; registry surface moved, revisit this pin")
	}

	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatalf("live.New: %v", err)
	}
	tools := protocol.New(l)

	root := t.TempDir()
	fixed := filepath.Join(root, "kiln-pinned") // same fixed-name shape, scratch location
	adapter := Adapter{
		Name:      "pinned",
		Dir:       fixed,
		BuildArgs: func(string) []string { return []string{"/bin/pwd"} },
	}
	runOneAgentTurn(context.Background(), log.New(io.Discard, "", 0), tools, adapter, "http://127.0.0.1:1", "pinned turn")

	fi, err := os.Stat(fixed)
	if err != nil {
		t.Fatalf("agent working dir was not created: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("agent working dir is mode %o — the cwd of a bash-capable coding agent must be owner-only (0700), never group/world-traversable", fi.Mode().Perm())
	}
	// The per-turn cwd is a unique child, removed when the turn ends;
	// the fixed parent must not accumulate turn directories.
	entries, err := os.ReadDir(fixed)
	if err != nil {
		t.Fatalf("read agent working dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("agent working dir still holds %d entry(ies) after the turn; per-turn cwds must be cleaned up", len(entries))
	}
}
