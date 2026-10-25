package clitest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/scratchdir"
)

// useRunRoot stands in for Main, which this package's tests do not run.
func useRunRoot(t *testing.T) {
	t.Helper()
	prev := runRoot
	runRoot = t.TempDir()
	t.Cleanup(func() { runRoot = prev })
}

func TestSharedTreePreparesOncePerKey(t *testing.T) {
	useRunRoot(t)
	var calls atomic.Int32
	prepare := func(dir string) error {
		calls.Add(1)
		return os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o600)
	}
	key := t.Name()
	var wg sync.WaitGroup
	dirs := make([]string, 8)
	for i := range dirs {
		wg.Go(func() { dirs[i] = SharedTree(t, key, prepare) })
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("prepare ran %d times for one key, want once", n)
	}
	for _, d := range dirs {
		if d != dirs[0] {
			t.Fatalf("callers got different trees: %q and %q", dirs[0], d)
		}
	}
	if _, err := os.Stat(filepath.Join(dirs[0], "marker")); err != nil {
		t.Fatalf("the tree lacks what prepare wrote: %v", err)
	}
	if other := SharedTree(t, key+"-other", prepare); other == dirs[0] || calls.Load() != 2 {
		t.Fatalf("a second key reused the first tree (%q) or skipped prepare (%d calls)", other, calls.Load())
	}
}

func TestSharedTreeGivesEveryCallerThePrepareError(t *testing.T) {
	useRunRoot(t)
	key := t.Name()
	var calls atomic.Int32
	prepare := func(string) error {
		calls.Add(1)
		return errors.New("generator broke")
	}
	for range 2 {
		ft := &fatalRecorder{TB: t}
		func() {
			defer func() { _ = recover() }()
			SharedTree(ft, key, prepare)
		}()
		if ft.msg == "" {
			t.Fatal("a caller got a tree whose prepare failed")
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("prepare ran %d times, want once: the error is shared, not retried", n)
	}
}

func TestSharedTreeAfterAPanickingPrepareFails(t *testing.T) {
	useRunRoot(t)
	key := t.Name()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the first caller did not see prepare's panic")
			}
		}()
		SharedTree(t, key, func(string) error { panic("boom") })
	}()
	ft := &fatalRecorder{TB: t}
	func() {
		defer func() { _ = recover() }()
		SharedTree(ft, key, func(string) error { return nil })
	}()
	if ft.msg == "" {
		t.Fatal("a caller after a panicking prepare got a half-made tree")
	}
}

func TestSharedTreeNeedsMain(t *testing.T) {
	prev := runRoot
	runRoot = ""
	t.Cleanup(func() { runRoot = prev })
	ft := &fatalRecorder{TB: t}
	func() {
		defer func() { _ = recover() }()
		SharedTree(ft, t.Name(), func(string) error { return nil })
	}()
	if ft.msg == "" {
		t.Fatal("SharedTree made a tree with no Main to remove it")
	}
}

func TestCopyTreeLeavesTheSourceAlone(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "a.go"), []byte("package a"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := CopyTree(t, src)
	if err := os.WriteFile(filepath.Join(dst, "sub", "a.go"), []byte("package b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(src, "sub", "a.go")); err != nil || string(got) != "package a" {
		t.Fatalf("writing the copy changed the source: %q, %v", got, err)
	}
}

// useModuleRoot points SharedScratchPackage at a temp module, so these
// tests never write into the repo.
func useModuleRoot(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(scratchdir.Root)), 0o700); err != nil {
		t.Fatal(err)
	}
	prev := moduleRoot
	moduleRoot = root
	t.Cleanup(func() { moduleRoot = prev })
}

// fatalOf runs f against a recorder and returns the message f failed
// with, or "" when it did not fail.
func fatalOf(t *testing.T, f func(testing.TB)) string {
	ft := &fatalRecorder{TB: t}
	func() {
		defer func() {
			if r := recover(); r != nil && r != "fatal" {
				panic(r)
			}
		}()
		f(ft)
	}()
	return ft.msg
}

// SharedScratchPackage writes into the repo, so it refuses to start
// without Main to remove the package or a module to put it in.
func TestSharedScratchPackageRefusesWithoutMainOrModule(t *testing.T) {
	useModuleRoot(t)
	prepare := func(string, string) error { return nil }
	prev := runRoot
	runRoot = ""
	t.Cleanup(func() { runRoot = prev })
	if msg := fatalOf(t, func(tb testing.TB) { SharedScratchPackage(tb, "needs-main", prepare) }); !strings.Contains(msg, "clitest.Main") {
		t.Errorf("with no Main: failed with %q, want a refusal naming clitest.Main", msg)
	}
	useRunRoot(t)
	moduleRoot = ""
	if msg := fatalOf(t, func(tb testing.TB) { SharedScratchPackage(tb, "needs-module", prepare) }); !strings.Contains(msg, "no go.mod") {
		t.Errorf("with no module: failed with %q, want a refusal naming the missing go.mod", msg)
	}
}

// One prepare per name, and removeSharedTrees deletes every package the
// run made, including one whose prepare failed.
func TestSharedScratchPackagePreparesOnceAndIsRemoved(t *testing.T) {
	useRunRoot(t)
	useModuleRoot(t)
	calls := 0
	prepare := func(dir, _ string) error {
		calls++
		return os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n"), 0o600)
	}
	dir, _ := SharedScratchPackage(t, "prepared-once", prepare)
	if again, _ := SharedScratchPackage(t, "prepared-once", prepare); again != dir || calls != 1 {
		t.Fatalf("second call got %q after %d prepares, want %q after 1", again, calls, dir)
	}
	var failed string
	msg := fatalOf(t, func(tb testing.TB) {
		SharedScratchPackage(tb, "prepare-fails", func(dir, _ string) error {
			failed = dir
			return errors.New("boom")
		})
	})
	if !strings.Contains(msg, "boom") || failed == "" {
		t.Fatalf("a failed prepare reported %q, want its error", msg)
	}
	removeSharedTrees()
	for _, d := range []string{dir, failed} {
		if _, err := os.Stat(d); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("removeSharedTrees left %s (%v)", d, err)
		}
	}
}

// fatalRecorder records Fatalf and stops the caller the way testing does,
// without failing the real test.
type fatalRecorder struct {
	testing.TB
	msg string
}

func (f *fatalRecorder) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	panic("fatal")
}

func (f *fatalRecorder) Helper() {}
