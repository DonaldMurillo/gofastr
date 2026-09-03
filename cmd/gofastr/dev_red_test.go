//go:build red

package main

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: an executable written to shared temp gets a unique, unguessable
// path (CWE-377): predictable names let a local attacker pre-create a symlink
// at the target so the build writes through it (arbitrary-file clobber) or
// swaps the binary between build and exec (code execution as the dev user).
// Surface: cmd/gofastr/dev.go devServerBinaryPath (:238-247) — the rebuilt
// server is compiled to fmt.Sprintf("gofastr-dev-server-%d", os.Getpid())
// under os.TempDir() (plus the isolation ID when active), a fully
// deterministic name; buildAndServe then `go build -o`s onto it (:281-282)
// and execs it (:302) with no CreateTemp/O_EXCL/Lstat guard anywhere between.
// Finding: every input to the path (pid, isolation ID, GOOS, temp root) is
// attacker-known or guessable, so on hosts with a shared temp root (Linux
// /tmp, CI runners, any multi-user box) the path is predictable before the
// build runs. On macOS os.TempDir() is a per-user 0700 dir, which narrows
// the vector to same-user races — the shared-root hosts are the exposure.
// Severity: LOW — dev tooling (`gofastr dev`), local multi-user/CI threat
// model, not a shipped server surface.
// Fix direction: mint an unguessable suffix once per process (crypto/rand or
// os.CreateTemp pattern) for the binary name, or build inside a freshly
// created 0700 per-process dir; keep the shutdown-path cleanup working
// (dev.go:143 os.Remove) by deriving it from the same builder.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

func TestDevServerRedUniqueBinaryPath(t *testing.T) {
	rt, err := isolation.Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("isolation.Resolve: %v", err)
	}

	p := devServerBinaryPath(rt)
	p2 := devServerBinaryPath(rt)
	dir, base := filepath.Dir(p), filepath.Base(p)

	// The name a local attacker can fully predict: every input the builder
	// uses is public (pid, isolation ID, GOOS).
	predicted := fmt.Sprintf("gofastr-dev-server-%d", os.Getpid())
	if rt.Active() {
		predicted += "-" + rt.ID()
	}
	if runtime.GOOS == "windows" {
		predicted += ".exe"
	}

	// Fix-shape A: the builder created its own private dir (0700) instead
	// of dropping the file directly in the shared temp root.
	privateDir := false
	if dir != filepath.Clean(os.TempDir()) {
		if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() && info.Mode().Perm() == 0o700 {
			privateDir = true
		}
	}
	// Fix-shape B: the name carries entropy beyond the predicted inputs
	// (per-process random suffix), observable as a name ≠ prediction and/or
	// a second call already disagreeing with the first.
	unguessable := base != predicted || p != p2

	if !unguessable && !privateDir {
		t.Errorf("SECURITY: [dev-tmp-binary] devServerBinaryPath returned %q — name fully predictable (%s) in the shared temp root with no private 0700 parent and no entropy between calls; "+
			"`go build -o` writes through a pre-planted symlink there (CWE-377 clobber) and the same path is exec'd at dev.go:302, so a local attacker on a shared-temp host swaps the dev server binary",
			p, predicted)
	}
}
