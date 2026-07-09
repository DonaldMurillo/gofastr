package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---- Test 8: `generate entity <name>` no longer prints the removal tombstone ----

func TestScaffoldEntityNoLongerTombstone(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	writeAddGoMod(t, dir, "example.com/scaffold")
	var out string
	code := covT_capExit(t, func() {
		out = covT_capStdout(t, func() {
			runGenerate([]string{"entity", "posts"})
		})
	})
	if strings.Contains(out, "has been removed") {
		t.Fatalf("`generate entity posts` still prints the removal tombstone:\n%s", out)
	}
	if code != -1 {
		t.Fatalf("expected no osExit for `generate entity posts`, got code %d", code)
	}
	if !fileExists(dir, "entities/posts.go") {
		t.Errorf("entities/posts.go not written:\n%s", out)
	}
}

// ---- Test 5: missing name / extra args → exit 1 with usage ----

func TestScaffoldEntityMissingNameUsage(t *testing.T) {
	covT_chdir(t, t.TempDir())
	// parseScaffoldArgs prints usage and returns ok=false (no osExit), so the
	// guidance text is capturable.
	var ok bool
	out := covT_capStdout(t, func() {
		_, _, ok = parseScaffoldArgs([]string{}, "entity")
	})
	if ok {
		t.Fatal("expected ok=false for `generate entity` with no name")
	}
	if !strings.Contains(out, "name") || !strings.Contains(out, "Usage") {
		t.Errorf("expected usage guidance for missing name:\n%s", out)
	}
	// The dispatch path must exit non-zero on the same input.
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			runGenerate([]string{"entity"})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 for `generate entity` with no name, got %d", code)
	}
}

func TestScaffoldEntityExtraArgsUsage(t *testing.T) {
	var ok bool
	out := covT_capStdout(t, func() {
		_, _, ok = parseScaffoldArgs([]string{"a", "b"}, "entity")
	})
	if ok {
		t.Fatal("expected ok=false for `generate entity a b` (extra positional)")
	}
	if !strings.Contains(out, "exactly one name") {
		t.Errorf("expected 'exactly one name' guidance:\n%s", out)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			runGenerate([]string{"entity", "a", "b"})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 for `generate entity a b` (extra arg), got %d", code)
	}
}

// buildGenerated runs `go build ./...` in dir, failing the test on error.
func buildGenerated(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated project did not build: %v\n%s", err, out)
	}
}

// scaffoldEmptyDir sets up a temp dir with a go.mod (no blueprint) and chdirs
// into it — the starting point for scaffolding into an empty project.
func scaffoldEmptyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	covT_chdir(t, dir)
	writeAddGoMod(t, dir, "example.com/scaffold")
	return dir
}

// scaffoldEntityBase is a base project with two entities (orders 0, 1),
// neither named "posts", so `generate entity posts` adds a fresh entity at
// order 2 and the next-order assertion is unambiguous.
func scaffoldEntityBase(module string) string {
	return fmt.Sprintf(`app:
  name: ScaffoldBase
  module: %s
  db:
    driver: sqlite
    url: file:test.db
entities:
  - name: widgets
    fields:
      - name: label
        type: string
        required: true
  - name: gadgets
    fields:
      - name: title
        type: string
`, module)
}

// generateBaseProject writes baseYml as a blueprint, generates the full base
// project from it, and returns the temp dir + blueprint path.
func generateBaseProject(t *testing.T, baseYml string) (dir, bp string) {
	t.Helper()
	dir, bp = addSetup(t, baseYml)
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	return dir, bp
}

// ---- Test 1: `generate entity posts` into a project → next order, untouched shell ----

func TestScaffoldEntityIntoProjectNextOrder(t *testing.T) {
	dir, _ := generateBaseProject(t, scaffoldEntityBase("example.com/addtest"))
	mainBefore := readFile(t, dir, "main.go")
	appBefore := readFile(t, dir, "app.go")
	registerBefore := readFile(t, dir, "entities/register.go")
	covT_capStdout(t, func() { generateScaffoldEntity([]string{"posts"}) })
	posts := readFile(t, dir, "entities/posts.go")
	if !strings.Contains(posts, "registrar{order: 2") {
		t.Errorf("entities/posts.go must continue order after widgets(0)+gadgets(1):\n%s", posts)
	}
	if readFile(t, dir, "main.go") != mainBefore {
		t.Error("main.go changed during scaffold (additive must not touch it)")
	}
	if readFile(t, dir, "app.go") != appBefore {
		t.Error("app.go changed during scaffold (additive must not touch it)")
	}
	if readFile(t, dir, "entities/register.go") != registerBefore {
		t.Error("entities/register.go changed during scaffold")
	}
	buildGenerated(t, dir)
}

// ---- Test 2: `generate screen contact` into a project → next screen order ----

func TestScaffoldScreenIntoProjectNextOrder(t *testing.T) {
	dir, _ := generateBaseProject(t, addScreenBaseBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateScaffoldScreen([]string{"contact"}) })
	contact := readFile(t, dir, "screen_contact.go")
	// about is the base's only screen at order 0; contact must continue at 1.
	if !strings.Contains(contact, "screenRegistrar{order: 1, fn: mountContactScreen}") {
		t.Errorf("screen_contact.go must continue order after the base:\n%s", contact)
	}
	buildGenerated(t, dir)
}

// ---- Test 3: scaffold into an EMPTY dir (with go.mod) emits the app shell ----

func TestScaffoldEntityIntoEmptyDirEmitsShell(t *testing.T) {
	dir := scaffoldEmptyDir(t)
	covT_capStdout(t, func() { generateScaffoldEntity([]string{"posts"}) })
	if !fileExists(dir, "main.go") {
		t.Error("main.go not emitted scaffolding into an empty dir")
	}
	if !fileExists(dir, "entities/posts.go") {
		t.Error("entities/posts.go not emitted scaffolding into an empty dir")
	}
	buildGenerated(t, dir)
}

// ---- Test 4: name collision → exit 0, file skipped and unchanged ----

func TestScaffoldEntityCollisionSkipped(t *testing.T) {
	dir, _ := generateBaseProject(t, addTestBlueprint("example.com/addtest"))
	postsBefore := readFile(t, dir, "entities/posts.go")
	out := covT_capStdout(t, func() {
		generateScaffoldEntity([]string{"posts"})
	})
	if readFile(t, dir, "entities/posts.go") != postsBefore {
		t.Error("entities/posts.go was overwritten by a re-scaffold (must be skipped)")
	}
	if !strings.Contains(out, "skipped") {
		t.Errorf("expected a skip mention on name collision:\n%s", out)
	}
}

// ---- Test 6: `--json` carries written files + skipped_existing ----

func TestScaffoldEntityJSONShape(t *testing.T) {
	dir, _ := generateBaseProject(t, addTestBlueprint("example.com/addtest"))
	js := covT_capStdout(t, func() {
		generateScaffoldEntity([]string{"comments", "--json"})
	})
	var shape struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
		SkippedExisting []string `json:"skipped_existing"`
	}
	if err := json.Unmarshal([]byte(js), &shape); err != nil {
		t.Fatalf("scaffold --json did not parse: %v\n%s", err, js)
	}
	if len(shape.Files) == 0 {
		t.Errorf("expected written files (entities/comments.go) in scaffold --json:\n%s", js)
	}
	if len(shape.SkippedExisting) == 0 {
		t.Errorf("expected skipped_existing (the project shell) in scaffold --json:\n%s", js)
	}
	if !fileExists(dir, "entities/comments.go") {
		t.Errorf("entities/comments.go not written")
	}
}

// ---- Test 7: legacy aggregated entities layout → refused ----

func TestScaffoldEntityRefusesLegacyLayout(t *testing.T) {
	dir, _ := addSetup(t, addTestBlueprint("example.com/addtest"))
	if err := os.MkdirAll(filepath.Join(dir, "entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "entities", "register.go"), []byte(legacyRegisterGo), 0o644); err != nil {
		t.Fatal(err)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateScaffoldEntity([]string{"comments"})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 scaffolding into a legacy layout, got %d", code)
	}
	if fileExists(dir, "entities/comments.go") {
		t.Error("entities/comments.go written into a legacy layout — output would not compile")
	}
}
