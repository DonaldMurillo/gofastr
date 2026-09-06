package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrUnsupportedApply is the sentinel the default (non-darwin or
// unbundled) platform answers with. battery/desktop maps it onto the
// bridge's unsupported code.
var ErrUnsupportedApply = errors.New("auto-update is not supported by this host")

// CommandRunner runs one external command to completion and reports
// its exit. ExecRunner is the real one over os/exec; tests install a
// fake to observe the codesign invocation without codesign present.
type CommandRunner interface {
	Run(name string, args ...string) error
}

// ExecRunner runs commands through os/exec, capturing the combined
// output for the error detail (the caller logs it server-side; the
// page never sees it).
type ExecRunner struct{}

// Run implements CommandRunner.
func (ExecRunner) Run(name string, args ...string) error {
	return runCommand(name, args...)
}

// Platform is the OS seam of the updater: where the running bundle
// lives, what version it reports, how its replacement proves its
// signature, and how the new copy is launched. One darwin
// implementation exists; every other host gets DefaultPlatform's
// unsupported answer.
type Platform interface {
	// Version returns the running app's version (the bundle's
	// CFBundleShortVersionString on darwin). "" when unbundled, and an
	// empty version never updates.
	Version() string
	// Bundle returns the running bundle's file name ("Notes.app") and
	// its parent directory. ok is false when the process is not
	// running from a bundle.
	Bundle() (name, dir string, ok bool)
	// VerifySignature runs codesign --verify --deep --strict on the
	// extracted bundle and refuses a failed check.
	VerifySignature(appDir string) error
	// Relaunch opens the newly installed bundle (open -n) so the new
	// version starts before the current process quits.
	Relaunch(appDir string) error
}

// MoveTree moves src (a directory tree) to dst, which must not exist.
// The fast path is os.Rename; when the two are on different volumes
// (staging under the app-data dir, bundle under /Applications) the
// rename fails and the tree is copied with modes preserved and the
// source removed.
func MoveTree(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// copyTree recursively copies src to dst preserving file modes.
func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		// A symlink or device in a bundle we just extracted and
		// verified cannot exist (the extractor refuses them); refuse
		// here too rather than duplicating whatever it is.
		return fmt.Errorf("copyTree: %s is not a regular file", "bundle entry")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// runCommand executes name with args, capturing combined output; a
// non-zero exit reports the exit status and the output tail (the tail
// is for the server log only; callers keep it out of bridge errors).
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		tail := string(out)
		if len(tail) > 512 {
			tail = tail[len(tail)-512:]
		}
		return fmt.Errorf("%s failed: %v: %s", name, err, strings.TrimSpace(tail))
	}
	return nil
}
