package devcmd

import (
	"path/filepath"
	"testing"
)

// TestScanModTimesPicksUpAllAssetTypes proves that every watched
// extension is detected in a single scan of a mixed directory.
func TestScanModTimesPicksUpAllAssetTypes(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	writeDevFile(t, filepath.Join(dir, "app.js"), "// JS")
	writeDevFile(t, filepath.Join(dir, "theme.css"), "/* CSS */")
	writeDevFile(t, filepath.Join(dir, "index.html"), "<html></html>")
	writeDevFile(t, filepath.Join(dir, "llm.md"), "# doc")

	result := ScanModTimes(dir)
	if len(result) != 5 {
		t.Fatalf("scanModTimes found %d files, want 5 (.go+.js+.css+.html+.md): %+v", len(result), result)
	}
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		filepath.Join(dir, "main.go"):    true,
		filepath.Join(dir, "app.js"):     true,
		filepath.Join(dir, "theme.css"):  true,
		filepath.Join(dir, "index.html"): true,
		filepath.Join(dir, "llm.md"):     true,
	}
	for path := range result {
		if !expected[path] {
			t.Errorf("unexpected file in scan result: %s", path)
		}
	}
}
