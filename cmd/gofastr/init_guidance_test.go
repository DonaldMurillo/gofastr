package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Bug: `gofastr init` scaffolds the sample posts entity with only
// Exposure{CRUD: true}, so the first documented curl answers 401 — and
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

func TestBuildAgentsMDTeachesPublicEscapeHatch(t *testing.T) {
	// Coding agents read AGENTS.md first; without the one-liner they
	// dead-end on the same 401 a human newcomer does.
	for _, want := range []string{"Public: true", "battery/auth", "gofastr docs auth"} {
		mustContain(t, string(buildAgentsMD()), want)
	}
}
