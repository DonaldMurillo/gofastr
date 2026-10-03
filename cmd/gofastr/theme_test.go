package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// TestThemeStarterBoots writes the starter into a temp Go module and
// RUNS it: a _test.go beside the scaffolded theme.go calls
// App.Validate() (the check App.WithTheme panics through at boot) and
// pins the derived size-scale names.
//
// The previous incarnation grepped eleven field names out of a
// hand-maintained template and passed while the scaffold panicked at
// boot (`style.Theme: invalid: Theme.Colors.CodeSurface:
// Color.Value is empty`): the template omitted Colors.CodeSurface/
// CodeText/CodeBorder, the five overlay/toast/dropdown durations and
// the whole easing set. The starter now comes from the same emitter
// `theme edit` writes back with, and this test proves the file on disk
// compiles and validates, not that it contains substrings.
func TestThemeStarterBoots(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs a temp Go module")
	}
	bootThemeModule(t, themeStarterSource())
}

// TestThemeEditWritebackBoots runs the `theme edit` write-back source
// through the same boot: emitThemeGoSource is what a user's edited
// theme/theme.go is rewritten from, so its output must validate too —
// and its auto-derived names must be the canonical ones, or every
// token past xl silently falls back to the framework's hard-coded
// values (the xxl/xxxl rename).
func TestThemeEditWritebackBoots(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs a temp Go module")
	}
	src, err := emitThemeGoSource(uitheme.Default(), "theme")
	if err != nil {
		t.Fatalf("emitThemeGoSource: %v", err)
	}
	bootThemeModule(t, string(src))
}

// bootThemeModule scaffolds a throwaway Go module whose theme package
// is src, adds a test that exercises the boot contract, and runs
// `go test` in it — the repo root is wired in through a replace
// directive the way the other generated-project tests in this package
// do, so no network is needed (core-ui/style has no module deps).
func bootThemeModule(t *testing.T, src string) {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/themeb\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "theme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme", "theme.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// The boot contract: what App.WithTheme enforces at startup, plus
	// the size-scale names the xxl/xxxl regression misderived. Written
	// as a string so it lives in the temp module, not this one.
	boot := `package theme

import "testing"

func TestThemeBoots(t *testing.T) {
	if err := App.Validate(); err != nil {
		t.Fatalf("theme does not validate (a host would panic at boot): %v", err)
	}
	for _, tc := range []struct{ got, want, what string }{
		{App.Spacing.XXL.Name, "2xl", "Spacing.XXL"},
		{App.Spacing.XXXL.Name, "3xl", "Spacing.XXXL"},
		{App.Typography.XXL.Name, "2xl", "Typography.XXL"},
		{App.Typography.XXXL.Name, "3xl", "Typography.XXXL"},
		{App.Breakpoints.XXL.Name, "2xl", "Breakpoints.XXL"},
		{App.Colors.CodeSurface.Name, "code-surface", "Colors.CodeSurface"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: auto-derived Name %q, want %q", tc.what, tc.got, tc.want)
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "theme", "boot_test.go"), []byte(boot), 0o644); err != nil {
		t.Fatal(err)
	}

	test := exec.Command("go", "test", "./...")
	test.Dir = dir
	if out, err := test.CombinedOutput(); err != nil {
		t.Fatalf("go test in temp module: %v\n%s", err, out)
	}
}

// The starter's DarkColors block is generated from the canonical framework
// palette (framework/ui/theme.Default), not hand-maintained; this pins the
// two against drifting apart.
func TestThemeStarterDarkColorsMatchCanonical(t *testing.T) {
	dark := uitheme.Default().DarkColors
	if len(dark) == 0 {
		t.Fatal("canonical theme has no DarkColors")
	}
	for name, value := range dark {
		re := regexp.MustCompile(fmt.Sprintf(`%q:\s*%q`, name, value))
		if !re.MatchString(themeStarter) {
			t.Errorf("themeStarter DarkColors missing %q: %q", name, value)
		}
	}
	if strings.Count(themeStarter, `"#`) < len(dark) {
		t.Error("themeStarter lost its color literals")
	}
}

// TestThemeStarterIsTheAppsOwn pins the two places the starter departs
// from uitheme.Default: it names the theme "app", not the framework's
// "framework-ui", and declares no component options, since the
// framework's complete default set is compiled into :root anyway.
func TestThemeStarterIsTheAppsOwn(t *testing.T) {
	if !strings.Contains(themeStarter, "\tName: \"app\",\n") {
		t.Error(`theme starter does not name the theme "app"`)
	}
	if strings.Contains(themeStarter, "Components:") {
		t.Error("theme starter declares component options; the framework defaults already apply")
	}
}
