package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeF(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGenerateConfigSearchStopsAtRepoRoot pins that config discovery
// cannot walk out of the project.
//
// Attack: searchDirs walked from the working directory to `/` with no
// boundary, and a discovered config may declare a `command` extension
// that `gofastr generate` runs with exec.CommandContext. A config
// planted anywhere above the project — a shared parent directory, a
// cloned repo's parent, /tmp, $HOME — therefore executed as the
// developer on a no-argument `gofastr generate`, printing nothing.
//
// The boundary is the module/repo root: a directory holding go.mod,
// go.work, or .git. That is where "the project" ends by every other
// definition the framework uses (framework/isolation resolves the same
// way), and nothing above it is the project's to configure.
func TestGenerateConfigSearchStopsAtRepoRoot(t *testing.T) {
	root := t.TempDir()
	// A config the attacker planted above the project.
	writeF(t, filepath.Join(root, "gofastr.codegen.yml"), "version: 1\ncodegen:\n  output: gen\n")
	// The project: a module root two levels down.
	project := filepath.Join(root, "workspace", "app")
	writeF(t, filepath.Join(project, "go.mod"), "module example.com/app\n\ngo 1.26\n")
	writeF(t, filepath.Join(project, "internal", "pkg", ".keep"), "")

	for _, start := range []string{project, filepath.Join(project, "internal", "pkg")} {
		got, err := DiscoverConfig(start)
		if err != nil {
			t.Fatalf("DiscoverConfig(%s): %v", start, err)
		}
		if got.Found {
			t.Errorf("SECURITY: [rce] config discovery from %s escaped the module root and found %s — a config planted above the project can name a command extension that runs as the developer",
				start, got.Path)
		}
	}

	// A config INSIDE the project must still be found from a subdirectory.
	writeF(t, filepath.Join(project, "gofastr.codegen.yml"), "version: 1\ncodegen:\n  output: gen\n")
	got, err := DiscoverConfig(filepath.Join(project, "internal", "pkg"))
	if err != nil {
		t.Fatalf("DiscoverConfig: %v", err)
	}
	if !got.Found {
		t.Fatal("config discovery no longer walks up to the project's own root")
	}
	if got.ProjectDir != project {
		t.Errorf("found config at %s, want the module root %s", got.ProjectDir, project)
	}
}

// A repo root marked by .git (no go.mod) bounds the walk the same way.
func TestGenerateConfigSearchStopsAtGitRoot(t *testing.T) {
	root := t.TempDir()
	writeF(t, filepath.Join(root, "gofastr.codegen.yml"), "version: 1\ncodegen:\n  output: gen\n")
	project := filepath.Join(root, "repo")
	writeF(t, filepath.Join(project, ".git", "HEAD"), "ref: refs/heads/main\n")

	got, err := DiscoverConfig(project)
	if err != nil {
		t.Fatalf("DiscoverConfig: %v", err)
	}
	if got.Found {
		t.Errorf("SECURITY: [rce] discovery escaped a .git repo root and found %s", got.Path)
	}
}

// TestDiscoveredCommandExtNeedsOptIn pins that a DISCOVERED config
// cannot silently execute a command.
//
// Even bounded at the repo root, a `command` extension in a config the
// user did not name on the command line runs a binary chosen by whoever
// wrote that file — a cloned repo, a dependency vendored into the tree,
// a teammate's branch. `--config` is an explicit act; discovery is not.
func TestDiscoveredCommandExtNeedsOptIn(t *testing.T) {
	project := t.TempDir()
	writeF(t, filepath.Join(project, "go.mod"), "module example.com/app\n\ngo 1.26\n")
	writeF(t, filepath.Join(project, "gofastr.codegen.yml"), `version: 1
codegen:
  output: gen
  extensions:
    - name: evil
      command: ["/bin/sh", "-c", "touch /tmp/pwned"]
`)

	got, err := DiscoverConfig(project)
	if err != nil {
		t.Fatalf("DiscoverConfig: %v", err)
	}
	if !got.Found {
		t.Fatal("test setup: config not discovered")
	}
	if !got.Discovered {
		t.Error("a config found by walking the tree must be marked Discovered so callers can require an opt-in before running its command extensions")
	}

	err = CheckCommandExtensions(got)
	if err == nil {
		t.Fatalf("SECURITY: [rce] a discovered config's command extension is runnable with no opt-in: %s", got.Path)
	}
	if !strings.Contains(err.Error(), "GOFASTR_CODEGEN_ALLOW_COMMANDS") {
		t.Errorf("the refusal must name the opt-in; got: %v", err)
	}

	t.Setenv("GOFASTR_CODEGEN_ALLOW_COMMANDS", "1")
	if err := CheckCommandExtensions(got); err != nil {
		t.Errorf("the documented opt-in did not permit the command extension: %v", err)
	}
}

// A config the user named explicitly is an act of intent, so its command
// extensions run without a second gate.
func TestExplicitConfigRunsCommandExtensions(t *testing.T) {
	project := t.TempDir()
	writeF(t, filepath.Join(project, "gofastr.codegen.yml"), `version: 1
codegen:
  output: gen
  extensions:
    - name: local
      command: ["echo", "hi"]
`)
	cfg, err := LoadConfig(filepath.Join(project, "gofastr.codegen.yml"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	explicit := Discovery{Path: filepath.Join(project, "gofastr.codegen.yml"), ProjectDir: project, Config: cfg, Found: true}
	if err := CheckCommandExtensions(explicit); err != nil {
		t.Errorf("an explicitly-named config must run its own command extensions: %v", err)
	}
}
