package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pins: a CLI-supplied string interpolated into scaffolded Go source never leaves its identifier or comment slot.
// Property: a CLI-supplied string interpolated into scaffolded Go source never
// leaves its identifier or comment slot. Every sibling scaffolding surface
// enforces this — generate entity|screen (TestScaffoldNameGuardedByValidate),
// blueprint hook handlers (blueprint_hooks_security_test.go
// TestHookHandlerMustDeriveIdentifier), generate cli
// (TestGenerateCLI_EntityNameMustDeriveIdentifier) — and `gofastr new handler`
// is the divergent surface: validateScaffoldName checks path shape only.
// Surfaces: cmd/gofastr/new.go::scaffoldHandler L153-191 — rawName into
// `func %s(...)` identifier position L180 plus file-name/comment slots
// L172/L179; method+path spliced raw into the `// %s handles %s %s.` comment
// L179.
// Finding (verified by execution): name 'x") {PWN()' is accepted and written
// at identifier position; a --path of '/x\nfunc init(){ println("PWN") }\n//'
// smuggles a top-level init() that compiled and ran in the probe.
// Fix direction: give scaffoldHandler the identifier guard the sibling
// surfaces carry (reject any name that is not a Go identifier, not just
// path-shaped names), and refuse a --path carrying newlines/control bytes
// before it reaches the comment slot (or render it inert).

func TestNewHandlerRedNameIdentifier(t *testing.T) {
	// Hostile-name leg: the name lands in `func %s(...)` (identifier
	// position) and in the file name + leading comment. All three are
	// Go-source slots, so a name that is not a bare Go identifier must be
	// refused — exactly as validateBlueprint refuses the same shape for
	// every generate-side sibling.
	for _, name := range []string{
		`x") {PWN()`,
		"2fa",
		"x`y",
	} {
		dir := t.TempDir()
		err := scaffoldHandler(dir, name, "POST", "/api/x", false)
		if err == nil {
			t.Errorf("SECURITY: [newhandler-scaffold-injection] scaffoldHandler accepted name %q: validateScaffoldName checks path shape only, so the name reaches `func %%s(...)` at identifier position in the scaffolded Go source. It must carry the isGoIdentifier guard the generate siblings (TestScaffoldNameGuardedByValidate, TestHookHandlerMustDeriveIdentifier) apply to the same slot.", name)
		}
		// Even if a future guard changes the error path, no file may
		// carry the hostile bytes: assert the write side too.
		entries, rerr := os.ReadDir(dir)
		if rerr != nil {
			t.Fatalf("setup broken: read scaffold dir: %v", rerr)
		}
		for _, e := range entries {
			data, ferr := os.ReadFile(filepath.Join(dir, e.Name()))
			if ferr != nil {
				t.Fatalf("setup broken: read %s: %v", e.Name(), ferr)
			}
			if strings.Contains(string(data), name) {
				t.Errorf("SECURITY: [newhandler-scaffold-injection] hostile name %q was written verbatim into %s — the name must never leave its identifier slot", name, e.Name())
			}
		}
	}

	// Control leg: the guard must not over-fire on the documented shape.
	// `gofastr new handler Ping --method POST --path /api/ping` scaffolds
	// a file that parses as Go.
	ctrl := t.TempDir()
	if err := scaffoldHandler(ctrl, "Ping", "POST", "/api/ping", false); err != nil {
		t.Fatalf("setup broken: scaffoldHandler(Ping) failed: %v — the guard must not over-fire on valid names", err)
	}
	data, err := os.ReadFile(filepath.Join(ctrl, "ping_handler.go"))
	if err != nil {
		t.Fatalf("setup broken: ping_handler.go not written: %v", err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "ping_handler.go", data, 0); err != nil {
		t.Fatalf("setup broken: scaffolded ping_handler.go does not parse: %v", err)
	}
}

func TestNewHandlerRedPathCommentInert(t *testing.T) {
	// Comment-slot leg: method+path are spliced raw into
	// `// %s handles %s %s.` — a newline in --path closes the comment and
	// the rest lands at top-level declaration position.
	dir := t.TempDir()
	hostilePath := "/x\nfunc init(){ println(\"PWN\") }\n//"
	err := scaffoldHandler(dir, "Foo", "GET", hostilePath, false)

	fooPath := filepath.Join(dir, "foo_handler.go")
	data, rerr := os.ReadFile(fooPath)
	if rerr != nil {
		if err == nil {
			t.Fatalf("setup broken: scaffoldHandler reported success but foo_handler.go is unreadable: %v", rerr)
		}
		// Refused and nothing written: that is an accepted outcome.
		return
	}
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, "foo_handler.go", data, 0)
	if perr != nil {
		t.Errorf("SECURITY: [newhandler-scaffold-injection] --path %q broke the scaffolded file out of its comment slot: the emitted foo_handler.go does not even parse (%v) — the path must be refused or rendered inert (no newlines/control bytes past the comment marker)", hostilePath, perr)
		return
	}
	if newHandlerRedTopLevelInit(f) {
		t.Errorf("SECURITY: [newhandler-scaffold-injection] --path %q smuggled a top-level init() into foo_handler.go: scaffoldHandler splices method+path raw into the `// %%s handles %%s %%s.` comment, so a newline lets a CLI string become compiled code instead of documentation. The path must be refused at the comment slot (newlines/control bytes) or rendered inert, and the smuggled decl must never be top-level.", hostilePath)
	}
}

// newHandlerRedTopLevelInit reports whether f declares a package-level
// init function (the probe's smuggled payload shape).
func newHandlerRedTopLevelInit(f *ast.File) bool {
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fd.Recv == nil && fd.Name.Name == "init" {
			return true
		}
	}
	return false
}
