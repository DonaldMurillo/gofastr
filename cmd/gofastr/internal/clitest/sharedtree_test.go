package clitest

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
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

// fatalRecorder records Fatalf and stops the caller the way testing does,
// without failing the real test.
type fatalRecorder struct {
	testing.TB
	msg string
}

func (f *fatalRecorder) Fatalf(format string, args ...any) {
	f.msg = format
	panic("fatal")
}

func (f *fatalRecorder) Helper() {}
