// Package scratchdir checks the naming contract the blueprint gate tests rely
// on: a scratch package they generate inside the repo module must stay
// invisible to `go list ./...` and `go build ./...` while it exists.
//
// The gates create the directory and fill it a moment later. A `./...`
// expansion in another test binary during that window used to fail with "no
// Go files in examples/<name>/<scratch>". Go's tooling skips any directory
// whose name begins with "_" or ".", so the gates name their scratch
// directories with a leading underscore. Renaming one without it reopens the
// window, and nothing else would notice until a parallel run flaked.
package scratchdir

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// AssertHiddenFromGoList writes a Go package under parent/name, runs
// `go list ./...` in parent, and fails t if the package shows up. It removes
// its probe, and parent/name too when it created that directory and nothing
// else has written there since.
func AssertHiddenFromGoList(t testing.TB, parent, name string) {
	t.Helper()
	scratch := filepath.Join(parent, name)
	_, statErr := os.Stat(scratch)
	created := errors.Is(statErr, os.ErrNotExist)

	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("probe suffix: %v", err)
	}
	probe := filepath.Join(scratch, "listprobe"+hex.EncodeToString(b[:]))
	if err := os.MkdirAll(probe, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", probe, err)
	}
	// Remove only the probe, then the scratch directory if this call made it
	// and it is empty. A gate in another test binary can create the same
	// directory between the Stat and the MkdirAll; a recursive remove would
	// delete its live files.
	t.Cleanup(func() {
		_ = os.RemoveAll(probe)
		if created {
			_ = os.Remove(scratch)
		}
	})
	if err := fileperm.WriteOwnerOnly(filepath.Join(probe, "probe.go"), []byte("package listprobe\n")); err != nil {
		t.Fatalf("write probe: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "go", "list", "-e", "-f", "{{.Dir}}", "./...")
	cmd.Dir = parent
	cmd.WaitDelay = 10 * time.Second
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list ./... in %s: %v", parent, err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Fatalf("go list ./... in %s listed nothing, so it proves nothing about %s", parent, name)
	}
	if strings.Contains(string(out), filepath.Base(probe)) {
		t.Errorf("go list ./... in %s found the scratch package %s: a scratch directory needs a leading underscore so ./... skips it", parent, name)
	}
}
