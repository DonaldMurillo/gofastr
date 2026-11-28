package clitest

import (
	"io"
	"os"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

// CovT_capExit runs fn until its first cli.OsExit and returns that code, or -1.
func CovT_capExit(t *testing.T, fn func()) (code int) {
	t.Helper()
	old := cli.OsExit
	code = -1
	cli.OsExit = func(c int) {
		code = c
		panic(CovT_exitSentinel{})
	}
	defer func() {
		cli.OsExit = old
		if r := recover(); r != nil {
			if _, ok := r.(CovT_exitSentinel); ok {
				return
			}
			panic(r)
		}
	}()
	fn()
	return code
}

type CovT_exitSentinel struct{}

// CovT_capStdout returns what fn wrote to stdout; nest CovT_capExit inside it.
func CovT_capStdout(t *testing.T, fn func()) string {
	t.Helper()
	return captureOutput(t, &os.Stdout, fn)
}

// CovT_capStderr is CovT_capStdout for stderr.
func CovT_capStderr(t *testing.T, fn func()) string {
	t.Helper()
	return captureOutput(t, &os.Stderr, fn)
}

// CovT_capOutput returns what fn wrote to stdout followed by what it wrote to stderr.
func CovT_capOutput(t *testing.T, fn func()) string {
	t.Helper()
	var errOut string
	out := CovT_capStdout(t, func() {
		errOut = CovT_capStderr(t, fn)
	})
	return out + errOut
}

func captureOutput(t *testing.T, stream **os.File, fn func()) string {
	t.Helper()
	old := *stream
	f, err := os.CreateTemp(t.TempDir(), "output-*")
	if err != nil {
		t.Fatal(err)
	}
	*stream = f
	defer func() {
		*stream = old
		_ = f.Close()
	}()
	fn()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// CovT_chdir changes to dir until the test ends.
func CovT_chdir(t *testing.T, dir string) {
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

// CovT_withTTY runs fn with cli.StdoutIsTTY true.
func CovT_withTTY(fn func()) {
	old := cli.StdoutIsTTY
	cli.StdoutIsTTY = true
	defer func() { cli.StdoutIsTTY = old }()
	fn()
}
