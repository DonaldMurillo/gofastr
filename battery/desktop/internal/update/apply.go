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
	// Output runs the command and returns its standard output. The
	// macOS signature pin reads the running bundle's designated
	// requirement this way.
	Output(name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands through os/exec, capturing the combined
// output for the error detail (the caller logs it server-side; the
// page never sees it).
type ExecRunner struct{}

// Run implements CommandRunner.
func (ExecRunner) Run(name string, args ...string) error {
	return runCommand(name, args...)
}

// Output implements CommandRunner. Standard error is dropped: codesign
// prints the bundle's absolute path there, and the failure is reported
// by exit status alone.
func (ExecRunner) Output(name string, args ...string) ([]byte, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("%s failed", name)
	}
	return out, nil
}

// SignaturePolicy is the macOS code-signing pin an install applies to
// the downloaded bundle (UpdateConfig.MacOSTeamID and MacOSAllowAdHoc).
// The zero value pins the update to the running app's own designated
// requirement and refuses when the running app has no signing identity.
type SignaturePolicy struct {
	// TeamID, when set, is the Apple Developer Team ID every update
	// must be signed by, instead of the running app's requirement.
	TeamID string
	// AllowAdHoc lets an app with no signing identity (ad-hoc signed or
	// unsigned) install an update that only carries a valid signature
	// of any kind. The install then rests on the feed signature alone.
	AllowAdHoc bool
}

// Platform is the OS seam of the updater: where the running bundle
// lives, what version it reports, how its replacement proves its
// signature, and how the new copy is launched. darwin swaps one .app
// directory; windows/amd64 swaps the executable and its loader DLL as a
// set (it also implements FileLayout). Every other host gets
// DefaultPlatform's unsupported answer.
type Platform interface {
	// Version returns the running app's version (the bundle's
	// CFBundleShortVersionString on darwin, the executable's
	// ProductVersion on windows). "" when unbundled, and an empty
	// version never updates.
	Version() string
	// Identifier returns the running app's CFBundleIdentifier. ""
	// when unbundled; an update bundle must declare the same one.
	Identifier() string
	// Bundle returns the running bundle's file name ("Notes.app") and
	// its parent directory. ok is false when the process is not
	// running from a bundle.
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

// Unit is one path an install replaces. Rel names the new copy inside
// the extracted bundle root ("" is the root itself); Current is the
// installed path it replaces.
type Unit struct {
	Rel     string
	Current string
}

// FileLayout is the optional Platform extension for a host whose
// install replaces files beside the running executable instead of one
// bundle directory (Windows: the .exe and WebView2Loader.dll). A
// Platform without it installs the macOS way: the extracted root
// replaces <dir>/<name>, and its identity comes from Info.plist.
type FileLayout interface {
	// Units lists the installed paths the swap replaces, in order; the
	// first is the one Relaunch starts. ok is false when not bundled.
	Units() (units []Unit, ok bool)
	// Inspect checks that the extracted root holds exactly the files
	// Units names and returns the identity it declares.
	Inspect(extracted string) (BundleInfo, error)
}
