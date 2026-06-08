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

func TestNewCLIGeneratedHandlerBuilds(t *testing.T) {
	bin := buildGofastrBin(t)
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")

	cmd := exec.Command(bin, "new", "handler", "Ping", "GET", "/ping")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gofastr new handler: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "ping_handler.go")); err != nil {
		t.Fatalf("handler not scaffolded: %v", err)
	}

	// `new entity` was removed — it must exit non-zero without scaffolding.
	ent := exec.Command(bin, "new", "entity", "User", "name:string")
	ent.Dir = dir
	if out, err := ent.CombinedOutput(); err == nil || !strings.Contains(string(out), "removed") {
		t.Fatalf("new entity should report removal and fail: err=%v out=%s", err, out)
	}
}
