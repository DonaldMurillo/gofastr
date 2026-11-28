package clitest

import (
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/internal/scratchdir"
)

var sharedTrees struct {
	mu      sync.Mutex
	entries map[string]*sharedEntry
	remove  []string
}

type sharedEntry struct {
	once       sync.Once
	dir        string
	importPath string
	err        error
}

var runRoot string

func sharedOnce(key string, create func() (dir, importPath string, err error)) *sharedEntry {
	sharedTrees.mu.Lock()
	if sharedTrees.entries == nil {
		sharedTrees.entries = map[string]*sharedEntry{}
	}
	e := sharedTrees.entries[key]
	if e == nil {
		e = &sharedEntry{}
		sharedTrees.entries[key] = e
	}
	sharedTrees.mu.Unlock()
	e.once.Do(func() {
		e.err = errors.New("preparing it panicked in an earlier test")
		e.dir, e.importPath, e.err = create()
	})
	return e
}

// SharedTree returns the directory prepare filled once per key for this
// test binary; callers read it and write only into a CopyTree copy.
func SharedTree(t testing.TB, key string, prepare func(dir string) error) string {
	t.Helper()
	e := sharedOnce("tree "+key, func() (string, string, error) {
		if runRoot == "" {
			return "", "", errors.New("clitest.SharedTree needs the package's TestMain to call clitest.Main, which removes the tree")
		}
		dir, err := os.MkdirTemp(runRoot, "tree-")
		if err != nil {
			return "", "", err
		}
		return dir, "", prepare(dir)
	})
	if e.err != nil {
		t.Fatalf("shared tree %s: %v", key, e.err)
	}
	return e.dir
}

// SharedScratchPackage is SharedTree for a scratch package inside the repo
// module; it returns the directory and the import path.
func SharedScratchPackage(t testing.TB, name string, prepare func(dir, importPath string) error) (dir, importPath string) {
	t.Helper()
	e := sharedOnce("scratch "+name, func() (string, string, error) {
		if runRoot == "" {
			return "", "", errors.New("clitest.SharedScratchPackage needs the package's TestMain to call clitest.Main, which removes the package")
		}
		if moduleRoot == "" {
			return "", "", errors.New("clitest: no go.mod at or above the test's working directory")
		}
		dir, importPath, err := scratchdir.Make(moduleRoot, name)
		if err != nil {
			return "", "", err
		}
		sharedTrees.mu.Lock()
		sharedTrees.remove = append(sharedTrees.remove, dir)
		sharedTrees.mu.Unlock()
		return dir, importPath, prepare(dir, importPath)
	})
	if e.err != nil {
		t.Fatalf("shared scratch package %s: %v", name, e.err)
	}
	return e.dir, e.importPath
}

// CopyTree copies src into a temp directory of t's own.
func CopyTree(t testing.TB, src string) string {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
	return dst
}

func removeSharedTrees() {
	sharedTrees.mu.Lock()
	defer sharedTrees.mu.Unlock()
	for _, dir := range sharedTrees.remove {
		if err := scratchdir.Remove(dir); err != nil {
			cli.Fprintf(os.Stderr, "clitest: remove shared scratch package %s: %v\n", dir, err)
		}
	}
	sharedTrees.remove = nil
}
