package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/generatecmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/verifycmd"
	"github.com/DonaldMurillo/gofastr/codegen"
)

// Every Go file a fresh app generate writes carries an app scaffold
// marker at this CLI's version, and none reads as edited. The chrome
// fixture copies the canonical header and footer packages, so it covers
// styles output the app generator passes through too.
func TestGeneratedAppFilesAreMarked(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(clitest.ChromeAppSpecYAML()), 0o600); err != nil {
		t.Fatal(err)
	}
	clitest.CovT_capStdout(t, func() { generatecmd.RunGenerate([]string{"app", bp}) })

	entries, err := verifycmd.ScanProvenance(dir)
	if err != nil {
		t.Fatal(err)
	}
	marked := map[string]codegen.Marker{}
	for _, e := range entries {
		if e.Edited {
			t.Errorf("%s reads as edited straight after generate", e.Path)
		}
		marked[e.Path] = e.Marker
	}
	goFiles, genFiles := 0, 0
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		goFiles++
		rel, _ := filepath.Rel(dir, p)
		m, ok := marked[filepath.ToSlash(rel)]
		if !ok {
			t.Errorf("%s has no provenance marker", rel)
			return nil
		}
		// A copied chrome package's styles output stays derived from its
		// sheet; everything else is the app's scaffold.
		if strings.HasSuffix(p, ".gen.go") {
			genFiles++
			if m.Kind != codegen.KindDerived || m.Generator != "styles" {
				t.Errorf("%s marker = %#v, want derived styles", rel, m)
			}
		} else if m.Kind != codegen.KindScaffold || m.Generator != "app" || m.Version != appgen.MarkerVersion(project.CliVersion()) {
			t.Errorf("%s marker = %#v", rel, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if goFiles == 0 || genFiles == 0 {
		t.Fatalf("generate wrote %d Go file(s), %d of them styles output; the fixture should cover both", goFiles, genFiles)
	}
}
