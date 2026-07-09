package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// TestRenderPerEntityFileLayout asserts the per-piece property: each entity
// lands in its own file, the file self-registers, and the seam stays clean.
func TestRenderPerEntityFileLayout(t *testing.T) {
	files, err := renderGeneratedProject([]framework.EntityDeclaration{
		{Name: "alpha", Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}}},
		{Name: "beta", Fields: []framework.FieldDeclaration{{Name: "qty", Type: "int"}}},
		{Name: "gamma", Fields: []framework.FieldDeclaration{{Name: "ok", Type: "bool"}}},
	})
	if err != nil {
		t.Fatalf("renderGeneratedProject: %v", err)
	}
	names := map[string]bool{}
	for _, f := range files {
		names[f.name] = true
	}
	for _, want := range []string{"register.go", "alpha.go", "beta.go", "gamma.go", "client/client.go"} {
		if !names[want] {
			t.Fatalf("missing generated file %q; got %#v", want, files)
		}
	}
	byName := map[string]string{}
	for _, f := range files {
		byName[f.name] = f.content
	}
	// Each entity file self-registers via init() with its declaration order.
	for i, ent := range []string{"alpha", "beta", "gamma"} {
		camel := toCamelCase(ent)
		content := byName[ent+".go"]
		for _, want := range []string{
			"func init() {",
			"func register" + camel + "(app *framework.App) {",
			fmt.Sprintf("registrar{order: %d, fn: register%s}", i, camel),
			`app.Entity("` + ent + `", framework.EntityConfig{`,
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s.go missing %q:\n%s", ent, want, content)
			}
		}
	}
	// The seam carries no entity name — adding an entity never edits it.
	for _, ent := range []string{`"alpha"`, `"beta"`, `"gamma"`} {
		if strings.Contains(byName["register.go"], ent) {
			t.Fatalf("register.go must not name entity %s:\n%s", ent, byName["register.go"])
		}
	}
}

// TestRenderRegisterSeamIdenticalForAnyEntityCount is the additive-file
// property: register.go is byte-identical whether the project declares one
// entity or many. Adding an entity is a new file, never an edit to the seam.
func TestRenderRegisterSeamIdenticalForAnyEntityCount(t *testing.T) {
	one, err := renderGeneratedProject([]framework.EntityDeclaration{
		{Name: "solo", Fields: []framework.FieldDeclaration{{Name: "n", Type: "string"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	many, err := renderGeneratedProject([]framework.EntityDeclaration{
		{Name: "alpha", Fields: []framework.FieldDeclaration{{Name: "n", Type: "string"}}},
		{Name: "beta", Fields: []framework.FieldDeclaration{{Name: "n", Type: "string"}}},
		{Name: "gamma", Fields: []framework.FieldDeclaration{{Name: "n", Type: "string"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var r1, r3 string
	for _, f := range one {
		if f.name == "register.go" {
			r1 = f.content
		}
	}
	for _, f := range many {
		if f.name == "register.go" {
			r3 = f.content
		}
	}
	if r1 == "" {
		t.Fatal("1-entity project missing register.go")
	}
	if !reflect.DeepEqual(r1, r3) {
		t.Fatalf("register.go differs between 1-entity and 3-entity projects")
	}
}

// TestRenderEntityFileNameCollisionGuard ensures an entity whose snake name
// collides with a fixed package file (register/shared/doc) is prefixed so it
// never shadows the seam or shared helpers.
func TestRenderEntityFileNameCollisionGuard(t *testing.T) {
	for _, n := range []string{"register", "shared", "doc"} {
		if got := entityFileName(n); got != "entity_"+n+".go" {
			t.Errorf("entityFileName(%q) = %q, want entity_%s.go", n, got, n)
		}
	}
	files, err := renderGeneratedProject([]framework.EntityDeclaration{
		{Name: "register", Fields: []framework.FieldDeclaration{{Name: "n", Type: "string"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.name == "register.go" && strings.Contains(f.content, `app.Entity("register"`) {
			t.Fatal("the seam register.go must not carry an entity registration")
		}
	}
	// The colliding entity still lands somewhere and self-registers.
	var entityReg string
	for _, f := range files {
		if f.name == "entity_register.go" {
			entityReg = f.content
		}
	}
	if entityReg == "" {
		t.Fatal("colliding entity did not produce entity_register.go")
	}
	if !strings.Contains(entityReg, `app.Entity("register", framework.EntityConfig{`) {
		t.Fatalf("entity_register.go missing its registration:\n%s", entityReg)
	}
}

// TestRenderPerEntityFilesCompile builds the generated entities package for a
// multi-entity project (with a relation + soft-delete + owner scope) to confirm
// the per-entity files compile and their init() self-registrations are valid.
func TestRenderPerEntityFilesCompile(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	crud := true
	files, err := renderGeneratedProject([]framework.EntityDeclaration{
		{Name: "posts", Table: "posts", CRUD: &crud, SoftDelete: true, Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string", Required: true},
			{Name: "author_id", Type: "relation", To: "users"},
		}, Relations: []framework.Relation{{Type: framework.RelManyToOne, Name: "author", Entity: "users", ForeignKey: "author_id"}}},
		{Name: "users", CRUD: &crud, OwnerField: "user_id", Fields: []framework.FieldDeclaration{{Name: "email", Type: "string", Unique: true}}},
	})
	if err != nil {
		t.Fatalf("renderGeneratedProject: %v", err)
	}
	for _, f := range files {
		full := filepath.Join(dir, "entities", f.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/perentity\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport _ \"example.com/perentity/entities\"\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated per-entity package did not build: %v\n%s", err, out)
	}
}

// TestPackRoundTripPerEntityFiles is the additive-generation + pack invariant:
// packing a freshly generated (per-entity) app recovers the exact declaration
// set in authored order — even when file-name (lexical) order differs from
// declaration order.
func TestPackRoundTripPerEntityFiles(t *testing.T) {
	crud := true
	decls := []framework.EntityDeclaration{
		{Name: "zebra", Table: "zebra", CRUD: &crud, Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}}},
		{Name: "alpha", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}, Relations: []framework.Relation{{Type: framework.RelManyToOne, Name: "z", Entity: "zebra", ForeignKey: "z_id"}}},
		{Name: "mango", CRUD: &crud, OwnerField: "user_id", Fields: []framework.FieldDeclaration{{Name: "qty", Type: "int"}}},
	}
	files, err := renderGeneratedProject(decls)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range files {
		full := filepath.Join(dir, "entities", f.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := packReadEntities(dir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("packed %d entities, want 3", len(got))
	}
	want := []string{"zebra", "alpha", "mango"}
	for i, w := range want {
		if got[i].Name != w {
			t.Fatalf("packed entity[%d] = %q, want %q (declaration order not recovered)", i, got[i].Name, w)
		}
	}
	if len(got[1].Relations) != 1 || got[1].Relations[0].Entity != "zebra" {
		t.Errorf("alpha relation lost in round-trip: %#v", got[1].Relations)
	}
	if got[2].OwnerField != "user_id" {
		t.Errorf("mango owner scope lost in round-trip: %q", got[2].OwnerField)
	}
}
