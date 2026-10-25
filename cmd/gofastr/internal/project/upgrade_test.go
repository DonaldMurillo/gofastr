package project

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// Registry parsing and validation live in internal/upgrade
// (registry_test.go): the embedded file, the refusal table, the
// semver helpers. What stays here is the CLI: go.mod reading, argument
// parsing, and the renderer.

func TestGoModGofastrVersion(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), `module example.com/app

go 1.26

require (
	github.com/DonaldMurillo/gofastr v0.21.0
	github.com/other/dep v1.2.3
)
`)
	v, replaced, err := GoModGofastrVersion(dir)
	if err != nil {
		t.Fatalf("goModGofastrVersion: %v", err)
	}
	if v != "v0.21.0" || replaced {
		t.Errorf("got v=%q replaced=%v", v, replaced)
	}
}

func TestGoModGofastrVersionReplaceDirective(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), `module example.com/app

go 1.26

require github.com/DonaldMurillo/gofastr v0.21.0

replace github.com/DonaldMurillo/gofastr => ../gofastr
`)
	v, replaced, err := GoModGofastrVersion(dir)
	if err != nil {
		t.Fatalf("goModGofastrVersion: %v", err)
	}
	if v != "v0.21.0" || !replaced {
		t.Errorf("got v=%q replaced=%v, want v0.21.0 + replaced", v, replaced)
	}
}

func TestGoModGofastrVersionBlockReplace(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), `module example.com/app

go 1.26

require github.com/DonaldMurillo/gofastr v0.21.0

replace (
	github.com/DonaldMurillo/gofastr => ../gofastr
	github.com/other/dep => ../dep
)
`)
	v, replaced, err := GoModGofastrVersion(dir)
	if err != nil {
		t.Fatalf("goModGofastrVersion: %v", err)
	}
	if v != "v0.21.0" || !replaced {
		t.Errorf("block-form replace must be detected: v=%q replaced=%v", v, replaced)
	}
}

// Only a require names the current version: a gofastr line inside an
// exclude (or retract) block is not one.
func TestGoModVersionIgnoresExcludeBlock(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), `module example.com/app

go 1.27

require github.com/DonaldMurillo/gofastr v0.80.0

exclude (
	github.com/DonaldMurillo/gofastr v0.85.1
)
`)
	v, replaced, err := GoModGofastrVersion(dir)
	if err != nil {
		t.Fatalf("goModGofastrVersion: %v", err)
	}
	if v != "v0.80.0" || replaced {
		t.Errorf("got v=%q replaced=%v, want v0.80.0 from the require", v, replaced)
	}
}
