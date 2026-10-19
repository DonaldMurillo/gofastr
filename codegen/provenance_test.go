package codegen

import (
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestStampRoundTrips(t *testing.T) {
	m := Marker{Generator: "app", Kind: KindScaffold, Version: "v0.92.0"}
	for path, body := range map[string]string{
		"app.go":         "// Package main is the app.\npackage main\n",
		"build.go":       "//go:build linux\n\npackage main\n",
		"client.js":      "export const x = 1;\n",
		"client.d.ts":    "export declare const x: number;\n",
		"go.mod":         "module example.com/app\n",
		"ui/a.style.css": ".a { color: red; }\n",
		"README.md":      "# App\n",
		"page.html":      "<!doctype html>\n<html></html>\n",
		"compose.yml":    "services: {}\n",
		".env":           "GOFASTR_SECRET=x\n",
		"run.sh":         "#!/bin/sh\necho hi\n",
		"Dockerfile":     "# syntax=docker/dockerfile:1\nFROM scratch\n",
		"Makefile":       "all:\n\techo hi\n",
		"schema.sql":     "SELECT 1;\n",
	} {
		stamped, err := Stamp(path, body, m)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		got, gotBody, ok := ReadMarker(path, stamped)
		if !ok {
			t.Fatalf("%s: no marker in\n%s", path, stamped)
		}
		if gotBody != body {
			t.Errorf("%s: body = %q, want %q", path, gotBody, body)
		}
		if got.Generator != "app" || got.Kind != KindScaffold || got.Version != "v0.92.0" || got.Sum != Sum(body) {
			t.Errorf("%s: marker = %#v", path, got)
		}
		if edited, ok := Edited(path, stamped); !ok || edited {
			t.Errorf("%s: fresh file edited=%v ok=%v", path, edited, ok)
		}
	}
}

// Lines that must stay first keep their place: a shebang, a Dockerfile
// parser directive, an HTML doctype.
func TestStampKeepsLeadingLines(t *testing.T) {
	m := Marker{Generator: "app", Kind: KindScaffold}
	for path, first := range map[string]string{
		"run.sh":     "#!/bin/sh",
		"Dockerfile": "# syntax=docker/dockerfile:1",
		"page.html":  "<!doctype html>",
	} {
		stamped, err := Stamp(path, first+"\nrest\n", m)
		if err != nil {
			t.Fatal(err)
		}
		if line, _, _ := strings.Cut(stamped, "\n"); line != first {
			t.Errorf("%s: first line = %q, want %q", path, line, first)
		}
	}
}

// The Go marker is a directive in its own comment group: gofmt leaves it
// where it is, and it never becomes the package doc comment.
func TestStampGoIsGofmtStable(t *testing.T) {
	src := "// Package main is the app.\npackage main\n"
	stamped, err := Stamp("main.go", src, Marker{Generator: "app", Kind: KindScaffold, Version: "v0.92.0"})
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := format.Source([]byte(stamped))
	if err != nil {
		t.Fatal(err)
	}
	if string(formatted) != stamped {
		t.Fatalf("gofmt moved the marker:\n%s", formatted)
	}
	if !strings.HasPrefix(stamped, "//gofastr:generated generator=app kind=scaffold version=v0.92.0 sum=") {
		t.Fatalf("stamped = %q", stamped)
	}
}

func TestEditedDetectsHandEdit(t *testing.T) {
	stamped, err := Stamp("app.go", "package main\n", Marker{Generator: "app", Kind: KindScaffold})
	if err != nil {
		t.Fatal(err)
	}
	edited, ok := Edited("app.go", stamped+"\nfunc f() {}\n")
	if !ok || !edited {
		t.Fatalf("edited=%v ok=%v", edited, ok)
	}
	if _, ok := Edited("app.go", "package main\n"); ok {
		t.Fatal("unmarked file reported a marker")
	}
}

func TestStampSkipsUncommentableFiles(t *testing.T) {
	for _, path := range []string{"package.json", "go.sum", "logo.png", "LICENSE"} {
		if Stampable(path) {
			t.Errorf("%s: stampable", path)
		}
		out, err := Stamp(path, "{}\n", Marker{Generator: "sdk", Kind: KindDerived})
		if err != nil || out != "{}\n" {
			t.Errorf("%s: Stamp = %q, %v", path, out, err)
		}
	}
}

// A value that could end the comment or break the line is refused, so a
// marker can never inject code.
func TestStampRefusesUnsafeValues(t *testing.T) {
	for _, m := range []Marker{
		{Generator: "", Kind: KindScaffold},
		{Generator: "app", Kind: "owned"},
		{Generator: "a b", Kind: KindScaffold},
		{Generator: "a*/", Kind: KindScaffold},
		{Generator: "app", Kind: KindScaffold, Version: "v1\npackage evil"},
		{Generator: "app", Kind: KindScaffold, Version: "-->"},
	} {
		if _, err := Stamp("a.css", "x\n", m); err == nil {
			t.Errorf("%#v: accepted", m)
		}
	}
}

func TestReadMarkerRejectsMalformed(t *testing.T) {
	for _, src := range []string{
		"//gofastr:generated kind=scaffold sum=0123456789abcdef\n\npackage main\n",
		"//gofastr:generated generator=app sum=0123456789abcdef\n\npackage main\n",
		"//gofastr:generated generator=app kind=scaffold\n\npackage main\n",
		"//gofastr:generated generator=app generator=cli kind=scaffold sum=0123456789abcdef\n\npackage main\n",
		"// gofastr:generated generator=app kind=scaffold sum=0123456789abcdef\n\npackage main\n",
	} {
		if m, _, ok := ReadMarker("a.go", src); ok {
			t.Errorf("accepted %q as %#v", src, m)
		}
	}
}

// A marker deep in the file is content, not provenance: only the header
// counts, so a string literal quoting one is never read as a marker.
func TestReadMarkerOnlyInHeader(t *testing.T) {
	src := "package main\n\n" + strings.Repeat("// line\n", 20) +
		"//gofastr:generated generator=app kind=scaffold sum=" + Sum("") + "\n"
	if _, _, ok := ReadMarker("a.go", src); ok {
		t.Fatal("read a marker below the header")
	}
}

func TestEditedOutputsFromManifest(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	files := NewFileSet()
	for _, p := range []string{"a.json", "b/c.go", "gone.txt"} {
		if err := files.Add(GeneratedFile{Path: p, Content: "x\n", Owner: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteFiles(files, WriteOptions{OutputRoot: "gen"}); err != nil {
		t.Fatal(err)
	}
	if edited, err := EditedOutputs("gen"); err != nil || len(edited) != 0 {
		t.Fatalf("fresh output: edited = %v, err = %v", edited, err)
	}
	if err := os.WriteFile(filepath.Join("gen", "b", "c.go"), []byte("y\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join("gen", "gone.txt")); err != nil {
		t.Fatal(err)
	}
	edited, err := EditedOutputs("gen")
	if err != nil || !slices.Equal(edited, []string{"b/c.go"}) {
		t.Fatalf("edited = %v, err = %v", edited, err)
	}
	if edited, err := EditedOutputs("missing"); err != nil || edited != nil {
		t.Fatalf("missing root: %v, %v", edited, err)
	}
}
