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

// ErrUnsupportedApply is the sentinel the default (non-darwin or unbundled) platform answers with.
var ErrUnsupportedApply = errors.New("auto-update is not supported by this host")

// CommandRunner runs one external command to completion and reports its exit.
type CommandRunner interface {
	Run(name string, args ...string) error
	// Output runs the command and returns its standard output.
	Output(name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands through os/exec, capturing the combined output for the error detail (the caller logs it server-side; the page never sees it).
type ExecRunner struct{}

// Run implements CommandRunner.
func (ExecRunner) Run(name string, args ...string) error {
	return runCommand(name, args...)
}

// ExitError is a command that ran to completion and exited non-zero.
type ExitError struct {
	Name   string
	Code   int
	Stderr string
}

func (e *ExitError) Error() string { return fmt.Sprintf("%s exited %d", e.Name, e.Code) }

// Output implements CommandRunner.
func (ExecRunner) Output(name string, args ...string) ([]byte, error) {
	out, err := exec.Command(name, args...).Output()
	if err == nil {
		return out, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.Exited() {
		return nil, &ExitError{Name: name, Code: exit.ExitCode(), Stderr: string(exit.Stderr)}
	}
	return nil, fmt.Errorf("%s could not run", name)
}

// SignaturePolicy is the macOS code-signing pin an install applies to the downloaded bundle (UpdateConfig.MacOSTeamID and MacOSAllowAdHoc).
type SignaturePolicy struct {
	// TeamID, when set, is the Apple Developer Team ID whose Developer
	// ID certificate every update must be signed with, instead of the
	// running app's requirement.
	TeamID string
	// AllowAdHoc lets an app with no signing identity (ad-hoc signed or unsigned) install an update that only carries a valid signature of any kind.
	AllowAdHoc bool
}

// Platform is the OS seam of the updater: where the running bundle
// lives, what version it reports, how its replacement proves its
// signature, and how the new copy is launched.
type Platform interface {
	// Version returns the running app's version (the bundle's CFBundleShortVersionString on darwin, the executable's ProductVersion on windows).
	Version() string
	// Identifier returns the running app's CFBundleIdentifier.
	Identifier() string
	// Bundle returns the running bundle's file name ("Notes.app") and its parent directory.
	Bundle() (name, dir string, ok bool)
	// VerifySignature checks the extracted bundle's code signature and
	// refuses a failed check: codesign --verify --deep --strict against
	// the SignaturePolicy's requirement on darwin, Authenticode under
	// CheckWindowsTrust on windows.
	VerifySignature(appDir string) error
	// Relaunch starts the newly installed copy (open -n on darwin, the
	// new executable with PredecessorEnv set on windows) so the new
	// version starts before the current process quits.
	Relaunch(appDir string) error
}

// MoveTree moves src (a directory tree) to dst, which must not exist.
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

func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
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

func relativeToArgs(out string, args []string) string {
	for _, a := range args {
		if a == "" || !filepath.IsAbs(a) {
			continue
		}
		out = strings.ReplaceAll(out, filepath.Clean(a), filepath.Base(a))
	}
	return out
}

var (
	errMoveDestExists = errors.New("MoveTree: destination already exists")
	errNotRegular     = errors.New("copyTree: a bundle entry is not a regular file")
)

// LogDetail reduces an error from a file-system call to text a log line
// may carry: the operation and the OS error ("rename: permission
// denied") of an *fs.PathError, *os.LinkError or *os.SyscallError, never
// the paths they hold, which name the user's home directory.
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

// Unit is one path an install replaces.
type Unit struct {
	Rel     string
	Current string
}

// FileLayout is the optional Platform extension for a host whose
// install replaces files beside the running executable instead of one
// bundle directory (Windows: the .exe and WebView2Loader.dll).
type FileLayout interface {
	// Units lists the installed paths the swap replaces, in order; the first is the one Relaunch starts.
	Units() (units []Unit, ok bool)
	// Inspect checks that the extracted root holds exactly the files Units names and returns the identity it declares.
	Inspect(extracted string) (BundleInfo, error)
}
