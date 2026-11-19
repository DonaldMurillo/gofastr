package clitest

import (
	"io"
	"os"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

// CovT_capExit runs fn with osExit swapped for a recorder. It returns the
// code passed to the first osExit call (or -1 if osExit was never called).
// Because production osExit calls do not return, the swapped version panics
// with a sentinel to unwind the stack at the call site, mirroring the
// real "execution stops here" semantics without killing the test binary.
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
				return // expected unwind from a captured osExit
			}
			panic(r)
		}
	}()
	fn()
	return code
}

type CovT_exitSentinel struct{}

// CovT_capStdout reads stdout when fn returns and restores it even on panic.
// To capture output and an exit code, call CovT_capExit inside fn.
func CovT_capStdout(t *testing.T, fn func()) string {
	t.Helper()
	return captureOutput(t, &os.Stdout, fn)
}

// CovT_capStderr is CovT_capStdout for stderr.
func CovT_capStderr(t *testing.T, fn func()) string {
	t.Helper()
	return captureOutput(t, &os.Stderr, fn)
}

// CovT_capOutput captures stdout and stderr together, stdout first. For
// tests that assert a refusal's text without caring which stream carried
// it: the ✗ line and usage errors go to stderr (cli.UsageFail), while
// usage blocks and help stay on stdout.
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

// CovT_chdir changes the working directory to dir for the duration of the
// test, restoring the original on cleanup.
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

// CovT_withTTY forces the color helpers down their ANSI branch for the
// duration of fn, restoring stdoutIsTTY afterward.
func CovT_withTTY(fn func()) {
	old := cli.StdoutIsTTY
	cli.StdoutIsTTY = true
	defer func() { cli.StdoutIsTTY = old }()
	fn()
}
