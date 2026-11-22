package cliplugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const (
	maxManifestStdout = 256 << 10 // 256 KiB
	maxManifestStderr = 64 << 10  // 64 KiB
)

// ProbeTimeout bounds the manifest handshake call.
// Var so tests can tune it deterministically.
var ProbeTimeout = 5 * time.Second

// ProbeWaitDelay bounds Wait() after the manifest child exits if a grandchild
// holds stdout/stderr pipes open. Var so security tests can assert it fires.
var ProbeWaitDelay = 2 * time.Second

// ResolveExecutable validates and resolves command[0] to an absolute executable
// path without invoking a shell.
//
// When confineToBase is true (used for project-declared plugins in
// gofastr.settings.yml), command must be a single direct executable path
// confined inside baseDir (no bare PATH launchers like "sh" or "go", no extra
// mutable script/argument entries, and no traversal or symlink escape outside
// baseDir) so the SHA-256 trust pin covers the exact executable artifact.
func ResolveExecutable(command []string, baseDir string, confineToBase bool) ([]string, bool, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return nil, false, fmt.Errorf("%w: command must not be empty", ErrInvalidPath)
	}
	rawExe := command[0]
	if strings.ContainsRune(rawExe, 0) {
		return nil, false, fmt.Errorf("%w: command path contains NUL byte", ErrInvalidPath)
	}
	for i, arg := range command[1:] {
		if strings.ContainsRune(arg, 0) {
			return nil, false, fmt.Errorf("%w: command[%d] contains NUL byte", ErrInvalidPath, i+1)
		}
	}

	hasSep := strings.ContainsRune(rawExe, '/') || strings.ContainsRune(rawExe, '\\') || strings.HasPrefix(rawExe, ".")
	if confineToBase {
		if len(command) != 1 {
			return nil, false, fmt.Errorf("%w: project plugin command must be a single direct executable path (no launcher or extra arguments)", ErrInvalidPath)
		}
		if !hasSep {
			return nil, false, fmt.Errorf("%w: project plugin command %q must be a direct path inside the project root, not a PATH launcher", ErrInvalidPath, textsafe.ScrubTerminal(rawExe))
		}
	}

	var resolved string
	var isProjectLocal bool

	if !hasSep {
		looked, err := exec.LookPath(rawExe)
		if err != nil {
			return nil, false, fmt.Errorf("%w: executable %q not found: %v", ErrInvalidPath, textsafe.ScrubTerminal(rawExe), err)
		}
		abs, err := filepath.Abs(looked)
		if err != nil {
			return nil, false, fmt.Errorf("%w: resolve %q: %v", ErrInvalidPath, textsafe.ScrubTerminal(rawExe), err)
		}
		resolved = abs
	} else {
		candidate := rawExe
		if !filepath.IsAbs(candidate) {
			if baseDir == "" {
				var err error
				baseDir, err = os.Getwd()
				if err != nil {
					return nil, false, fmt.Errorf("%w: working directory: %v", ErrInvalidPath, err)
				}
			}
			candidate = filepath.Join(baseDir, candidate)
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return nil, false, fmt.Errorf("%w: resolve %q: %v", ErrInvalidPath, textsafe.ScrubTerminal(rawExe), err)
		}
		resolved = abs
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %q: %v", ErrInvalidPath, textsafe.ScrubTerminal(resolved), err)
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%w: %q must be a regular executable file", ErrInvalidPath, textsafe.ScrubTerminal(resolved))
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return nil, false, fmt.Errorf("%w: %q is not executable (mode %04o)", ErrInvalidPath, textsafe.ScrubTerminal(resolved), info.Mode().Perm())
	}

	realExe, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return nil, false, fmt.Errorf("%w: resolve symlinks for %q: %v", ErrInvalidPath, textsafe.ScrubTerminal(resolved), err)
	}

	if baseDir != "" {
		realBase, err := filepath.EvalSymlinks(baseDir)
		if err == nil {
			if withinDir(realBase, realExe) {
				isProjectLocal = true
			} else if confineToBase {
				return nil, false, fmt.Errorf("%w: project plugin path %q resolves outside project root %q",
					ErrInvalidPath, textsafe.ScrubTerminal(rawExe), textsafe.ScrubTerminal(baseDir))
			}
		} else if confineToBase {
			return nil, false, fmt.Errorf("%w: resolve project root %q: %v", ErrInvalidPath, textsafe.ScrubTerminal(baseDir), err)
		}
	}

	out := make([]string, len(command))
	out[0] = realExe
	copy(out[1:], command[1:])
	return out, isProjectLocal, nil
}

func withinDir(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// HashFile computes the lowercase hex SHA-256 digest of the regular file at path.
func HashFile(path string) (string, error) {
	f, err := openRegularReadOnly(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SnapshotExecutable copies the regular file at path into a private owner-only
// temporary directory (0700) while computing its SHA-256 digest in a single
// pass. Callers execute the returned snapshot path and invoke cleanup when done
// so neither probing nor running can race against writes inside a checkout.
func SnapshotExecutable(path string) (snapPath string, digest string, cleanup func(), err error) {
	src, err := openRegularReadOnly(path)
	if err != nil {
		if errors.Is(err, ErrInvalidPath) {
			return "", "", nil, err
		}
		return "", "", nil, fmt.Errorf("%w: open %q: %v", ErrInvalidPath, textsafe.ScrubTerminal(path), err)
	}
	defer src.Close()

	tmpDir, err := os.MkdirTemp("", "gofastr-plugin-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("create private plugin temp dir: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpDir, 0o700); err != nil {
			_ = os.RemoveAll(tmpDir)
			return "", "", nil, err
		}
	}
	name := "plugin"
	if ext := filepath.Ext(path); ext != "" {
		name += ext
	} else if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dstPath := filepath.Join(tmpDir, name)
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", "", nil, err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, h), src); err != nil {
		_ = dst.Close()
		_ = os.RemoveAll(tmpDir)
		return "", "", nil, err
	}
	if err := dst.Close(); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", "", nil, err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dstPath, 0o700); err != nil {
			_ = os.RemoveAll(tmpDir)
			return "", "", nil, err
		}
	}
	return dstPath, hex.EncodeToString(h.Sum(nil)), func() { _ = os.RemoveAll(tmpDir) }, nil
}

// PrepareVerifiedExec returns the command vector to probe and execute for rp
// along with a cleanup callback. For project-trusted plugins pinned by SHA-256,
// it snapshots the executable into a private 0700 temp file outside the
// checkout and verifies its digest in one pass, eliminating TOCTOU between
// verification, probing, and execution.
func PrepareVerifiedExec(rp ResolvedPlugin) ([]string, func(), error) {
	if rp.Status != StatusReady {
		if rp.Err != nil {
			return nil, nil, rp.Err
		}
		return nil, nil, fmt.Errorf("plugin %q is not ready (%s)", rp.Name, rp.Status)
	}
	if len(rp.Command) == 0 || rp.Command[0] == "" {
		return nil, nil, fmt.Errorf("%w: empty plugin command", ErrInvalidPath)
	}
	if rp.Source != SourceProject && rp.SHA256 == "" {
		return append([]string(nil), rp.Command...), func() {}, nil
	}
	if rp.SHA256 == "" {
		return nil, nil, fmt.Errorf("%w: project plugin %q has no SHA-256 trust digest", ErrTrustMismatch, rp.Name)
	}
	snapPath, digest, cleanup, err := SnapshotExecutable(rp.Command[0])
	if err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(digest, rp.SHA256) {
		cleanup()
		return nil, nil, fmt.Errorf("%w: project plugin %q executable changed since it was trusted; run `gofastr plugin trust %s` to approve the updated binary",
			ErrTrustMismatch, rp.Name, rp.Name)
	}
	cmd := append([]string(nil), rp.Command...)
	cmd[0] = snapPath
	return cmd, cleanup, nil
}

// Probe invokes command with HandshakeFlag in a minimal, secret-free
// environment and decodes and validates the returned Manifest.
func Probe(ctx context.Context, command []string, workDir string) (Manifest, error) {
	if len(command) == 0 || command[0] == "" {
		return Manifest{}, fmt.Errorf("%w: empty plugin command", ErrInvalidPath)
	}
	if ProbeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ProbeTimeout)
		defer cancel()
	}

	args := make([]string, 0, len(command))
	args = append(args, command[1:]...)
	args = append(args, HandshakeFlag)

	cmd := exec.CommandContext(ctx, command[0], args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = BuildProbeEnv(nil)
	cmd.WaitDelay = ProbeWaitDelay

	stdout := &cappedBuffer{limit: maxManifestStdout}
	stderr := &cappedBuffer{limit: maxManifestStderr}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	runErr := cmd.Run()
	if runErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Manifest{}, fmt.Errorf("%w: manifest probe timed out after %s", ErrInvalidManifest, ProbeTimeout)
		}
		detail := strings.TrimSpace(textsafe.ScrubTerminal(string(stderr.Bytes())))
		if detail != "" {
			return Manifest{}, fmt.Errorf("%w: manifest probe failed (%v): %s", ErrInvalidManifest, runErr, detail)
		}
		return Manifest{}, fmt.Errorf("%w: manifest probe failed: %v", ErrInvalidManifest, runErr)
	}
	if stdout.truncated {
		return Manifest{}, fmt.Errorf("%w: manifest response exceeded %d byte limit", ErrInvalidManifest, stdout.limit)
	}
	body := bytes.TrimSpace(stdout.Bytes())
	if len(body) == 0 {
		return Manifest{}, fmt.Errorf("%w: plugin returned empty manifest", ErrInvalidManifest)
	}

	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("%w: decode manifest JSON: %v", ErrInvalidManifest, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Manifest{}, fmt.Errorf("%w: trailing content after manifest JSON", ErrInvalidManifest)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); room > 0 {
		if len(p) <= room {
			return c.buf.Write(p)
		}
		if _, err := c.buf.Write(p[:room]); err != nil {
			return 0, err
		}
	}
	c.truncated = true
	return len(p), nil
}

func (c *cappedBuffer) Bytes() []byte { return c.buf.Bytes() }
