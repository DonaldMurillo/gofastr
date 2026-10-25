package clitest

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/scratchdir"
)

// A generated app tree costs a toolchain step: `go build`, `go vet` or
// `go test` compiles most of the framework into it. Tests that check the
// same app share one tree per test binary: the first caller prepares it,
// every later caller gets the prepared directory, and Main removes it when
// the run ends. A shared tree is read-only to its callers. A test that
// writes into the tree (a probe file, an edited source, a swapped
// catalog) works on CopyTree's copy instead. Runtime files are the one
// allowance: a boot keeps its database out of the tree and may leave the
// app's own run files in its working directory.
var sharedTrees struct {
	mu      sync.Mutex
	entries map[string]*sharedEntry
	// remove holds the scratch packages SharedScratchPackage created,
	// which live in the repo, not under Main's temp root.
	remove []string
}

type sharedEntry struct {
	once       sync.Once
	dir        string
	importPath string
	err        error
}

// runRoot is the temp root Main made for this run; shared trees live in it.
var runRoot string

// sharedOnce runs create once per key for the life of the test binary and
// hands every caller its result. A create that panics fails its own caller
// and leaves the error below for the rest.
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

// SharedTree returns a directory that prepare filled once for this test
// binary, under Main's temp root. Every caller with the same key gets the
// same directory, or the same error when prepare failed. It is safe under
// t.Parallel: concurrent callers wait for the one prepare. prepare must not
// use a *testing.T; it reports through its error. It may build the app
// binary too, so tests that only boot it share that link as well.
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

// SharedScratchPackage is SharedTree for a tree that has to sit inside the
// repo module (internal/scratchdir explains why): prepare fills the
// scratch package Root/name once for this test binary, and Main removes it
// when the run ends. It returns the directory and the import path.
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

// CopyTree copies a shared tree into a temp dir of t's own, for a test
// that writes into it.
func CopyTree(t testing.TB, src string) string {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
	return dst
}

// removeSharedTrees removes the scratch packages this run created. The
// trees under runRoot go with it.
func removeSharedTrees() {
	sharedTrees.mu.Lock()
	defer sharedTrees.mu.Unlock()
	for _, dir := range sharedTrees.remove {
		if err := scratchdir.Remove(dir); err != nil {
			fmt.Fprintf(os.Stderr, "clitest: remove shared scratch package %s: %v\n", dir, err)
		}
	}
	sharedTrees.remove = nil
}
