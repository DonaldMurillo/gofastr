package verifycmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
)

func TestReportProvenance(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string, m codegen.Marker) {
		t.Helper()
		stamped, err := codegen.Stamp(rel, content, m)
		if err != nil {
			t.Fatal(err)
		}
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(stamped), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("app.go", "package main\n", codegen.Marker{Generator: "app", Kind: codegen.KindScaffold, Version: "v0.92.0"})
	write("ui/a_style.gen.go", "package ui\n", codegen.Marker{Generator: "styles", Kind: codegen.KindDerived})
	write("testdata/skip.go", "package skip\n", codegen.Marker{Generator: "app", Kind: codegen.KindScaffold})
	for _, rel := range []string{"app.go", "ui/a_style.gen.go"} {
		f, err := os.OpenFile(filepath.Join(dir, rel), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("// edit\n"); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	var notes, warns []string
	reportProvenance(dir,
		func(f string, a ...any) { notes = append(notes, strings.TrimSpace(fmt.Sprintf(f, a...))) },
		func(f string, a ...any) { warns = append(warns, strings.TrimSpace(fmt.Sprintf(f, a...))) })
	if len(notes) != 1 || notes[0] != "Generated files: derived styles: 1, scaffold app v0.92.0: 1; 1 scaffold file(s) edited since, which is expected: they are yours" {
		t.Fatalf("notes = %q", notes)
	}
	if len(warns) != 1 || !strings.HasPrefix(warns[0], "ui/a_style.gen.go was edited since `gofastr generate styles`") {
		t.Fatalf("warns = %q", warns)
	}
}
