package service

import (
	"runtime"
	"strings"
	"testing"
)

// A tool's stdout is the value read back (plutil's JSON); a warning it
// prints on stderr must not join that value, though a failure keeps
// both streams for the error.
func TestToolOutputKeepsStderrOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	out, err := execTools{}.output("sh", "-c", `echo '[]'; echo 'warning: slow disk' >&2`)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "[]\n" {
		t.Fatalf("output = %q, want only stdout %q", got, "[]\n")
	}
	out, err = execTools{}.output("sh", "-c", `echo partial; echo 'boom' >&2; exit 3`)
	if err == nil || !strings.Contains(string(out), "boom") || !strings.Contains(string(out), "partial") {
		t.Fatalf("failed tool: out %q, err %v; want both streams and an error", out, err)
	}
}
