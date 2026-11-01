package update

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	// Identifier returns the running app's CFBundleIdentifier. ""
	// when unbundled; an update bundle must declare the same one.
	Identifier() string
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
// The fast path is a rename that refuses an existing dst in the same
// syscall; when the two are on different volumes (staging under the
// app-data dir, bundle under /Applications) the rename fails and the
// tree is copied, every create exclusive, and the source removed.
func MoveTree(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return errMoveDestExists
	} else if !os.IsNotExist(err) {
		return err
	}
	err := renameNoReplace(src, dst)
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrExist) { // EEXIST or ENOTEMPTY
		return errMoveDestExists
	}
	if err := copyTree(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// copyTree recursively copies src to dst preserving file modes. Every
// directory and file is created exclusively: a dst that appeared after
// MoveTree's absence check stops the copy instead of taking a merge.
func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		// Owner-only and writable while the children are copied; the
		// extractor's directory mode (dirPerm) lands after.
		if err := os.Mkdir(dst, 0o700); err != nil {
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
		return os.Chmod(dst, dirPerm(info.Mode()))
	}
	if !info.Mode().IsRegular() {
		// A symlink or device in a bundle we just extracted and
		// verified cannot exist (the extractor refuses them); refuse
		// here too rather than duplicating whatever it is.
		return errNotRegular
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(info.Mode().Perm()); err != nil {
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
		tail := relativeToArgs(string(out), args)
		if len(tail) > 512 {
			tail = tail[len(tail)-512:]
		}
		return fmt.Errorf("%s failed: %v: %s", name, err, strings.TrimSpace(tail))
	}
	return nil
}

// relativeToArgs rewrites every absolute-path argument in a command's
// output to its base name. codesign and open print the bundle path they
// were given ("/Users/ann/Library/.../Notes.app: invalid signature"),
// and the log must not carry the user's home directory, so the line
// becomes "Notes.app: invalid signature" with nested files left
// relative to the bundle.
func relativeToArgs(out string, args []string) string {
	for _, a := range args {
		if a == "" || !filepath.IsAbs(a) {
			continue
		}
		out = strings.ReplaceAll(out, filepath.Clean(a), filepath.Base(a))
	}
	return out
}

// Fixed-text errors MoveTree and copyTree return; LogDetail passes
// their text through because it names no path.
var (
	errMoveDestExists = errors.New("MoveTree: destination already exists")
	errNotRegular     = errors.New("copyTree: a bundle entry is not a regular file")
)

// LogDetail reduces an error from a file-system call to text a log line
// may carry: the operation and the OS error ("rename: permission
// denied") of an *fs.PathError, *os.LinkError or *os.SyscallError, never
// the paths they hold, which name the user's home directory. Any other
// error collapses to fixed words, since its text may embed a path.
func LogDetail(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + ": " + pe.Err.Error()
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Op + ": " + le.Err.Error()
	}
	var se *os.SyscallError
	if errors.As(err, &se) {
		return se.Syscall + ": " + se.Err.Error()
	}
	for _, fixed := range []error{errMoveDestExists, errNotRegular} {
		if errors.Is(err, fixed) {
			return fixed.Error()
		}
	}
	return "not a file-system error"
}
