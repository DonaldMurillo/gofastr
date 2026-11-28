package testcmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestRunTestScrubsChildStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Go command uses a POSIX shell")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(`#!/bin/sh
printf '%s\n' '--- PASS: TestX (0.00s)'
printf 'log \033]52;c;cHduZWQ=\007 \033[2J line\n'
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunTest(nil) })
	})
	if code != -1 {
		t.Fatalf("exit %d, want none: %q", code, out)
	}
	if strings.ContainsAny(out, "\x1b\a") {
		t.Errorf("a control byte from the child's stdout reached the terminal: %q", out)
	}
	if !strings.Contains(out, "line") {
		t.Errorf("the scrubbed line lost its text: %q", out)
	}
}

func TestRunTestUnknownFlagExitsTwo(t *testing.T) {
	var code int
	clitest.CovT_capOutput(t, func() {
		code = clitest.CovT_capExit(t, func() { RunTest([]string{"--count=1"}) })
	})
	if code != 2 {
		t.Errorf("exit %d, want 2 for a usage error", code)
	}
}
