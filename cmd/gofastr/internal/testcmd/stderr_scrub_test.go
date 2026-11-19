package testcmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A malformed import path spells an ESC byte into `go test`'s build
// diagnostics, which arrive on the child's stderr. That copy must pass
// through the terminal scrub: the control byte never reaches the
// terminal, the message text does.
func TestRunTestScrubsChildStderr(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module stderrmod\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "package x\n\nimport (\n\t\"testing\"\n\t\"\\x1bbad\"\n)\n\nfunc TestStderr(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	code := -1
	stderr := clitest.CovT_capStderr(t, func() {
		clitest.CovT_capStdout(t, func() {
			code = clitest.CovT_capExit(t, func() { RunTest(nil) })
		})
	})
	if code != 1 {
		t.Fatalf("failing tests should exit 1, got %d: %s", code, stderr)
	}
	if strings.Contains(stderr, "\x1b") {
		t.Fatalf("raw ESC from child stderr reached the terminal: %q", stderr)
	}
	if !strings.Contains(stderr, "bad") {
		t.Fatalf("scrubbed stderr lost the diagnostic text: %q", stderr)
	}
}

func TestRunTestFlushesUnterminatedChildStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Go command uses a POSIX shell")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(`#!/bin/sh
printf 'partial\033 diagnostic' >&2
exit "$GOFASTR_TEST_CHILD_STATUS"
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	for _, tc := range []struct {
		name      string
		childCode string
		wantExit  int
	}{
		{"success", "0", -1},
		{"failure", "7", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOFASTR_TEST_CHILD_STATUS", tc.childCode)
			var code int
			stderr := clitest.CovT_capStderr(t, func() {
				clitest.CovT_capStdout(t, func() {
					code = clitest.CovT_capExit(t, func() { RunTest(nil) })
				})
			})
			if code != tc.wantExit {
				t.Errorf("exit = %d, want %d", code, tc.wantExit)
			}
			if !strings.HasPrefix(stderr, "partial diagnostic") {
				t.Errorf("stderr = %q, want scrubbed unterminated diagnostic", stderr)
			}
		})
	}
}
