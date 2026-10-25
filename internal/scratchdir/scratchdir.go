// Package scratchdir owns where the app spec gate tests generate their
// scratch packages, and checks the naming contract that keeps those packages
// invisible to `go list ./...` and `go build ./...` while they exist.
//
// The gates generate an app from an app spec and compile it. The generated
// code imports its own packages, so it has to sit inside the repo module for
// those imports to resolve without a go.mod, a replace directive, or a
// network fetch. Every gate generates under Root, one directory per gate.
//
// Root's first element begins with "_". The gates create a directory and fill
// it a moment later; a `./...` expansion in another test binary during that
// window used to fail with "no Go files in". Go's tooling skips any
// directory whose name begins with "_" or ".", at every moment, while still
// resolving an explicit import path through it.
//
// Root is tracked (its README keeps it in every checkout) and never removed,
// and only its children come and go. Go's test cache hashes the directory
// listings a test reads, entry mtimes included. When the gates generated
// beside each example (examples/<name>/_appspec*gen), every package that
// walks the repo saw examples/ and the example directory change during each
// sweep and re-ran on an unchanged tree. Now the only listing that moves is
// Root's own, and the walkers skip "_" directories the way Go does.
package scratchdir

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// Root is where the gates generate, relative to the module root.
const Root = "_scratch/appspecs"

// modulePath is the repo module's path; go.mod declares it.
const modulePath = "github.com/DonaldMurillo/gofastr"

// ModuleRoot returns the nearest directory at or above the working directory
// that holds a go.mod.
func ModuleRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		} else if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("stat %s: %v", filepath.Join(dir, "go.mod"), err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod at or above the working directory")
		}
		dir = parent
	}
}

// Package clears the scratch package Root/name, creates it, and returns its
// absolute directory and import path. Cleanup removes it again, so a killed
// run is the only way one is left behind, and the next run clears it.
//
// It never opens or stats Root itself. Go's test cache records those calls,
// and Root's listing changes whenever another gate's package comes or goes,
// so a gate that touched it would re-run on an unchanged tree. That rules
// out os.MkdirAll (it stats the parent) and os.RemoveAll (it opens it).
func Package(t testing.TB, name string) (dir, importPath string) {
	t.Helper()
	dir, importPath, err := Make(ModuleRoot(t), name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Remove(dir) })
	return dir, importPath
}

// Make is Package for a scratch package that outlives one test: it clears
// and creates moduleRoot/Root/name and returns its directory and import
// path, and the caller removes it with Remove when it is done.
func Make(moduleRoot, name string) (dir, importPath string, err error) {
	dir = filepath.Join(moduleRoot, filepath.FromSlash(Root), name)
	if err := removeTree(dir); err != nil {
		return "", "", fmt.Errorf("clear stale scratch %s: %w", dir, err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("mkdir scratch %s: %w", dir, err)
	}
	return dir, modulePath + "/" + Root + "/" + name, nil
}

// Remove deletes a scratch package Make created, without opening Root.
func Remove(dir string) error {
	return removeTree(dir)
}

// removeTree deletes dir and everything below it, opening only dir and its
// subdirectories. A symlink is removed, never followed, dir itself included.
func removeTree(dir string) error {
	if fi, err := os.Lstat(dir); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return os.Remove(dir)
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if err := removeTree(p); err != nil {
				return err
			}
			continue
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

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
