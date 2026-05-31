package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ----- (a) Golden tests -----

func TestNewEntityGolden(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldEntity(dir, "User", []string{"name:string", "email:string:unique"}, false); err != nil {
		t.Fatalf("scaffoldEntity: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "entities", "users.json"))
	if err != nil {
		t.Fatalf("read generated: %v", err)
	}
	want := `{
  "name": "User",
  "table": "users",
  "fields": [
    {"name": "name", "type": "string"},
    {"name": "email", "type": "string", "unique": true}
  ]
}`
	if string(got) != want {
		t.Fatalf("entity golden mismatch.\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestNewHandlerGolden(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldHandler(dir, "Ping", "POST", "/api/ping", false); err != nil {
		t.Fatalf("scaffoldHandler: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "ping_handler.go"))
	if err != nil {
		t.Fatalf("read generated: %v", err)
	}
	want := `// Ping handler — scaffolded by gofastr new handler.
package main

import (
    "net/http"
)

// Ping handles POST /api/ping.
func Ping(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("{\"ok\": true}"))
}
`
	if string(got) != want {
		t.Fatalf("handler golden mismatch.\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestNewRouteGolden(t *testing.T) {
	// Route generation prints a snippet — assert the snippet text.
	got := routeSnippet("GET", "/items", "ItemHandler")
	want := `app.Router().Handle("GET", "/items", ItemHandler)`
	if got != want {
		t.Fatalf("route snippet mismatch.\nwant: %s\ngot:  %s", want, got)
	}
}

// ----- (b) Idempotency -----

func TestNewEntityIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldEntity(dir, "User", []string{"name:string"}, false); err != nil {
		t.Fatalf("first scaffoldEntity: %v", err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "entities", "users.json"))

	err := scaffoldEntity(dir, "User", []string{"name:string", "evil:string"}, false)
	if err == nil {
		t.Fatal("expected error on duplicate scaffold, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got: %v", err)
	}

	second, _ := os.ReadFile(filepath.Join(dir, "entities", "users.json"))
	if !bytes.Equal(first, second) {
		t.Fatal("file was clobbered despite duplicate-detection error")
	}
}

func TestNewHandlerIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldHandler(dir, "Ping", "GET", "/p", false); err != nil {
		t.Fatalf("first scaffoldHandler: %v", err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "ping_handler.go"))

	err := scaffoldHandler(dir, "Ping", "POST", "/other", false)
	if err == nil {
		t.Fatal("expected error on duplicate handler, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists', got: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "ping_handler.go"))
	if !bytes.Equal(first, second) {
		t.Fatal("handler file was clobbered")
	}
}

// ----- (c) -overwrite -----

func TestNewEntityOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldEntity(dir, "User", []string{"a:string"}, false); err != nil {
		t.Fatalf("first: %v", err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "entities", "users.json"))

	if err := scaffoldEntity(dir, "User", []string{"b:string"}, true); err != nil {
		t.Fatalf("overwrite scaffoldEntity: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "entities", "users.json"))
	if bytes.Equal(first, second) {
		t.Fatal("overwrite did not change file")
	}
	if !strings.Contains(string(second), `"name": "b"`) {
		t.Fatalf("overwrite did not contain new field: %s", second)
	}
}

func TestNewHandlerOverwrite(t *testing.T) {
	dir := t.TempDir()
	scaffoldHandler(dir, "Ping", "GET", "/old", false)
	first, _ := os.ReadFile(filepath.Join(dir, "ping_handler.go"))
	if err := scaffoldHandler(dir, "Ping", "POST", "/new", true); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "ping_handler.go"))
	if bytes.Equal(first, second) {
		t.Fatal("overwrite did not change handler")
	}
	if !strings.Contains(string(second), "POST /new") {
		t.Fatalf("overwrite did not reflect new method/path: %s", second)
	}
}

// ----- (d) Help / usage exit codes -----

// Build the binary once and reuse for the help tests. Uses go build into
// a tempdir so we don't pollute the source tree.
func buildGofastrBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "gofastr")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}
	return out
}

func TestNewNoArgsExitsNonZero(t *testing.T) {
	bin := buildGofastrBin(t)
	cmd := exec.Command(bin, "new")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit on `new` with no resource, got 0; output: %s", out)
	}
	if !strings.Contains(strings.ToLower(string(out)), "usage") {
		t.Fatalf("expected usage in output, got: %s", out)
	}
}

func TestNewHelpExitsZero(t *testing.T) {
	bin := buildGofastrBin(t)
	cmd := exec.Command(bin, "new", "-h")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected zero exit on `new -h`, got %v; output: %s", err, out)
	}
	if !strings.Contains(strings.ToLower(string(out)), "usage") &&
		!strings.Contains(strings.ToLower(string(out)), "resources") {
		t.Fatalf("expected help text, got: %s", out)
	}
}

func TestNewCLIGeneratedHandlerAndEntityBuild(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildGofastrBin(t)
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/newcli\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")

	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gofastr %v: %v\n%s", args, err, out)
		}
		return out
	}
	run("new", "handler", "Ping", "GET", "/ping")
	run("new", "entity", "User", "name:string", "email:string:unique")

	entityPath := filepath.Join(dir, "entities", "users.json")
	first, err := os.ReadFile(entityPath)
	if err != nil {
		t.Fatalf("read entity: %v", err)
	}
	dup := exec.Command(bin, "new", "entity", "User", "other:string")
	dup.Dir = dir
	if out, err := dup.CombinedOutput(); err == nil || !strings.Contains(string(out), "already exists") {
		t.Fatalf("duplicate entity should fail without clobbering: err=%v out=%s", err, out)
	}
	second, err := os.ReadFile(entityPath)
	if err != nil {
		t.Fatalf("read entity after duplicate: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("duplicate entity command clobbered existing file")
	}

	gen := exec.Command(bin, "generate")
	gen.Dir = dir
	gen.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("gofastr generate after new entity: %v\n%s", err, out)
	}

	cmd := exec.Command("go", "test", "-mod=mod", ".", "./.gofastr/entities")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("new-generated output did not build: %v\n%s", err, out)
	}
}
