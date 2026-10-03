package contracts

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReadModulePathAndImportPathFor(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "go.mod", "module example.com/proj\n\ngo 1.21\n")
	if ReadModulePath(dir) != "example.com/proj" {
		t.Fatalf("module = %q", ReadModulePath(dir))
	}
	if ReadModulePath(t.TempDir()) != "" {
		t.Fatal("missing go.mod → empty")
	}
	if got := ImportPathFor("example.com/proj", dir, dir); got != "example.com/proj" {
		t.Fatalf("root import = %q", got)
	}
	sub := filepath.Join(dir, "pkg")
	if got := ImportPathFor("example.com/proj", dir, sub); got != "example.com/proj/pkg" {
		t.Fatalf("sub import = %q", got)
	}
}

// A go.mod may quote the module path and trail a comment; both forms
// are legal and must parse to the bare path.
func TestReadModulePathQuotedAndCommented(t *testing.T) {
	for _, tc := range []struct{ name, goMod, want string }{
		{"plain", "module example.com/plain\n\ngo 1.26\n", "example.com/plain"},
		{"quoted", "module \"example.com/q\"\n\ngo 1.26\n", "example.com/q"},
		{"trailing comment", "module example.com/c // the app\n\ngo 1.26\n", "example.com/c"},
	} {
		dir := t.TempDir()
		writeTemp(t, dir, "go.mod", tc.goMod)
		if got := ReadModulePath(dir); got != tc.want {
			t.Errorf("%s: ReadModulePath = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The pass discovers stylesheets as well as Go, but the Go analyzers'
// accessors must stay Go-only: every existing analyzer assumes a file it
// is handed parses as Go, and quietly feeding it a stylesheet is the
// failure mode this test exists to make loud.
func TestStyleFilesAreSeparateFromGoFiles(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeTemp(t, dir, "main.go", "package main\n")
	writeTemp(t, dir, "app.css", "body { margin: 0 }\n")
	writeTemp(t, dir, "static/site.css", ".a { color: red }\n")
	writeTemp(t, dir, "dist/skipped.css", "body { margin: 0 }\n")
	writeTemp(t, dir, ".hidden/skipped.css", "body { margin: 0 }\n")

	p, err := NewPass(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}

	rel := func(fs []SourceFile) []string {
		var out []string
		for _, f := range fs {
			out = append(out, f.Rel)
		}
		return out
	}

	for _, f := range p.Files() {
		if strings.HasSuffix(f.Rel, ".css") {
			t.Errorf("Files() included stylesheet %s — it is documented as Go-only", f.Rel)
		}
	}
	for _, f := range p.AppFiles() {
		if strings.HasSuffix(f.Rel, ".css") {
			t.Errorf("AppFiles() included stylesheet %s — the Go analyzers assume Go source", f.Rel)
		}
	}
	for _, f := range p.TestFiles() {
		if strings.HasSuffix(f.Rel, ".css") {
			t.Errorf("TestFiles() included stylesheet %s", f.Rel)
		}
	}

	got := strings.Join(rel(p.StyleFiles()), ",")
	want := "app.css,static/site.css"
	if got != want {
		t.Errorf("StyleFiles() = %q, want %q (skip rules must apply: dist/ and hidden dirs)", got, want)
	}

	// The stylesheet's bytes must be readable like any discovered file.
	if _, ok := p.Source("app.css"); !ok {
		t.Error("Source(app.css) unavailable — discovery read it but did not keep it")
	}
}

// Configuration exemptions apply to stylesheets exactly as to Go source.
func TestStyleFilesHonourExemptPath(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeTemp(t, dir, "app.css", "body { margin: 0 }\n")
	writeTemp(t, dir, "vendor-pkg/lib.css", ".a { color: red }\n")

	cfg := DefaultConfig()
	cfg.Exempt = []string{"vendor-pkg/**"}
	p, err := NewPass(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := p.StyleFiles()
	if len(got) != 1 || got[0].Rel != "app.css" {
		var rels []string
		for _, f := range got {
			rels = append(rels, f.Rel)
		}
		t.Errorf("StyleFiles() = %v, want [app.css] — the exempt path was not honoured", rels)
	}
}
