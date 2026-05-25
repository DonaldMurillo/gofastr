package harness

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/profile"
)

func TestNewBoots(t *testing.T) {
	dir := t.TempDir()
	p, err := profile.Parse(strings.NewReader(`
schema_version = 1
name = "default"
default_model = "zai:glm-5.1"
prompt_header = "You are helpful."
context_sources = ["AGENTS.md"]
skill_packs = ["builtin"]
tool_packs = ["fs", "search"]
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
		CredstorePass: "test-passphrase",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Shutdown()

	if len(h.Providers) != 2 {
		t.Errorf("Providers count = %d, want 2 (openrouter+zai)", len(h.Providers))
	}
	if h.Tools == nil {
		t.Fatal("Tools registry not initialized")
	}
	tools := h.Tools.List()
	// Limited by tool_packs = ["fs", "search"]: expect Read, Write, Edit, Glob, Ls, Grep.
	got := map[string]bool{}
	for _, tl := range tools {
		got[tl.Name()] = true
	}
	for _, want := range []string{"Read", "Write", "Edit", "Glob", "Ls", "Grep"} {
		if !got[want] {
			t.Errorf("missing tool %q", want)
		}
	}
	if got["Bash"] {
		t.Error("Bash should not be enabled when only fs+search packs requested")
	}
}

func TestCreateSessionAttachable(t *testing.T) {
	dir := t.TempDir()
	p, _ := profile.Parse(strings.NewReader(`
schema_version = 1
name = "default"
default_model = "zai:glm-5.1"
prompt_header = ""
context_sources = []
tool_packs = ["fs"]
permissions = "preset/default.toml"
allow_project_hooks = false
`))
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
	defer h.Shutdown()
	sess := h.CreateSession(h.Providers[0], "openrouter:test")
	if h.Mux.EngineFor(sess) == nil {
		t.Fatal("engine not registered with mux after CreateSession")
	}
}
