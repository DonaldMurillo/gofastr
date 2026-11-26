//go:build !windows

package main

import (
	"runtime"
	"testing"
)

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
