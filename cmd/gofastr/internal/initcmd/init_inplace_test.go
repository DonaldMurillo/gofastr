package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"golang.org/x/mod/modfile"
)

// inPlaceDir chdirs into a fresh directory named base and pins a dev CLI
// on the local toolchain, so init needs no network.
func inPlaceDir(t *testing.T, base string) string {
	t.Helper()
	oldVersion := cli.Version
	cli.Version = "dev"
	t.Cleanup(func() { cli.Version = oldVersion })
	t.Setenv("GOTOOLCHAIN", "local")
	dir := filepath.Join(t.TempDir(), base)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	clitest.CovT_chdir(t, dir)
	return dir
}

func runInitCode(t *testing.T, args ...string) int {
	t.Helper()
	var code int
	clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunInit(args) })
	})
	return code
}

// `gofastr init .` named the module local/., which go mod init refuses,
// after every other file was written.
func TestInitDotModuleNamedAfterDir(t *testing.T) {
	inPlaceDir(t, "inplace-app")
	if code := runInitCode(t, ".", "--no-entity"); code != -1 {
		t.Fatalf("init . exited %d", code)
	}
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mod.Module.Mod.Path != "local/inplace-app" {
		t.Fatalf("module = %q, want local/inplace-app", mod.Module.Mod.Path)
	}
}

// init . in a directory that already holds the app's own files must not
// replace them: the same rule a repeat `init <name>` follows.
func TestInitDotKeepsExistingFiles(t *testing.T) {
	inPlaceDir(t, "existing-app")
	keep := map[string]string{"main.go": "package main // mine\n", ".env": "GOFASTR_SECRET=keep\n"}
	for name, body := range keep {
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if code := runInitCode(t, "."); code != 1 {
		t.Fatalf("init . over existing files exited %d, want 1", code)
	}
	for name, body := range keep {
		if got, _ := os.ReadFile(name); string(got) != body {
			t.Errorf("init . replaced %s: %q", name, got)
		}
	}
	if _, err := os.Stat("screens.go"); err == nil {
		t.Error("a refused init . still wrote screens.go")
	}
}

// --force is the named way to scaffold over existing files.
func TestInitDotForceOverwrites(t *testing.T) {
	inPlaceDir(t, "forced-app")
	if err := os.WriteFile("main.go", []byte("package main // mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runInitCode(t, ".", "--no-entity", "--force"); code != -1 {
		t.Fatalf("init . --force exited %d", code)
	}
	if got, _ := os.ReadFile("main.go"); strings.Contains(string(got), "// mine") {
		t.Error("init . --force kept the old main.go")
	}
}

// A repo's own .gitignore is kept, and gains the entries the scaffold
// needs, .env first among them.
func TestInitDotMergesGitignore(t *testing.T) {
	inPlaceDir(t, "ignore-app")
	if err := os.WriteFile(".gitignore", []byte("node_modules/\n.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runInitCode(t, ".", "--no-entity"); code != -1 {
		t.Fatalf("init . exited %d", code)
	}
	got, _ := os.ReadFile(".gitignore")
	if !strings.HasPrefix(string(got), "node_modules/\n.env\n") || !strings.Contains(string(got), "\n.gofastr/\n") {
		t.Errorf(".gitignore not merged:\n%s", got)
	}
	if strings.Count(string(got), ".env\n") != 1 {
		t.Errorf(".gitignore repeats .env:\n%s", got)
	}
}

// A misspelled flag scaffolded the default app and exited 0.
func TestInitRefusesUnknownFlag(t *testing.T) {
	inPlaceDir(t, "work")
	if code := runInitCode(t, "app", "--noentty"); code != 2 {
		t.Fatalf("unknown flag exited %d, want 2", code)
	}
	if _, err := os.Stat("app"); err == nil {
		t.Error("init with an unknown flag created the project")
	}
}

// --reinit refreshes an existing project; the fresh-scaffold guard
// refused it as "already exists".
func TestInitReinitExistingDir(t *testing.T) {
	inPlaceDir(t, "work")
	if err := os.Mkdir("myapp", 0o755); err != nil {
		t.Fatal(err)
	}
	if code := runInitCode(t, "myapp", "--reinit"); code != -1 {
		t.Fatalf("init myapp --reinit exited %d", code)
	}
	if _, err := os.Stat(filepath.Join("myapp", "AGENTS.md")); err != nil {
		t.Errorf("reinit wrote no AGENTS.md: %v", err)
	}
}

// init . over a module that already exists wrote imports for
// local/<dir>/entities while go.mod kept its own module line, and the
// app did not build.
func TestInitDotUsesExistingModule(t *testing.T) {
	inPlaceDir(t, "foo")
	if err := os.WriteFile("go.mod", []byte("module github.com/me/foo\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runInitCode(t, "."); code != -1 {
		t.Fatalf("init . exited %d", code)
	}
	main, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), `"github.com/me/foo/entities"`) || strings.Contains(string(main), "local/foo") {
		t.Fatalf("main.go does not import the existing module's entities:\n%s", main)
	}
	data, _ := os.ReadFile("go.mod")
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mod.Module.Mod.Path != "github.com/me/foo" {
		t.Fatalf("module = %q, want github.com/me/foo", mod.Module.Mod.Path)
	}
}

// A --module that disagrees with the existing go.mod is refused before
// any file is written.
func TestInitDotRefusesOtherModule(t *testing.T) {
	inPlaceDir(t, "foo")
	if err := os.WriteFile("go.mod", []byte("module github.com/me/foo\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runInitCode(t, ".", "--module=github.com/me/bar"); code != 1 {
		t.Fatalf("init . --module=<other> exited %d, want 1", code)
	}
	if _, err := os.Stat("main.go"); err == nil {
		t.Error("a refused init . still wrote main.go")
	}
}

// Reinit over an AGENTS.md whose markers were edited away advised
// `gofastr agents init --force`, a flag agents init does not have. The
// advice names reinit's own --force, and that run replaces the file.
func TestReinitBrokenAgentsMDAdvice(t *testing.T) {
	inPlaceDir(t, "work")
	if err := os.Mkdir("myapp", 0o755); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join("myapp", "AGENTS.md")
	if err := os.WriteFile(agents, []byte("# my notes, no markers\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunInit([]string{"myapp", "--reinit"}) })
	})
	if code != 1 {
		t.Fatalf("reinit over broken markers exited %d, want 1", code)
	}
	if strings.Contains(out, "agents init --force") || !strings.Contains(out, "gofastr init myapp --reinit --force") {
		t.Fatalf("reinit advice does not name a command that works:\n%s", out)
	}
	if code := runInitCode(t, "myapp", "--reinit", "--force"); code != -1 {
		t.Fatalf("reinit --force exited %d", code)
	}
	if got, _ := os.ReadFile(agents); strings.Contains(string(got), "no markers") {
		t.Fatalf("reinit --force kept the broken AGENTS.md:\n%s", got)
	}
}
