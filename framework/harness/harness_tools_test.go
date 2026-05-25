package harness

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/profile"
)

// TestToolSchemasPopulated verifies the harness wires registered
// tools onto Engine.Tools so the model knows it has capabilities.
// (Without this the model says "I don't have tools" — the bug from
// the user's screenshot.)
func TestToolSchemasPopulated(t *testing.T) {
	dir := t.TempDir()
	p, err := profile.Parse(strings.NewReader(`
schema_version = 1
name = "default"
default_model = "zai:glm-5.1"
prompt_header = ""
context_sources = []
tool_packs = ["fs", "search", "git", "web"]
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
	defer h.Shutdown()

	schemas := h.toolSchemas()
	if len(schemas) == 0 {
		t.Fatal("no tool schemas produced — model would think it has no capabilities")
	}
	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Name] = true
		if s.Description == "" {
			t.Errorf("tool %q has empty description", s.Name)
		}
		if len(s.InputSchema) == 0 {
			t.Errorf("tool %q has empty input schema", s.Name)
		}
	}
	for _, want := range []string{"Read", "Write", "Bash", "Grep", "Glob", "Ls", "WebFetch"} {
		if !names[want] {
			t.Errorf("missing %q in tool schemas (registered tools: %v)", want, names)
		}
	}
}
