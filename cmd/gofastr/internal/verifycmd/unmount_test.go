package verifycmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core/settings"
)

// verify lists each copy, warns for one older than go.mod's release, for
// an entry with no files, and for a marked copy the settings file lost.
func TestReportUnmountedWarnings(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.27\n\nrequire "+project.GofastrModule+" v0.92.0\n")
	old := settings.Unmounted{Source: project.GofastrModule + "/core/yaml", Version: "v0.91.0", Path: "unmounted/core/yaml", Date: "2026-10-19"}
	gone := settings.Unmounted{Source: project.GofastrModule + "/core/textsafe", Version: "v0.92.0", Path: "unmounted/core/textsafe", Date: "2026-10-19"}
	clitest.WriteUnmountedSettings(t, dir, old, gone)
	if err := os.MkdirAll(filepath.Join(dir, "unmounted", "core", "yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "unmounted", "core", "yaml", "yaml.go"), "package yaml\n")
	stray, err := codegen.Stamp("x.go", "package x\n", codegen.Marker{Generator: project.UnmountGenerator, Kind: codegen.KindScaffold, Version: "v0.90.0", Source: project.GofastrModule + "/battery/auth"})
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "x.go"), stray)

	var notes, warns []string
	marked, err := ScanProvenance(dir)
	if err != nil {
		t.Fatal(err)
	}
	reportUnmounted(dir, marked,
		func(f string, a ...any) { notes = append(notes, fmt.Sprintf(f, a...)) },
		func(f string, a ...any) { warns = append(warns, fmt.Sprintf(f, a...)) })
	all := strings.Join(warns, "\n")
	for _, want := range []string{
		"core/yaml was copied at v0.91.0 and go.mod requires gofastr v0.92.0",
		project.UnmountedCompareURL("v0.91.0", "v0.92.0"),
		"lists " + project.GofastrModule + "/core/textsafe at unmounted/core/textsafe, but that directory holds no files",
		"x.go is marked as an unmounted copy of " + project.GofastrModule + "/battery/auth",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("warnings miss %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "textsafe was copied") {
		t.Errorf("a current copy is reported behind:\n%s", all)
	}
	if len(notes) != 2 || !strings.Contains(notes[0], "core/textsafe v0.92.0 at unmounted/core/textsafe (unmounted 2026-10-19)") {
		t.Errorf("notes = %v", notes)
	}
}

// Without a readable go.mod verify cannot compare the copies with the
// release, and says so instead of going quiet.
func TestReportUnmountedNoGoMod(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteUnmountedSettings(t, dir, settings.Unmounted{Source: project.GofastrModule + "/core/yaml", Version: "v0.91.0", Path: "unmounted/core/yaml", Date: "2026-10-19"})
	var warns []string
	reportUnmounted(dir, nil, func(string, ...any) {}, func(f string, a ...any) { warns = append(warns, fmt.Sprintf(f, a...)) })
	if all := strings.Join(warns, "\n"); !strings.Contains(all, "cannot tell whether the unmounted copies are behind") {
		t.Errorf("warnings:\n%s", all)
	}
}
