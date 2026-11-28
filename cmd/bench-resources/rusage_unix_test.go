//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// A finished Go test binary peaked well over a megabyte; a kilobyte count would read as bytes far below it.
func TestPeakRSSFromStateIsBytes(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if got := peakRSSFromState(cmd.ProcessState); got < 1<<20 {
		t.Fatalf("peakRSSFromState = %d bytes, want at least 1 MiB", got)
	}
	if got := peakRSSFromState(nil); got != 0 {
		t.Fatalf("peakRSSFromState(nil) = %d, want 0", got)
	}
}

func TestNormaliseRSSIsBytes(t *testing.T) {
	want := int64(3 * 1024)
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		want = 3
	}
	if got := normaliseRSS(3); got != want {
		t.Fatalf("normaliseRSS(3) on %s = %d, want %d", runtime.GOOS, got, want)
	}
	if got := normaliseRSS(-1); got != 0 {
		t.Fatalf("normaliseRSS(-1) = %d, want 0", got)
	}
}
