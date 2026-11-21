package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirstGoPackageNameSkipsIgnoredFilesAndReadsUnfinishedClause(t *testing.T) {
	dir := t.TempDir()
	for _, input := range [][2]string{
		{".old.go", "package hidden\n"},
		{"_old.go", "package ignored\n"},
		{"app.go", "package app\nfunc unfinished(\n"},
	} {
		if err := os.WriteFile(filepath.Join(dir, input[0]), []byte(input[1]), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := FirstGoPackageName(dir, "")
	if err != nil || !ok || got != "app" {
		t.Fatalf("package %q, found %v, error %v; want app", got, ok, err)
	}
}
