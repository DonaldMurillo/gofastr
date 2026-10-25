package appgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

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

// generateBaseProject writes baseYml as an app spec, generates the full base
// project from it, and returns the temp dir + app spec path.
func generateBaseProject(t *testing.T, baseYml string) (dir, bp string) {
	t.Helper()
	dir, bp = clitest.AddSetup(t, baseYml)
	clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: bp}) })
	return dir, bp
}

// ---- Test 1: `generate entity posts` into a project → next order, untouched shell ----

func TestScaffoldEntityIntoProjectNextOrder(t *testing.T) {
	dir, _ := generateBaseProject(t, scaffoldEntityBase("example.com/addtest"))
	mainBefore := clitest.ReadFile(t, dir, "main.go")
	appBefore := clitest.ReadFile(t, dir, "app.go")
	registerBefore := clitest.ReadFile(t, dir, "entities/register.go")
	clitest.CovT_capStdout(t, func() { GenerateScaffoldEntity([]string{"posts"}) })
	posts := clitest.ReadFile(t, dir, "entities/posts.go")
	if !strings.Contains(posts, "registrar{order: 2") {
		t.Errorf("entities/posts.go must continue order after widgets(0)+gadgets(1):\n%s", posts)
	}
	if clitest.ReadFile(t, dir, "main.go") != mainBefore {
		t.Error("main.go changed during scaffold (additive must not touch it)")
	}
	if clitest.ReadFile(t, dir, "app.go") != appBefore {
		t.Error("app.go changed during scaffold (additive must not touch it)")
	}
	if clitest.ReadFile(t, dir, "entities/register.go") != registerBefore {
		t.Error("entities/register.go changed during scaffold")
	}
	clitest.VariantBuild(t)
	clitest.BuildGenerated(t, dir)
}

// ---- Test 2: `generate screen contact` into a project → next screen order ----

func TestScaffoldScreenIntoProjectNextOrder(t *testing.T) {
	dir, _ := generateBaseProject(t, clitest.AddScreenBaseAppSpec("example.com/addtest"))
	clitest.CovT_capStdout(t, func() { GenerateScaffoldScreen([]string{"contact"}) })
	contact := clitest.ReadFile(t, dir, "screen_contact.go")
	// about is the base's only screen at order 0; contact must continue at 1.
	if !strings.Contains(contact, "screenRegistrar{order: 1, fn: mountContactScreen}") {
		t.Errorf("screen_contact.go must continue order after the base:\n%s", contact)
	}
	clitest.VariantBuild(t)
	clitest.BuildGenerated(t, dir)
}

// ---- Test 3: scaffold into an EMPTY dir (with go.mod) emits the app shell ----

func TestScaffoldEntityIntoEmptyDirEmitsShell(t *testing.T) {
	dir := clitest.ScaffoldEmptyDir(t)
	clitest.CovT_capStdout(t, func() { GenerateScaffoldEntity([]string{"posts"}) })
	if !clitest.FileExists(dir, "main.go") {
		t.Error("main.go not emitted scaffolding into an empty dir")
	}
	if !clitest.FileExists(dir, "entities/posts.go") {
		t.Error("entities/posts.go not emitted scaffolding into an empty dir")
	}
	clitest.VariantBuild(t)
	clitest.BuildGenerated(t, dir)
}

// ---- Test 4: name collision → exit 0, file skipped and unchanged ----

func TestScaffoldEntityCollisionSkipped(t *testing.T) {
	dir, _ := generateBaseProject(t, addTestAppSpec("example.com/addtest"))
	postsBefore := clitest.ReadFile(t, dir, "entities/posts.go")
	out := clitest.CovT_capStdout(t, func() {
		GenerateScaffoldEntity([]string{"posts"})
	})
	if clitest.ReadFile(t, dir, "entities/posts.go") != postsBefore {
		t.Error("entities/posts.go was overwritten by a re-scaffold (must be skipped)")
	}
	if !strings.Contains(out, "skipped") {
		t.Errorf("expected a skip mention on name collision:\n%s", out)
	}
}

// ---- Test 6: `--json` carries written files + skipped_existing ----

func TestScaffoldEntityJSONShape(t *testing.T) {
	dir, _ := generateBaseProject(t, addTestAppSpec("example.com/addtest"))
	js := clitest.CovT_capStdout(t, func() {
		GenerateScaffoldEntity([]string{"comments", "--json"})
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
	if !clitest.FileExists(dir, "entities/comments.go") {
		t.Errorf("entities/comments.go not written")
	}
}

// ---- Test 7: legacy aggregated entities layout → refused ----

func TestScaffoldEntityRefusesLegacyLayout(t *testing.T) {
	dir, _ := clitest.AddSetup(t, addTestAppSpec("example.com/addtest"))
	if err := os.MkdirAll(filepath.Join(dir, "entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "entities", "register.go"), []byte(legacyRegisterGo), 0o644); err != nil {
		t.Fatal(err)
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			GenerateScaffoldEntity([]string{"comments"})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 scaffolding into a legacy layout, got %d", code)
	}
	if clitest.FileExists(dir, "entities/comments.go") {
		t.Error("entities/comments.go written into a legacy layout — output would not compile")
	}
}
