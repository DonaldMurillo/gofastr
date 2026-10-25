package main

import (
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/verifycmd"
)

// captureVerify runs the command with stdout redirected and osExit
// intercepted, returning the output and the requested exit code.
func captureVerify(t *testing.T, args []string) (string, int) {
	t.Helper()
	// cmd/affected sets it for the sweep, and verify would read the repo's
	// semantic-coverage manifest instead of the fixture's.
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "")
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", "")
	origExit, origStdout := cli.OsExit, os.Stdout
	code := 0
	cli.OsExit = func(c int) { code = c }
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() {
		cli.OsExit, os.Stdout = origExit, origStdout
	}()

	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			b.Write(buf[:n])
			if readErr != nil {
				break
			}
		}
		done <- b.String()
	}()

	verifycmd.RunVerify(args)
	w.Close()
	out := <-done
	r.Close()
	return out, code
}
