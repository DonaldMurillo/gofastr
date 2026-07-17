package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// addRepoRoot is resolved at package init (before any test chdirs), so
// writeAddGoMod can find the repo root regardless of cwd.
var addRepoRoot = func() string {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}
	return root
}()

// addTestBlueprint is a minimal valid blueprint with 2 entities and a module,
// used as the base project for additive-generation tests.
func addTestBlueprint(module string) string {
	return fmt.Sprintf(`app:
  name: AddTest
  module: %s
  db:
    driver: sqlite
    url: file:test.db
entities:
  - name: posts
    fields:
      - name: title
        type: string
        required: true
  - name: tags
    fields:
      - name: label
        type: string
`, module)
}

// addFragmentBlueprint is a partial yml with one NEW entity not in the base.
func addFragmentBlueprint() string {
	return `entities:
  - name: comments
    fields:
      - name: body
        type: string
        required: true
`
}

// addRedeclareFragment re-declares an entity that the base already has.
func addRedeclareFragment() string {
	return `entities:
  - name: posts
    fields:
      - name: title
        type: string
        required: true
`
}

// addSetup creates a temp dir with go.mod, chdirs into it, and writes a
// blueprint yml. Returns (dir, bpPath).
func addSetup(t *testing.T, yml string) (dir, bpPath string) {
	t.Helper()
	dir = t.TempDir()
	covT_chdir(t, dir)
	writeAddGoMod(t, dir, "example.com/addtest")
	bpPath = filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(bpPath, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, bpPath
}

func writeAddGoMod(t *testing.T, dir, module string) {
	t.Helper()
	goVersion, err := repoGoVersion(addRepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("module %s\n\ngo %s\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => %s\n", module, goVersion, addRepoRoot)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(addRepoRoot, dir); err != nil {
		t.Fatal(err)
	}
}

// readFile reads a file under dir, failing the test if it doesn't exist.
func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// fileExists reports whether a path exists under dir.
func fileExists(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, rel))
	return err == nil
}

// ---- Test 1: flag parsing + validation ----

func TestParseAddFlag(t *testing.T) {
	opts := parseGenerateOptions([]string{"--add", "--from=f.yml"})
	if !opts.add {
		t.Fatalf("--add not parsed: %#v", opts)
	}
}

func TestAddAndForceMutuallyExclusive(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	os.WriteFile(bp, []byte("entities:\n  - name: x\n    fields:\n      - name: a\n        type: string\n"), 0o644)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateProject([]string{"--from=" + bp, "--add", "--force"})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 for --add --force, got %d", code)
	}
}

func TestAddRequiresFrom(t *testing.T) {
	covT_chdir(t, t.TempDir())
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateProject([]string{"--add"})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 for --add without --from, got %d", code)
	}
}

// TestOverwriteErrorNamesAddPath pins issue #77 item 5: the refuse-to-
// overwrite conflict error must name `generate --add` so a user who hits
// it at the moment of need discovers the additive path without reading docs.
func TestOverwriteErrorNamesAddPath(t *testing.T) {
	_, bp := addSetup(t, addTestBlueprint("example.com/addtest"))
	// First generate succeeds (writes main.go, app.go, …).
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	// Second plain generate (no --add, no --force) must refuse and name --add.
	// Capture stdout panic-safely: covT_capStdout's read-after-fn is skipped
	// when fn panics via osExit, so redirect to a temp file ourselves and read
	// it in a recover'd defer.
	old := os.Stdout
	f, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = f
	var out string
	code := covT_capExit(t, func() {
		defer func() {
			_, _ = f.Seek(0, io.SeekStart)
			b, _ := io.ReadAll(f)
			out = string(b)
		}()
		generateFromBlueprint(generateOptions{from: bp})
	})
	os.Stdout = old
	if code != 1 {
		t.Fatalf("want exit 1 on conflict, got %d", code)
	}
	for _, want := range []string{"one-shot", "generate --add", "--from=<blueprint>"} {
		if !strings.Contains(out, want) {
			t.Errorf("conflict error missing %q:\n%s", want, out)
		}
	}
}

// ---- Test 2: --add into empty dir ≡ plain generate ----

func TestAddEmptyDirMatchesPlainGenerate(t *testing.T) {
	base := addTestBlueprint("example.com/addtest")
	// Plain generate into dir1.
	dir1, bp1 := addSetup(t, base)
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp1}) })
	// Add into dir2 (same blueprint, empty dir).
	dir2 := t.TempDir()
	covT_chdir(t, dir2)
	writeAddGoMod(t, dir2, "example.com/addtest")
	bp2 := filepath.Join(dir2, "gofastr.yml")
	os.WriteFile(bp2, []byte(base), 0o644)
	covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: bp2, add: true})
	})
	// Compare every file under both dirs.
	var files1, files2 []string
	filepath.Walk(dir1, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.HasSuffix(p, ".mod") && !strings.HasSuffix(p, ".sum") {
			rel, _ := filepath.Rel(dir1, p)
			files1 = append(files1, rel)
		}
		return nil
	})
	filepath.Walk(dir2, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.HasSuffix(p, ".mod") && !strings.HasSuffix(p, ".sum") {
			rel, _ := filepath.Rel(dir2, p)
			files2 = append(files2, rel)
		}
		return nil
	})
	if len(files1) != len(files2) {
		t.Fatalf("file count differs: plain=%d add=%d\nplain=%v\nadd=%v", len(files1), len(files2), files1, files2)
	}
	for _, rel := range files1 {
		a := readFile(t, dir1, rel)
		b := readFile(t, dir2, rel)
		if a != b {
			t.Errorf("file %s differs between plain and add generate", rel)
		}
	}
}

// ---- Test 3: add fragment → order continuity + existing files untouched ----

func TestAddFragmentOrderContinuity(t *testing.T) {
	dir, bp := addSetup(t, addTestBlueprint("example.com/addtest"))
	// Generate base project (2 entities: posts order=0, tags order=1).
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	// Snapshot existing files before add.
	mainBefore := readFile(t, dir, "main.go")
	appBefore := readFile(t, dir, "app.go")
	registerBefore := readFile(t, dir, "entities/register.go")
	clientBefore := readFile(t, dir, "entities/client/client.go")
	// Add a fragment yml (1 new entity: comments) — NOT tied to the base yml.
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addFragmentBlueprint()), 0o644)
	covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true})
	})
	// New entity file must exist with order 2 (base had 0,1).
	commentsPath := filepath.Join(dir, "entities", "comments.go")
	comments, err := os.ReadFile(commentsPath)
	if err != nil {
		t.Fatalf("comments.go not written: %v", err)
	}
	if !strings.Contains(string(comments), "registrar{order: 2") {
		t.Errorf("comments.go missing order 2:\n%s", comments)
	}
	// Existing files must be byte-identical.
	if readFile(t, dir, "main.go") != mainBefore {
		t.Error("main.go changed during --add")
	}
	if readFile(t, dir, "app.go") != appBefore {
		t.Error("app.go changed during --add")
	}
	if readFile(t, dir, "entities/register.go") != registerBefore {
		t.Error("register.go changed during --add")
	}
	if readFile(t, dir, "entities/client/client.go") != clientBefore {
		t.Error("client.go changed during --add")
	}
}

// ---- Test 4: fragment re-declares existing entity → skipped ----

func TestAddRedeclaredEntitySkipped(t *testing.T) {
	dir, bp := addSetup(t, addTestBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	postsBefore := readFile(t, dir, "entities/posts.go")
	// Add a fragment that re-declares the existing "posts" entity.
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addRedeclareFragment()), 0o644)
	out := covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true})
	})
	// Existing file must be untouched.
	if readFile(t, dir, "entities/posts.go") != postsBefore {
		t.Error("posts.go was overwritten during --add (should be skipped)")
	}
	// Output should mention skipping.
	if !strings.Contains(out, "skipped") {
		t.Errorf("expected skip mention in output:\n%s", out)
	}
}

// ---- Test 5: union compiles ----

func TestAddUnionCompiles(t *testing.T) {
	dir, bp := addSetup(t, addTestBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addFragmentBlueprint()), 0o644)
	covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true})
	})
	// The generated union must compile.
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("union did not build: %v\n%s", err, out)
	}
}

// ---- Test 6: pack round-trip on the union ----

func TestAddUnionPackRoundTrip(t *testing.T) {
	dir, bp := addSetup(t, addTestBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addFragmentBlueprint()), 0o644)
	covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true})
	})
	got, err := packReadEntities(dir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	want := []string{"posts", "tags", "comments"}
	if len(got) != len(want) {
		t.Fatalf("packed %d entities, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("packed entity[%d] = %q, want %q", i, got[i].Name, w)
		}
	}
}

// ---- Test 7: --add --dry-run --json includes skipped_existing ----

func TestAddDryRunJSONSkippedExisting(t *testing.T) {
	dir, bp := addSetup(t, addTestBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addFragmentBlueprint()), 0o644)
	// Add dry-run JSON: should include skipped_existing.
	addJSON := covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true, dryRun: true, json: true})
	})
	var addShape struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
		SkippedExisting []string `json:"skipped_existing"`
	}
	if err := json.Unmarshal([]byte(addJSON), &addShape); err != nil {
		t.Fatalf("add JSON parse: %v\n%s", err, addJSON)
	}
	if len(addShape.SkippedExisting) == 0 {
		t.Errorf("expected skipped_existing in add JSON:\n%s", addJSON)
	}
	if len(addShape.Files) == 0 {
		t.Errorf("expected written files in add JSON:\n%s", addJSON)
	}
	// Non-add JSON: should NOT have skipped_existing key.
	plainJSON := covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: bp, dryRun: true, json: true, force: true})
	})
	if strings.Contains(plainJSON, "skipped_existing") {
		t.Errorf("non-add JSON must not contain skipped_existing:\n%s", plainJSON)
	}
}

// ---- Test 8: --add refuses the pre-0.15 aggregated entities layout ----

// legacyRegisterGo is the pre-0.15 aggregated entities/register.go: an
// inline RegisterAll holding the app.Entity calls directly. A per-entity
// file added next to it would reference the registrar seam that doesn't
// exist there, producing a project that no longer compiles.
const legacyRegisterGo = `package entities

import (
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

func RegisterAll(app *framework.App) {
	app.Entity("posts", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})
}
`

func TestAddRefusesLegacyLayout(t *testing.T) {
	dir, _ := addSetup(t, addTestBlueprint("example.com/addtest"))
	if err := os.MkdirAll(filepath.Join(dir, "entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	regPath := filepath.Join(dir, "entities", "register.go")
	if err := os.WriteFile(regPath, []byte(legacyRegisterGo), 0o644); err != nil {
		t.Fatal(err)
	}
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addFragmentBlueprint()), 0o644)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateFromBlueprint(generateOptions{from: frag, add: true})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 for --add into legacy layout, got %d", code)
	}
	if fileExists(dir, "entities/comments.go") {
		t.Error("comments.go written into a legacy layout — output would not compile")
	}
}

// ---- Test 9: client.go warn fires when client exists ----

func TestAddClientGoWarnWhenExists(t *testing.T) {
	dir, bp := addSetup(t, addTestBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addFragmentBlueprint()), 0o644)
	// Capture stderr to check for the warning.
	out := covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true})
	})
	if !strings.Contains(out, "client.go") || !strings.Contains(out, "not updated") {
		t.Errorf("expected client.go stale warning in output:\n%s", out)
	}
}

// ---- Test 10: --add a screen fragment into an existing project ----

// addScreenBaseBlueprint is a base project with an entity + a standalone
// screen, so the per-screen layout (seam + screen_about.go) already exists.
func addScreenBaseBlueprint(module string) string {
	return fmt.Sprintf(`app:
  name: AddScreen
  module: %s
  db:
    driver: sqlite
    url: file:test.db
entities:
  - name: posts
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: about
    route: /about
    title: About
`, module)
}

// addScreenFragment is a partial yml with one NEW standalone screen.
func addScreenFragment() string {
	return `screens:
  - name: contact
    route: /contact
    title: Contact
`
}

func TestAddScreenFragmentWritesPerScreenFile(t *testing.T) {
	dir, bp := addSetup(t, addScreenBaseBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	appBefore := readFile(t, dir, "app.go")
	seamBefore := readFile(t, dir, "screens_register.go")
	aboutBefore := readFile(t, dir, "screen_about.go")
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addScreenFragment()), 0o644)
	covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true})
	})
	// New standalone screen file written, continuing after the base's order 0.
	contact := readFile(t, dir, "screen_contact.go")
	if !strings.Contains(contact, "screenRegistrar{order: 1, fn: mountContactScreen}") {
		t.Errorf("contact screen must continue order after the base (order 1):\n%s", contact)
	}
	// Existing files byte-identical — additive never edits them.
	if readFile(t, dir, "app.go") != appBefore {
		t.Error("app.go changed during --add of a screen")
	}
	if readFile(t, dir, "screens_register.go") != seamBefore {
		t.Error("screens_register.go changed during --add of a screen")
	}
	if readFile(t, dir, "screen_about.go") != aboutBefore {
		t.Error("screen_about.go changed during --add of a screen")
	}
}

// ---- Test 11: --add an entity fragment emits its crud screen file ----

func addEntityWithScreenFragment() string {
	return `entities:
  - name: reviews
    fields:
      - name: body
        type: string
        required: true
screens:
  - name: reviews
    route: /reviews
    title: Reviews
    body:
      - kind: entity_list
        entity: reviews
        fields: [body]
        create: true
`
}

func TestAddEntityFragmentEmitsCrudScreenFile(t *testing.T) {
	dir, bp := addSetup(t, addScreenBaseBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addEntityWithScreenFragment()), 0o644)
	covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true})
	})
	// The reviews entity + its per-entity crud screen file are both written.
	reviewsCrud := readFile(t, dir, "screen_reviews_crud.go")
	if !strings.Contains(reviewsCrud, `appResources["reviews"] = ResourceConfig{`) {
		t.Errorf("screen_reviews_crud.go must wire the reviews resource:\n%s", reviewsCrud)
	}
	if !strings.Contains(reviewsCrud, "type ReviewsScreen struct") {
		t.Errorf("screen_reviews_crud.go must define the ReviewsScreen:\n%s", reviewsCrud)
	}
	if !fileExists(dir, "entities/reviews.go") {
		t.Error("entities/reviews.go not written")
	}
	// The union compiles — the added crud file + entity self-register cleanly.
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("union did not build after --add entity+screen: %v\n%s", err, out)
	}
}

// ---- Test 12: screens-only fragment may reference EXISTING project entities ----

// addScreenOverExistingEntityFragment references the base project's "posts"
// entity without redeclaring it — the flagship additive use case.
func addScreenOverExistingEntityFragment() string {
	return `screens:
  - name: archive
    route: /archive
    title: Archive
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
`
}

func TestAddScreenOverExistingEntity(t *testing.T) {
	dir, bp := addSetup(t, addScreenBaseBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addScreenOverExistingEntityFragment()), 0o644)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateFromBlueprint(generateOptions{from: frag, add: true})
		})
	})
	if code != -1 {
		t.Fatalf("screens fragment over an existing entity must validate against the project's entities, got exit %d", code)
	}
	if !fileExists(dir, "screen_archive.go") && !fileExists(dir, "screen_posts_crud.go") {
		t.Error("no screen file written for the archive screen")
	}
	// The union compiles.
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("union did not build: %v\n%s", err, out)
	}
}

// ---- Test 13: screen order continuity without any entities ----

// addScreensOnlyBase has screens but ZERO entities — screen offsets must not
// depend on the entity offset being non-zero.
func addScreensOnlyBase(module string) string {
	return fmt.Sprintf(`app:
  name: AddTest
  module: %s
  db:
    driver: sqlite
    url: file:test.db
screens:
  - name: about
    route: /about
    title: About
`, module)
}

func TestAddScreenOrderNoEntities(t *testing.T) {
	dir, bp := addSetup(t, addScreensOnlyBase("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addScreenFragment()), 0o644)
	covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true})
	})
	contact := readFile(t, dir, "screen_contact.go")
	if !strings.Contains(contact, "screenRegistrar{order: 1") {
		t.Errorf("contact must continue after about's order 0 even with no entities:\n%s", contact)
	}
}

// ---- Test 14: seams + call sites are ALWAYS emitted (additive-ready) ----

// A project generated without entities (or screens) must still carry the
// entity (or screen) seam AND its call site in the owned shell — otherwise a
// later `--add`/scaffold writes files that compile but never register/mount.

func TestEntitySeamAlwaysEmitted(t *testing.T) {
	dir, bp := addSetup(t, addScreensOnlyBase("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	if !fileExists(dir, "entities/register.go") {
		t.Error("entities/register.go seam missing from an entity-less project")
	}
	if !strings.Contains(readFile(t, dir, "main.go"), "entities.RegisterAll(fwApp)") {
		t.Error("main.go must call entities.RegisterAll even with no entities yet")
	}
	// The empty entities package + import must compile.
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("entity-less project did not build: %v\n%s", err, out)
	}
}

func TestScreenSeamAlwaysEmitted(t *testing.T) {
	dir, bp := addSetup(t, addTestBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	if !fileExists(dir, "screens_register.go") {
		t.Error("screens_register.go seam missing from a screen-less project")
	}
	if !strings.Contains(readFile(t, dir, "app.go"), "mountGenerated(fwApp, site, db)") {
		t.Error("app.go must call mountGenerated even with no screens yet")
	}
}

// ---- Test 15: --add warns when the owned call site has been stripped ----

func stripLine(t *testing.T, dir, rel, needle string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, needle) {
			continue
		}
		kept = append(kept, line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddWarnsMissingMountCall(t *testing.T) {
	dir, bp := addSetup(t, addTestBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	stripLine(t, dir, "app.go", "mountGenerated(fwApp, site, db)")
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addScreenFragment()), 0o644)
	out := covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true})
	})
	if !strings.Contains(out, "mountGenerated") {
		t.Errorf("expected a warn naming the missing mountGenerated call:\n%s", out)
	}
}

func TestAddWarnsMissingRegisterAll(t *testing.T) {
	dir, bp := addSetup(t, addScreensOnlyBase("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	stripLine(t, dir, "main.go", "entities.RegisterAll(fwApp)")
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addFragmentBlueprint()), 0o644)
	out := covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true})
	})
	if !strings.Contains(out, "entities.RegisterAll") {
		t.Errorf("expected a warn naming the missing entities.RegisterAll call:\n%s", out)
	}
}

// ---- Test 15b: --add screen whose route collides with an existing one ----

func addRouteCollisionFragment() string {
	// A different screen NAME but the SAME route as the base's "about".
	return `screens:
  - name: company
    route: /about
    title: Company
`
}

func TestAddScreenRouteCollisionRefused(t *testing.T) {
	dir, bp := addSetup(t, addScreenBaseBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addRouteCollisionFragment()), 0o644)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateFromBlueprint(generateOptions{from: frag, add: true})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 for a colliding screen route, got %d", code)
	}
	if fileExists(dir, "screen_company.go") {
		t.Error("screen_company.go written despite a route collision (would shadow /about)")
	}
}

// A fragment screen colliding with a SYNTHESIZED CRUD form route (/new,
// /{id}/edit) must also be refused — those routes are mounted by the
// generated crud screen file but dropped by packReadScreens, so the guard
// must scan the real Register calls, not the recovered authored screens.
func addSynthRouteBase(module string) string {
	return fmt.Sprintf(`app:
  name: SynthBase
  module: %s
  db:
    driver: sqlite
    url: file:test.db
entities:
  - name: posts
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: posts
    route: /posts
    title: Posts
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
        create: true
`, module)
}

func TestAddScreenCollidesWithSynthesizedRoute(t *testing.T) {
	dir, bp := addSetup(t, addSynthRouteBase("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	// The base's entity_list with create:true synthesizes a /posts/new form
	// route. A fragment authoring a DIFFERENT screen at /posts/new collides.
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(`screens:
  - name: custompostform
    route: /posts/new
    title: Custom
`), 0o644)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateFromBlueprint(generateOptions{from: frag, add: true})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 for collision with the synthesized /posts/new route, got %d", code)
	}
	if fileExists(dir, "screen_custompostform.go") {
		t.Error("screen written despite colliding with a synthesized form route")
	}
}

// Re-adding the SAME screen (same name + route) must be an idempotent skip,
// not a hard route-collision error — matching entity redeclaration semantics.
func TestAddSameScreenIsIdempotent(t *testing.T) {
	dir, bp := addSetup(t, addScreenBaseBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addScreenFragment()), 0o644)
	// First add: writes screen_contact.go.
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: frag, add: true}) })
	if !fileExists(dir, "screen_contact.go") {
		t.Fatal("first add did not write screen_contact.go")
	}
	before := readFile(t, dir, "screen_contact.go")
	// Second identical add: must NOT hard-error; the existing file is skipped.
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateFromBlueprint(generateOptions{from: frag, add: true})
		})
	})
	if code != -1 {
		t.Fatalf("re-adding the same screen should be an idempotent skip (exit 0), got exit %d", code)
	}
	if readFile(t, dir, "screen_contact.go") != before {
		t.Error("screen_contact.go rewritten on idempotent re-add")
	}
}

// ---- Test 15c: --add entity whose name is a case variant of an existing one ----

func addCaseVariantEntityFragment() string {
	// The base declares "posts"; this fragment re-declares it as "Posts".
	// Same file (entities/posts.go), same Go type — a redeclaration, not a
	// new entity. It must be reported as skipped, never silently dropped.
	return `entities:
  - name: Posts
    fields:
      - name: title
        type: string
        required: true
`
}

func TestAddEntityCaseVariantIsRedeclaration(t *testing.T) {
	dir, bp := addSetup(t, addScreenBaseBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	postsBefore := readFile(t, dir, "entities/posts.go")
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addCaseVariantEntityFragment()), 0o644)
	out := covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: frag, add: true})
	})
	// The existing file is untouched (not rewritten as "Posts").
	if readFile(t, dir, "entities/posts.go") != postsBefore {
		t.Error("entities/posts.go was rewritten by a case-variant redeclaration")
	}
	// It is reported as skipped, not silently dropped.
	if !strings.Contains(out, "posts.go") || !strings.Contains(out, "skipped") {
		t.Errorf("case-variant redeclaration should be reported skipped:\n%s", out)
	}
	// The entities dir holds exactly one entity .go file (posts.go) — no
	// second file for the case variant. (Can't assert on "Posts.go" directly:
	// macOS's case-insensitive FS aliases it to posts.go.)
	ents, _ := os.ReadDir(filepath.Join(dir, "entities"))
	goFiles := 0
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && e.Name() != "register.go" {
			goFiles++
		}
	}
	if goFiles != 1 {
		t.Errorf("want exactly 1 entity file after case-variant add, got %d", goFiles)
	}
	// The project must still compile — no second Posts type was emitted.
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("union did not build: %v\n%s", err, o)
	}
}

// ---- Test 16: --add refuses the pre-per-screen aggregated screens.go ----

// legacyAggregatedScreensGo is the old layout: all screen structs in one
// screens.go. A per-screen file added next to it self-registers against a
// seam that doesn't exist there, so the project stops compiling — refuse.
const legacyAggregatedScreensGo = `package main

import (
	"context"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type AboutScreen struct{}

func (s *AboutScreen) ScreenTitle() string        { return "About" }
func (s *AboutScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *AboutScreen) Render() render.HTML {
	return render.Tag("div", nil, nil)
}
`

func TestAddRefusesAggregatedScreensLayout(t *testing.T) {
	dir, bp := addSetup(t, addScreenBaseBlueprint("example.com/addtest"))
	covT_capStdout(t, func() { generateFromBlueprint(generateOptions{from: bp}) })
	// Simulate a legacy project: drop the per-screen files + seam, drop an
	// aggregated screens.go in their place.
	for _, rel := range []string{"screens_register.go", "screen_about.go"} {
		os.Remove(filepath.Join(dir, rel))
	}
	os.WriteFile(filepath.Join(dir, "screens.go"), []byte(legacyAggregatedScreensGo), 0o644)
	frag := filepath.Join(dir, "fragment.yml")
	os.WriteFile(frag, []byte(addScreenFragment()), 0o644)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateFromBlueprint(generateOptions{from: frag, add: true})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 for --add into aggregated screens.go layout, got %d", code)
	}
	if fileExists(dir, "screen_contact.go") {
		t.Error("screen_contact.go written into a legacy layout — output would not compile")
	}
}
