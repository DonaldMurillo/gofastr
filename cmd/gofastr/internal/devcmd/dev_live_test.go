package devcmd

import (
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// TestChangedIgnoresUnmodifiedFiles proves that unchanged files don't
// trigger a false-positive rebuild. Same mod-time → no change.
func TestChangedIgnoresUnmodifiedFiles(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	prev := ScanModTimes(dir)
	// Same scan immediately; mod times should be identical.
	curr := ScanModTimes(dir)
	if Changed(prev, curr) {
		t.Fatal("changed() = true for identical mod times — false positive rebuild")
	}
}

// TestBuildAndServeReturnsFalseOnBuildError proves that a build failure
// (syntax error) returns false without starting a server.
func TestBuildAndServeReturnsFalseOnBuildError(t *testing.T) {
	dir := t.TempDir()
	// Write invalid Go: will fail to build.
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main\nfunc invalid syntax here{")

	var mu sync.Mutex
	var cmd *exec.Cmd

	ok := buildAndServe(dir, ".", "localhost:0", nil, &mu, &cmd, false, nil, nil)
	if ok {
		t.Fatal("buildAndServe should return false for invalid Go code")
		KillServer(&mu, &cmd)
	}
}

// TestScanModTimesPicksUpAllAssetTypes proves that .go, .js, .css, and
// .html are all detected in a single scan of a mixed directory.
func TestScanModTimesPicksUpAllAssetTypes(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	writeDevFile(t, filepath.Join(dir, "app.js"), "// JS")
	writeDevFile(t, filepath.Join(dir, "theme.css"), "/* CSS */")
	writeDevFile(t, filepath.Join(dir, "index.html"), "<html></html>")

	result := ScanModTimes(dir)
	if len(result) != 4 {
		t.Fatalf("scanModTimes found %d files, want 4 (.go+.js+.css+.html): %+v", len(result), result)
	}

	expected := map[string]bool{
		filepath.Join(dir, "main.go"):    true,
		filepath.Join(dir, "app.js"):     true,
		filepath.Join(dir, "theme.css"):  true,
		filepath.Join(dir, "index.html"): true,
	}
	for path := range result {
		if !expected[path] {
			t.Errorf("unexpected file in scan result: %s", path)
		}
	}
}
