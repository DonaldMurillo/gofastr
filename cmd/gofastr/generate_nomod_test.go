package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A blueprint generated outside a Go module used to report plain success and
// then recommend `go mod tidy`, which is the one command that cannot work
// there:
//
//	✓ Generated 22 file(s) in the module root
//	Next steps:
//	  go mod tidy   →  go.mod file not found in current directory or any parent
//
// The generated code imports itself by module path (…/entities), so without a
// go.mod nothing it emitted can build. The adjacent case, a go.mod that
// declares a DIFFERENT module, already fails with a message naming the exact
// remedy (resolveBlueprintModule). Only the absent case was unhandled, and it
// is the one a first-time user hits.
//
// This is a warning rather than a hard error on purpose: generating into a
// directory that is about to become a module is legitimate, and the blueprint
// declares app.module, so the exact `go mod init` line can be printed instead
// of refusing the work.
func TestGenerateWithoutGoModWarnsAndNamesTheInitCommand(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}

	out := covT_capStdout(t, func() { runGenerate([]string{"--from=" + bp}) })

	if !strings.Contains(out, "go.mod") {
		t.Errorf("generating outside a module never mentioned go.mod:\n%s", out)
	}
	// The remedy must be the runnable command, not a description of it. The
	// module-mismatch guard sets that bar and this path should meet it.
	module := blueprintModuleFromYAML(t, testBlueprintYAML())
	wantInit := "go mod init " + module
	if !strings.Contains(out, wantInit) {
		t.Errorf("did not name the exact remedy %q:\n%s", wantInit, out)
	}
	// `go mod tidy` as the FIRST step is the specific lie being fixed: it
	// cannot succeed before `go mod init`. It may still appear after it.
	tidyAt := strings.Index(out, "go mod tidy")
	initAt := strings.Index(out, wantInit)
	if tidyAt >= 0 && initAt >= 0 && tidyAt < initAt {
		t.Errorf("recommended `go mod tidy` before `go mod init` — tidy cannot run first:\n%s", out)
	}
}

// The regression guard for the fix above: inside a real module the extra
// warning must not appear, or every correct run grows noise it should not.
func TestGenerateInsideModuleDoesNotWarnAboutGoMod(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	module := blueprintModuleFromYAML(t, testBlueprintYAML())
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}

	out := covT_capStdout(t, func() { runGenerate([]string{"--from=" + bp}) })

	if strings.Contains(out, "go mod init") {
		t.Errorf("warned about a missing go.mod inside a real module:\n%s", out)
	}
	if !strings.Contains(out, "go mod tidy") {
		t.Errorf("dropped the ordinary next steps inside a module:\n%s", out)
	}
}

// blueprintModuleFromYAML reads the `module:` line out of a test blueprint so
// the assertions above pin the real declared path rather than a copy that can
// drift from testBlueprintYAML.
func blueprintModuleFromYAML(t *testing.T, yaml string) string {
	t.Helper()
	for _, line := range strings.Split(yaml, "\n") {
		_, after, found := strings.Cut(strings.TrimSpace(line), "module:")
		if !found {
			continue
		}
		if v := strings.TrimSpace(after); v != "" {
			return v
		}
	}
	t.Fatalf("testBlueprintYAML declares no module: line — update this helper")
	return ""
}

// The documented fresh-app flow is go mod init, gofastr generate, go mod
// tidy. With no gofastr requirement in go.mod, tidy resolved the
// generated axe_test.go's direct chromedp import at @latest (v0.19.1,
// a breaking generic API) instead of the framework's own pin, and
// framework/testkit/axetest stopped compiling. The generator pins the
// framework release matching this CLI, so MVS keeps chromedp at the
// framework's version.
func TestGeneratePinsFrameworkVersion(t *testing.T) {
	oldVersion := version
	version = "0.39.1"
	t.Cleanup(func() { version = oldVersion })

	dir := t.TempDir()
	covT_chdir(t, dir)
	module := blueprintModuleFromYAML(t, testBlueprintYAML())
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() { runGenerate([]string{"--from=" + bp}) })
	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mod), "github.com/DonaldMurillo/gofastr v0.39.1") {
		t.Fatalf("generate did not pin the CLI's framework release:\n%s\n%s", mod, out)
	}
}

// A development CLI has no release to pin: the next steps name the
// go get that must run before tidy, and a go.mod that already requires
// the framework is left alone.
func TestGenerateDevBuildNamesGoGet(t *testing.T) {
	oldVersion := version
	version = "dev"
	t.Cleanup(func() { version = oldVersion })

	dir := t.TempDir()
	covT_chdir(t, dir)
	module := blueprintModuleFromYAML(t, testBlueprintYAML())
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() { runGenerate([]string{"--from=" + bp}) })
	getAt := strings.Index(out, "go get github.com/DonaldMurillo/gofastr@")
	tidyAt := strings.Index(out, "go mod tidy")
	if getAt < 0 || tidyAt < 0 || getAt > tidyAt {
		t.Errorf("a dev CLI must name `go get github.com/DonaldMurillo/gofastr@…` before go mod tidy:\n%s", out)
	}
}
