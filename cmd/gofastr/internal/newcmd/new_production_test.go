package newcmd

import (
	"bytes"
	"os"
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
	want := `// Ping handler: scaffolded by gofastr new handler.
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
	// Route generation prints a snippet; assert the snippet text.
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
