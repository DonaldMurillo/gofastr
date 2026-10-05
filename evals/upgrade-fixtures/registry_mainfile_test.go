package upgradefixtures

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A public package directory that also holds a build-constrained
// package main file (a //go:build ignore generator beside the library)
// is still a package an app imports. The index once stopped at the
// main file and dropped the whole directory, so a removal from that
// package's API went unnoticed and a new import of it read as removed.
// A directory whose files are all package main stays out.
func TestMainFileBesideLibraryKeepsDir(t *testing.T) {
	repo := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		p := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/m\n\ngo 1.27\n")
	write("lib/lib.go", "package lib\n\n// Exported is the API.\nfunc Exported() {}\n")
	write("lib/gen.go", "//go:build ignore\n\npackage main\n\nfunc main() {}\n")
	write("tool/main.go", "package main\n\nfunc main() {}\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	api, err := loadExportedAPI(repo, worktreeRef)
	if err != nil {
		t.Fatal(err)
	}
	lib, ok := api["lib"]
	if !ok {
		t.Fatal("lib dropped from the index: a package main file beside the library hid the package")
	}
	if !lib.decls["Exported"] {
		t.Errorf("lib's Exported missing from the index: %v", lib.decls)
	}
	if _, ok := api["tool"]; ok {
		t.Error("tool indexed: a directory of package main files is not an importable package")
	}
}
