package main

import (
	"os"
	"os/exec"
	"testing"
)

// A running Go test binary holds well over a megabyte; ps reports kilobytes.
func TestReadRSSIsBytes(t *testing.T) {
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("no ps")
	}
	if got := readRSS(os.Getpid()); got < 1<<20 {
		t.Fatalf("readRSS(self) = %d bytes, want at least 1 MiB", got)
	}
}
