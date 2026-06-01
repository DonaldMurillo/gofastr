package main

import (
	"io"
	"os"
	"testing"
)

// covT_capExit runs fn with osExit swapped for a recorder. It returns the
// code passed to the first osExit call (or -1 if osExit was never called).
// Because production osExit calls do not return, the swapped version panics
// with a sentinel to unwind the stack at the call site, mirroring the
// real "execution stops here" semantics without killing the test binary.
func covT_capExit(t *testing.T, fn func()) (code int) {
	t.Helper()
	old := osExit
	code = -1
	osExit = func(c int) {
		code = c
		panic(covT_exitSentinel{})
	}
	defer func() {
		osExit = old
		if r := recover(); r != nil {
			if _, ok := r.(covT_exitSentinel); ok {
				return // expected unwind from a captured osExit
			}
			panic(r)
		}
	}()
	fn()
	return code
}

type covT_exitSentinel struct{}

// covT_capStdout captures everything fn writes to os.Stdout. It backs the
// capture with a temp file (not a pipe) so a panicking fn — e.g. one that
// trips a captured osExit — can never leave a blocked reader goroutine
// behind. Output is restored and read on the deferred path regardless.
func covT_capStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	f, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = f
	defer func() { os.Stdout = old }()
	fn()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return string(b)
}

// covT_chdir changes the working directory to dir for the duration of the
// test, restoring the original on cleanup.
func covT_chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// covT_withTTY forces the color helpers down their ANSI branch for the
// duration of fn, restoring stdoutIsTTY afterward.
func covT_withTTY(fn func()) {
	old := stdoutIsTTY
	stdoutIsTTY = true
	defer func() { stdoutIsTTY = old }()
	fn()
}
