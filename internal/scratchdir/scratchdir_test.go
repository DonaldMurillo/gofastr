package scratchdir

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// listableModule writes a one-package module so `go list ./...` has
// something to list.
func listableModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := fileperm.WriteOwnerOnly(filepath.Join(dir, "go.mod"), []byte("module example.test/m\n\ngo 1.22\n")); err != nil {
		t.Fatal(err)
	}
	if err := fileperm.WriteOwnerOnly(filepath.Join(dir, "m.go"), []byte("package m\n")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A gate in another test binary can create the same scratch directory while
// the check runs. Cleanup must leave that run's files alone.
func TestCleanupKeepsOtherWritersFiles(t *testing.T) {
	dir := listableModule(t)
	live := filepath.Join(dir, "_scratch", "live.go")
	t.Run("check", func(t *testing.T) {
		AssertHiddenFromGoList(t, dir, "_scratch")
		if err := fileperm.WriteOwnerOnly(live, []byte("package live\n")); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("cleanup removed a file it did not write: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "_scratch"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("scratch holds %d entries after cleanup, want only live.go", len(entries))
	}
}

func TestCleanupRemovesTheScratchItMade(t *testing.T) {
	dir := listableModule(t)
	t.Run("check", func(t *testing.T) {
		AssertHiddenFromGoList(t, dir, "_scratch")
	})
	if _, err := os.Stat(filepath.Join(dir, "_scratch")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch directory left behind: %v", err)
	}
}

// The gates rely on Root staying out of `./...` at the module root.
func TestRootHiddenFromGoList(t *testing.T) {
	// The probe goes one level down so the check never writes into Root
	// itself (see Package).
	Package(t, "listcheck")
	AssertHiddenFromGoList(t, ModuleRoot(t), Root+"/listcheck")
}

func TestModulePathMatchesGoMod(t *testing.T) {
	mod, err := os.ReadFile(filepath.Join(ModuleRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if first, _, _ := strings.Cut(string(mod), "\n"); first != "module "+modulePath {
		t.Fatalf("go.mod declares %q, scratchdir assumes %q", first, "module "+modulePath)
	}
}

// Root must exist in every checkout: a gate that created it would change
// the module root's listing mid-sweep.
func TestRootIsTracked(t *testing.T) {
	readme := filepath.Join(ModuleRoot(t), filepath.FromSlash(Root), "README.md")
	if _, err := os.Stat(readme); err != nil {
		t.Fatalf("%s missing: %v", readme, err)
	}
}

func TestPackageIsRemovedAfterTheTest(t *testing.T) {
	var dir, importPath string
	t.Run("gate", func(t *testing.T) {
		dir, importPath = Package(t, "scratchdirprobe")
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("scratch package not created: %v", err)
		}
	})
	if want := modulePath + "/" + Root + "/scratchdirprobe"; importPath != want {
		t.Fatalf("import path %q, want %q", importPath, want)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch package left behind: %v", err)
	}
}

// A killed run leaves its package behind; the next run must not build on it.
func TestPackageClearsAStaleRun(t *testing.T) {
	stale := filepath.Join(ModuleRoot(t), filepath.FromSlash(Root), "scratchdirstale", "stale.go")
	if err := os.Mkdir(filepath.Dir(stale), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := fileperm.WriteOwnerOnly(stale, []byte("package stale\n")); err != nil {
		t.Fatal(err)
	}
	Package(t, "scratchdirstale")
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale file survived: %v", err)
	}
}

func TestRemoveTreeDoesNotFollowSymlinks(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "keep.txt")
	if err := fileperm.WriteOwnerOnly(outside, []byte("keep\n")); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "tree")
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := fileperm.WriteOwnerOnly(filepath.Join(dir, "a", "b", "f.go"), []byte("package b\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(dir, "a", "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := removeTree(dir); err != nil {
		t.Fatalf("removeTree: %v", err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tree left behind: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("removeTree followed a symlink out of the tree: %v", err)
	}
	if err := removeTree(dir); err != nil {
		t.Fatalf("removeTree of a missing tree: %v", err)
	}
}

func TestRemoveTreeUnlinksASymlinkRoot(t *testing.T) {
	target := t.TempDir()
	keep := filepath.Join(target, "keep.txt")
	if err := fileperm.WriteOwnerOnly(keep, []byte("keep\n")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "pkg")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := removeTree(link); err != nil {
		t.Fatalf("removeTree: %v", err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink left behind: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("removeTree emptied the symlink's target: %v", err)
	}
}
