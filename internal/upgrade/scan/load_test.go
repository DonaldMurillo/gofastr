package scan

import (
	"fmt"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"golang.org/x/tools/go/packages"
)

// winOnly is a file the host configuration excludes: it builds only
// under GOOS=windows.
const winOnly = `//go:build windows

package main

import "example.com/kit/ui"

func win() { _ = ui.SiteHeader("w") }
`

func plainMain() string {
	return "package main\n\nfunc main() {}\n"
}

const toolsUse = "package main\n\nimport \"example.com/kit/ui\"\n\nfunc main() { _ = ui.SiteHeader(\"t\") }\n"

// nestedTools writes a nested module dir/tools requiring the kit the
// way the main module does; a "go.mod" entry replaces the generated
// one.
func nestedTools(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	gomod := "module example.com/app/tools\n\ngo 1.26.0\n\nrequire example.com/kit v0.1.0\n\nreplace example.com/kit => ../../kit\n"
	if override, ok := files["go.mod"]; ok {
		gomod = override
	}
	writeWorkspaceFile(t, dir, "tools/go.mod", gomod)
	for rel, src := range files {
		if rel == "go.mod" {
			continue
		}
		writeWorkspaceFile(t, dir, "tools/"+rel, src)
	}
}

func TestScanWindowsTagFileHit(t *testing.T) {
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go": plainMain(),
		"win.go":  winOnly,
	}), n)
	// On a windows host the main load compiles the file; everywhere
	// else the windows configuration load must reach it.
	wantHits(t, res, n, hitAt(winOnly, "SiteHeader", "win.go", siteHeaderSym.String()))
}

func TestScanSuffixFilesHit(t *testing.T) {
	linuxSrc := "package main\n\nimport \"example.com/kit/ui\"\n\nfunc linuxOnly() { _ = ui.SiteHeader(\"l\") }\n"
	winSrc := "package main\n\nimport \"example.com/kit/ui\"\n\nfunc winOnly() { _ = ui.SiteHeader(\"w\") }\n"
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"oops_linux.go":   linuxSrc,
		"oops_windows.go": winSrc,
	}), n)
	// Whichever host runs the test, one file is always on the far
	// side of a GOOS configuration.
	wantHits(t, res, n,
		hitAt(linuxSrc, "SiteHeader", "oops_linux.go", siteHeaderSym.String()),
		hitAt(winSrc, "SiteHeader", "oops_windows.go", siteHeaderSym.String()))
}

func TestScanTaggedTestFileHit(t *testing.T) {
	src := `//go:build integration

package main

import "example.com/kit/ui"

func extra() { _ = ui.SiteHeader("e") }
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":       plainMain(),
		"extra_test.go": src,
	}), n)
	wantHits(t, res, n, hitAt(src, "SiteHeader", "extra_test.go", siteHeaderSym.String()))
}

// TestScanExtraConfigSkipsScannedFiles proves the configuration load
// reports only its own files: main.go is already scanned by the main
// load, and the error the windows build hits there (unixHelper is in
// the !windows file) stays out of the report.
func TestScanExtraConfigSkipsScannedFiles(t *testing.T) {
	mainSrc := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.SiteHeader("t")
	unixHelper()
}
`
	nixSrc := "//go:build !windows\n\npackage main\n\nfunc unixHelper() {}\n"
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go": mainSrc,
		"nix.go":  nixSrc,
		"win.go":  winOnly,
	}), n)
	wantHits(t, res, n,
		hitAt(mainSrc, "SiteHeader", "main.go", siteHeaderSym.String()),
		hitAt(winOnly, "SiteHeader", "win.go", siteHeaderSym.String()))
	if unexplained := hitStrs(res.Unexplained); len(unexplained) > 0 {
		t.Errorf("already-scanned file's windows-only error reported:\n%v", unexplained)
	}
	if !res.TypeChecked || len(res.Broken) > 0 {
		t.Errorf("an error only in already-scanned files must not mark the app broken: TypeChecked=%v Broken=%v",
			res.TypeChecked, res.Broken)
	}
}

func TestScanBrokenConstrainedFileBroken(t *testing.T) {
	// kitV2 deleted SiteHeader: the windows file breaks in the windows
	// load, which is the upgrade's doing and must say so.
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, kitV2, map[string]string{
		"main.go": plainMain(),
		"win.go":  winOnly,
	}), n)
	if res.TypeChecked || len(res.Broken) == 0 {
		t.Errorf("a constrained file broken in its own load must mark the app broken: TypeChecked=%v Broken=%v",
			res.TypeChecked, res.Broken)
	}
	if len(res.Hits[n]) == 0 {
		t.Error("the compile-error fallback found no hit in win.go")
	}
}

func TestScanUnsatisfiableUnscanned(t *testing.T) {
	stuck := `//go:build linux && windows

package main

func stuck() {}
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":  plainMain(),
		"stuck.go": stuck,
	}), n)
	wantHits(t, res, n)
	want := []string{"stuck.go (no satisfiable build configuration)"}
	if fmt.Sprint(res.Unscanned) != fmt.Sprint(want) {
		t.Errorf("Unscanned:\n got %v\nwant %v", res.Unscanned, want)
	}
}

// genProgram is a `go run gen.go` generator: package main beside
// package pages, kept out of every build by the ignore tag.
const genProgram = `//go:build ignore

package main

import "example.com/kit/ui"

func main() { _ = ui.SiteHeader("g") }
`

func TestScanIgnoreGeneratorHit(t *testing.T) {
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":        plainMain(),
		"pages/pages.go": "package pages\n\nfunc Page() {}\n",
		"pages/gen.go":   genProgram,
	}), n)
	wantHits(t, res, n, hitAt(genProgram, "SiteHeader", "pages/gen.go", siteHeaderSym.String()))
	if !res.TypeChecked || len(res.Unscanned) > 0 || len(res.Unexplained) > 0 {
		t.Errorf("generator must load alone and type-check: TypeChecked=%v Unscanned=%v Unexplained=%v",
			res.TypeChecked, res.Unscanned, hitStrs(res.Unexplained))
	}
}

func TestScanIgnoreDisabledFileUnscanned(t *testing.T) {
	// A disabled copy of a sibling's code: built with its package it
	// redeclares Page, built alone it misses its siblings.
	old := "//go:build ignore\n\npackage pages\n\nimport \"example.com/kit/ui\"\n\nfunc Page() { _ = ui.SiteHeader(\"o\") }\n"
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":        plainMain(),
		"pages/pages.go": "package pages\n\nfunc Page() {}\n",
		"pages/old.go":   old,
	}), n)
	wantHits(t, res, n)
	want := []string{"pages/old.go (excluded from every build by //go:build ignore)"}
	if fmt.Sprint(res.Unscanned) != fmt.Sprint(want) {
		t.Errorf("Unscanned:\n got %v\nwant %v", res.Unscanned, want)
	}
	if !res.TypeChecked || len(res.Unexplained) > 0 {
		t.Errorf("a disabled file must not break the scan: TypeChecked=%v Unexplained=%v",
			res.TypeChecked, hitStrs(res.Unexplained))
	}
}

func TestScanNestedModuleHit(t *testing.T) {
	dir := newWorkspace(t, defaultKit, map[string]string{"main.go": plainMain()})
	nestedTools(t, dir, map[string]string{"main.go": toolsUse})
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, dir, n)
	wantHits(t, res, n, hitAt(toolsUse, "SiteHeader", "tools/main.go", siteHeaderSym.String()))
}

// TestScanNestedModuleInWorkOnce proves a go.work-listed nested module
// is scanned exactly once whether the workspace's main load reaches it
// (the coverage check then skips the walk's load) or not: the tools
// module directory backs exactly one load, and its hit is reported
// once.
func TestScanNestedModuleInWorkOnce(t *testing.T) {
	dir := newWorkspace(t, defaultKit, map[string]string{"main.go": plainMain()})
	nestedTools(t, dir, map[string]string{"main.go": toolsUse})
	writeWorkspaceFile(t, dir, "go.work", "go 1.27.0\n\nuse (\n\t.\n\t./tools\n)\n")

	tools := filepath.Join(dir, "tools")
	old := packagesLoad
	toolsLoads := 0
	packagesLoad = func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
		if cfg.Dir == tools {
			toolsLoads++
		}
		return old(cfg, patterns...)
	}
	t.Cleanup(func() { packagesLoad = old })

	testEnv(t)
	// Workspace mode refuses -mod=mod; replace-only resolution
	// needs no flag.
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", filepath.Join(dir, "go.work"))
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res, err := Run(dir, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if toolsLoads != 1 {
		t.Errorf("the nested module must be loaded exactly once, got %d", toolsLoads)
	}
	wantHits(t, res, n, hitAt(toolsUse, "SiteHeader", "tools/main.go", siteHeaderSym.String()))
}

func TestScanSkipsIgnoredModuleDirs(t *testing.T) {
	dir := newWorkspace(t, defaultKit, map[string]string{"main.go": plainMain()})
	for _, sub := range []string{"testdata/tm", "vendor/vm"} {
		writeWorkspaceFile(t, dir, sub+"/go.mod",
			"module example.com/skip\n\ngo 1.26.0\n\nrequire example.com/kit v0.1.0\n\nreplace example.com/kit => ../../../kit\n")
		writeWorkspaceFile(t, dir, sub+"/main.go", toolsUse)
	}
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, dir, n)
	wantHits(t, res, n)
	if len(res.Unscanned) > 0 {
		t.Errorf("skipped module dirs produced Unscanned entries: %v", res.Unscanned)
	}
}

func TestScanNestedGoModDirective(t *testing.T) {
	dir := newWorkspace(t, defaultKit, map[string]string{"main.go": plainMain()})
	directive := "go 1.25.0"
	nestedTools(t, dir, map[string]string{
		"main.go": plainMain(),
		"go.mod":  "module example.com/app/tools\n\n" + directive + "\n\nrequire example.com/kit v0.1.0\n\nreplace example.com/kit => ../../kit\n",
	})
	n := gomodNote("1.26")
	res := mustRun(t, dir, n)
	want := "tools/go.mod:" + strconv.Itoa(lineOf("module example.com/app/tools\n\n"+directive+"\n", directive)) + ":0 gomod go 1.25.0 < 1.26"
	wantHits(t, res, n, want)
}

func TestScanNestedConstrainedFileHit(t *testing.T) {
	dir := newWorkspace(t, defaultKit, map[string]string{"main.go": plainMain()})
	nestedTools(t, dir, map[string]string{
		"main.go":          plainMain(),
		"tools_windows.go": winOnly,
	})
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, dir, n)
	wantHits(t, res, n, hitAt(winOnly, "SiteHeader", "tools/tools_windows.go", siteHeaderSym.String()))
}
