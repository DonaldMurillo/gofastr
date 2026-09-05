package evalrunner

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Property: a NEGATIVE caller-supplied duration must never fold onto the
// default arm of a `<= 0` check. runCodex tested inv.Timeout `<= 0` and
// substituted 30 minutes, so a negative timeout (sign or unit error)
// silently granted the agent process the LONGEST wall clock instead of
// the tight bound the caller computed.
// Surfaces: runCodex (codexInvocation.Timeout, builder + judge runs).
// Pins inv.Timeout <= 0 folding onto the 30m default, found by the
// 2026-09-04 red-probe round; fixed in runCodex returning an error for
// Timeout < 0 while 0 keeps the default.

func TestRunCodexNegativeTimeoutRejected(t *testing.T) {
	log := filepath.Join(t.TempDir(), "codex.log")
	err := runCodex(context.Background(), codexInvocation{
		Program: "/bin/sh",
		Args:    []string{"-c", "exit 0"},
		LogPath: log,
		Timeout: -time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "must be >= 0") {
		t.Fatalf("runCodex: negative Timeout silently folded onto the 30m default: %v", err)
	}
}

func TestRunCodexZeroTimeoutKeepsDefault(t *testing.T) {
	log := filepath.Join(t.TempDir(), "codex.log")
	if err := runCodex(context.Background(), codexInvocation{
		Program: "/bin/sh",
		Args:    []string{"-c", "exit 0"},
		LogPath: log,
	}); err != nil {
		t.Fatalf("zero Timeout must keep the 30m default and run: %v", err)
	}
}
