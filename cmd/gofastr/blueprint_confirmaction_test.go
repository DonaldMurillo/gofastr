package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
)

// softDeleteOnlyBlueprint is the edge case for import flags: an entity with
// soft_delete, no screens, no nav, no marketing. Before the ConfirmAction
// removal, blueprintHasSoftDelete alone forced the ui + widget imports into
// app.go (for the now-dead ConfirmAction mount). After removal those imports
// must NOT be emitted for this shape, or the generated package fails to build
// with "imported and not used".
func softDeleteOnlyBlueprint() Blueprint {
	return Blueprint{
		App: BlueprintApp{Name: "Demo", Module: "example.com/demo"},
		Entities: []framework.EntityDeclaration{{
			Name:  "posts",
			Scope: &fwentity.ScopeDeclaration{SoftDelete: true},
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string"},
			},
		}},
	}
}

// The dead ui.ConfirmAction mount (trigger discarded, RPCPath wrong on three
// axes, nothing referencing it) must not be emitted. The resource engine ships
// the working delete (framework/ui/resource/resource.go).
func TestSoftDeleteAppOmitsDeadConfirmAction(t *testing.T) {
	app := renderBlueprintApp(softDeleteOnlyBlueprint())
	if strings.Contains(app, "ConfirmAction") {
		t.Errorf("generated app.go still emits the dead ConfirmAction widget mount:\n%s", app)
	}
	if strings.Contains(app, "delete-") {
		t.Errorf("generated app.go still emits a delete-* ConfirmAction name:\n%s", app)
	}
}

// The minimal soft-delete-only shape must still render a compiling app.go: the
// ui/widget imports must be absent (nothing uses them), not merely unreferenced.
func TestSoftDeleteOnlyAppCompiles(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/demo\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	writeTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	bp := softDeleteOnlyBlueprint()
	bp.App.OutputDir = "gen"
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	for _, file := range files {
		full := filepath.Join(dir, "gen", file.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "build", "-mod=mod", "./gen/entities", "./gen")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("minimal soft-delete app did not build: %v\n%s", err, output)
	}
}
