package main

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestNormaliseRSSIsBytes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Maxrss on windows")
	}
	want := int64(3)
	if runtime.GOOS == "linux" {
		want = 3 * 1024
	}
	if got := normaliseRSS(3); got != want {
		t.Fatalf("normaliseRSS(3) on %s = %d, want %d", runtime.GOOS, got, want)
	}
	if got := normaliseRSS(-1); got != 0 {
		t.Fatalf("normaliseRSS(-1) = %d, want 0", got)
	}
}

// A running Go test binary holds well over a megabyte; ps reports kilobytes.
func TestReadRSSIsBytes(t *testing.T) {
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("no ps")
	}
	if got := readRSS(os.Getpid()); got < 1<<20 {
		t.Fatalf("readRSS(self) = %d bytes, want at least 1 MiB", got)
	}
}
