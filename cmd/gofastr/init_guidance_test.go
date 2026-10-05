package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Bug: `gofastr init` scaffolds the sample posts entity with only
// Exposure{CRUD: true}, so the first documented curl answers 401, and
// until these tests, no scaffold-generated surface (entity file, printed
// next steps, AGENTS.md) named the escape hatches: `Public: true` for
// anonymous access, or battery/auth for a real login flow. Secure by
// default stays (issue #65); the scaffold must teach the way out.

func TestWriteEntitiesGoTeachesPublicEscapeHatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeEntitiesGo(dir)
	src, err := os.ReadFile(filepath.Join(dir, "entities", "entities.go"))
	if err != nil {
		t.Fatalf("read scaffolded entities.go: %v", err)
	}
	body := string(src)
	for _, want := range []string{
		"Public: true",      // the concrete opt-out, verbatim as it compiles
		"session",           // why the 401 happens
		"battery/auth",      // the production path
		"gofastr docs auth", // where the full story lives
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scaffolded entities.go missing %q — a newcomer's first CRUD call 401s with no path out:\n%s", want, body)
		}
	}
	// The guidance must sit on the Exposure block itself, not in a
	// comment blocks away that the user never connects to the 401.
	guide := strings.Index(body, "Public: true")
	exp := strings.Index(body, "Exposure:")
	if guide < 0 || exp < 0 {
		t.Fatalf("missing Exposure or guidance in:\n%s", body)
	}
	// Directly above the Exposure line = a small negative offset.
	if d := guide - exp; d > 0 || d < -300 {
		t.Errorf("'Public: true' guidance sits %d bytes from the Exposure block — put it directly above", d)
	}
}

func TestPrintInitNextStepsTeachesPublicEscapeHatch(t *testing.T) {
	out := covT_capStdout(t, func() { printInitNextSteps("myapp", false) })
	for _, want := range []string{"Public: true", "battery/auth"} {
		if !strings.Contains(out, want) {
			t.Errorf("init next steps missing %q:\n%s", want, out)
		}
	}
	// --no-entity scaffolds no posts entity: the note would point at a
	// file that was never generated.
	noEntity := covT_capStdout(t, func() { printInitNextSteps("myapp", true) })
	if strings.Contains(noEntity, "Public: true") {
		t.Errorf("--no-entity next steps point at /posts CRUD guidance, but no entity was scaffolded:\n%s", noEntity)
	}
}

// The CLAUDE.md an init app ships tells agents how to add an entity or
// a screen. Every scaffold command it advises must run against that
// same init output, and the hand-edit seams it names must exist there.
// It used to advise generate --add / entity / screen, which refuse the
// init layout, and their refusal advised `gofastr pack`, which fails
// on it too.
func TestInitAdviceWorksOnInitApp(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runInit([]string{"myapp"}) })
	root := filepath.Join(dir, "myapp")
	covT_chdir(t, root) // the scaffolds write into the working directory
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	guide := read("CLAUDE.md")
	for cmd, run := range map[string]func(){
		"gofastr generate entity": func() { generateScaffoldEntity([]string{"note"}) },
		"gofastr generate screen": func() { generateScaffoldScreen([]string{"about"}) },
		"gofastr generate --add":  func() { generateScaffoldScreen([]string{"contact"}) },
	} {
		if !strings.Contains(guide, cmd) {
			continue
		}
		var code int
		out := covT_capStdout(t, func() { code = covT_capExit(t, run) })
		if code > 0 {
			t.Errorf("CLAUDE.md advises %s, which refuses the init app (exit %d):\n%s", cmd, code, out)
		}
	}
	for _, want := range []string{"entities/entities.go", "site.Register"} {
		if !strings.Contains(guide, want) {
			t.Errorf("CLAUDE.md does not name the init seam %q", want)
		}
	}
	if !strings.Contains(read("entities/entities.go"), "func RegisterAll(") || !strings.Contains(read("main.go"), "site.Register(") {
		t.Fatal("the seams CLAUDE.md names are missing from the init output")
	}

	// The scaffolds' refusal on this layout names it and the way
	// forward, not `gofastr pack` (which cannot read an init app).
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { generateScaffoldScreen([]string{"about"}) })
	})
	if code != 1 || strings.Contains(out, "gofastr pack") || !strings.Contains(out, "gofastr init") {
		t.Errorf("refusal on an init app (exit %d) must name the init layout, not gofastr pack:\n%s", code, out)
	}
}

func TestBuildAgentsMDTeachesPublicEscapeHatch(t *testing.T) {
	// Coding agents read AGENTS.md first; without the one-liner they
	// dead-end on the same 401 a human newcomer does.
	for _, want := range []string{"Public: true", "battery/auth", "gofastr docs auth"} {
		mustContain(t, string(buildAgentsMD()), want)
	}
}

// init ignored *.db but not the -shm/-wal files SQLite writes beside it in WAL
// mode, so a first commit of the whole tree picked up a live database sidecar.
func TestInitIgnoresSQLiteSidecars(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runInit([]string{"myapp"}) })
	b, err := os.ReadFile(filepath.Join(dir, "myapp", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	for _, want := range []string{"bin/", "*.db", "*.db-shm", "*.db-wal"} {
		if !slices.Contains(lines, want) {
			t.Errorf("init .gitignore does not ignore %s; got:\n%s", want, b)
		}
	}
}
