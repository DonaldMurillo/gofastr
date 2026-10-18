package atomicfile

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenKeepsPreviousFileAfterReplacement(t *testing.T) {
	dir := t.TempDir()
	source, target := filepath.Join(dir, "next"), filepath.Join(dir, "current")
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := Replace(source, target); err != nil {
		t.Fatal(err)
	}
	before, err := io.ReadAll(f)
	if err != nil || string(before) != "old" {
		t.Fatalf("open reader lost the previous file: %q, %v", before, err)
	}
	after, err := ReadFile(target)
	if err != nil || string(after) != "new" {
		t.Fatalf("new reader did not see replacement: %q, %v", after, err)
	}
}
