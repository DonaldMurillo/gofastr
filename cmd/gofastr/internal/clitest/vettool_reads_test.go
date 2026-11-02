package clitest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestRepoVettoolCallersDeclareTheirReads holds every test that vets with
// RepoVettool to a //gofastr:test-reads directive naming the analyzers.
// RepoVettool builds ./cmd/vettool in a subprocess, which no import edge
// shows: cmd/affected selected appgen and generatecmd for an analyzer
// change only by accident of whatever else the diff touched, so a widened
// rule that fired on emitted code (the generated CLI's config.go is the
// standing example) passed the pull request and failed main's full run.
// Found 2026-11-02 guard bash: `go run ./cmd/affected -files
// internal/analyzers/discardeddecode/discardeddecode.go` listed neither
// package.
//
//gofastr:test-reads cmd/gofastr/
func TestRepoVettoolCallersDeclareTheirReads(t *testing.T) {
	if moduleRoot == "" {
		t.Skip("no go.mod at or above the test's working directory")
	}
	root := filepath.Join(moduleRoot, "cmd", "gofastr")
	directive := regexp.MustCompile(`(?m)^//gofastr:test-reads\s.*\binternal/analyzers/(\s|$)`)
	callers := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "vettool_reads_test.go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(src), "RepoVettool()") {
			return nil
		}
		callers++
		if !directive.Match(src) {
			rel, _ := filepath.Rel(moduleRoot, path)
			t.Errorf("%s calls clitest.RepoVettool but declares no `//gofastr:test-reads internal/analyzers/ cmd/vettool/`: an analyzer change never selects it", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if callers == 0 {
		t.Fatal("found no RepoVettool caller under cmd/gofastr: the walk is broken")
	}
}
