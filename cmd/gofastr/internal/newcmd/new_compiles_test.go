package newcmd

import (
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `gofastr new handler type` wrote `func type(...)`, which does not parse;
// `main` and `init` redeclared the package's entry points, and a
// predeclared name (`string`, `len`) shadowed it for every other file.
// Each passed the identifier guard and broke the build.
func TestScaffoldHandlerRefusesNamesThatBreakTheBuild(t *testing.T) {
	for _, name := range []string{"type", "func", "range", "main", "init", "string", "len", "error", "_"} {
		dir := t.TempDir()
		if err := scaffoldHandler(dir, name, "GET", "/x", false); err == nil {
			t.Errorf("scaffoldHandler(%q) = nil, want a refusal", name)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("scaffoldHandler(%q) wrote %d file(s) before refusing", name, len(entries))
		}
	}
}

// A name the package already declares (HomeScreen in an init scaffold's
// screens.go) is a redeclaration.
func TestScaffoldHandlerRefusesDeclaredName(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "screens.go", "package main\n\nfunc HomeScreen() {}\n")
	if err := scaffoldHandler(dir, "HomeScreen", "GET", "/", false); err == nil || !strings.Contains(err.Error(), "already declares") {
		t.Fatalf("scaffoldHandler(HomeScreen) = %v, want an already-declares refusal", err)
	}
}

// Run from a non-main package directory (entities/), the handler was
// written as package main beside package entities: "found packages
// entities and main". It takes the directory's package, and the file is
// gofmt-clean.
func TestScaffoldHandlerUsesTheDirectorysPackage(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "entities.go", "package entities\n")
	writeGo(t, dir, "entities_test.go", "package entities_test\n")
	if err := scaffoldHandler(dir, "Orders", "GET", "/orders", false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "orders_handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "\npackage entities\n") {
		t.Errorf("handler does not take the directory's package:\n%s", got)
	}
	formatted, err := format.Source(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(formatted) != string(got) {
		t.Errorf("scaffolded handler is not gofmt-clean:\n%s", got)
	}

	empty := t.TempDir()
	if err := scaffoldHandler(empty, "Orders", "GET", "/orders", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(empty, "orders_handler.go")); !strings.Contains(string(got), "\npackage main\n") {
		t.Errorf("handler in a directory with no Go files is not package main:\n%s", got)
	}
}

func writeGo(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}
