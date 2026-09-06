// Pins, found by the 2026-09-04 red-probe round, that the built-binary
// size stat under the bench output dir joined paths lexically only, so
// a symlinked name pointed the measurement at a file outside the dir;
// fixed by routing the stat through an *os.Root, which refuses symlink
// escapes in the kernel.
//
// Property: stats under the bench outDir must stay under it even when
// the name is a symlink pointing outside. A real file still stats.
//
// Surfaces: statUnder (runOne's binary-size step).
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutDirSymlinkStatRefused(t *testing.T) {
	outDir := t.TempDir()
	outside := t.TempDir()

	if err := os.WriteFile(filepath.Join(outside, "fakebin"), []byte("not-a-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "fakebin"), filepath.Join(outDir, "app")); err != nil {
		t.Fatal(err)
	}
	// Symlinked directory component out of outDir.
	if err := os.Symlink(outside, filepath.Join(outDir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	// Control: a real binary under outDir.
	if err := os.WriteFile(filepath.Join(outDir, "real"), []byte("xx"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, ok := statUnder(outDir, "app"); ok {
		t.Error("SECURITY: [bench-rootread] statUnder followed a symlinked name out of outDir")
	}
	if _, ok := statUnder(outDir, "linkdir/fakebin"); ok {
		t.Error("SECURITY: [bench-rootread] statUnder followed a symlinked directory out of outDir")
	}
	if n, ok := statUnder(outDir, "real"); !ok || n != 2 {
		t.Errorf("control: statUnder(real) = %d, %v; want 2, true", n, ok)
	}
}
