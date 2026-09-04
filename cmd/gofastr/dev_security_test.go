package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

// Property: the rebuilt dev-server binary lives at an unguessable path —
// a per-process 0700 temp dir minted with os.MkdirTemp — so a local
// co-user can neither pre-plant a symlink for `go build -o` to write
// through nor swap the binary between build and exec (CWE-377).
func TestDevServerBinaryPathUnguessable(t *testing.T) {
	rt, err := isolation.Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("isolation.Resolve: %v", err)
	}

	p := devServerBinaryPath(rt)
	p2 := devServerBinaryPath(rt)
	dir, base := filepath.Dir(p), filepath.Base(p)

	// The name a local attacker could fully predict if the builder still
	// used only public inputs (pid, isolation ID, GOOS) directly under
	// the shared temp root.
	predicted := fmt.Sprintf("gofastr-dev-server-%d", os.Getpid())
	if rt.Active() {
		predicted += "-" + rt.ID()
	}
	if runtime.GOOS == "windows" {
		predicted += ".exe"
	}

	// Fix-shape A: the builder created its own private dir (0700)
	// instead of dropping the file directly in the shared temp root.
	privateDir := false
	if dir != filepath.Clean(os.TempDir()) {
		if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() && info.Mode().Perm() == 0o700 {
			privateDir = true
		}
	}
	// Fix-shape B: the name carries entropy beyond the predicted inputs,
	// observable as a name ≠ prediction and/or a second call already
	// disagreeing with the first.
	unguessable := base != predicted || p != p2

	if !unguessable && !privateDir {
		t.Errorf("devServerBinaryPath returned %q — name fully predictable (%s) in the shared temp root with no private 0700 parent and no entropy between calls; "+
			"`go build -o` would write through a pre-planted symlink there and the same path is exec'd",
			p, predicted)
	}
}
