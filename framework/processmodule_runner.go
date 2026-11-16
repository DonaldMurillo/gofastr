package framework

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Runner spawns a process module's child. It verifies the artifact digest before
// exec and wires stdio; the supervisor does the handshake.
type Runner interface {
	// Start spawns the child. ctx bounds the spawn, not the child's lifetime.
	Start(ctx context.Context, spec ChildSpec) (RunningChild, error)
}

// ChildSpec is the supervisor's spawn request; every field is host-derived.
type ChildSpec struct {
	Descriptor      ProcessModuleDescriptor
	InstanceID      string
	EffectiveGrants []access.Permission
	Generation      uint64

	// Stderr receives the child's stderr; nil makes a default-size sink.
	Stderr *moduleproto.RingSink

	// ExtraEnv holds KEY=VALUE entries added to the allowlisted env; they win on collision.
	ExtraEnv []string

	// InheritEnv names host variables copied beyond the allowlist.
	InheritEnv []string

	// ScratchDir is the child's working dir; empty makes one the runner removes on reap.
	ScratchDir string
}

// RunningChild is a spawned child. Teardown is CloseStdin, a deadline, Kill, Wait.
type RunningChild interface {
	Codec() *moduleproto.Codec
	Stderr() *moduleproto.RingSink

	// Pid returns the child's pid, or -1 before it started.
	Pid() int

	// ProcessGroup returns the child's Unix process group id, or 0.
	ProcessGroup() int

	// CloseStdin closes the child's stdin. It is idempotent.
	CloseStdin() error

	// Kill sends SIGKILL to the child's process group, or the child. It does not wait.
	Kill() error

	// Wait reaps the child and kills what is left of its process group. It is safe
	// to call concurrently.
	Wait() error
}

// TrustedProcessRunner launches trusted modules: digest pin, allowlisted env, own
// process group and scratch dir, but no confinement.
type TrustedProcessRunner struct {
	// EnvAllowlist names the host variables the child gets; nil uses
	// [DefaultChildEnvAllowlist].
	EnvAllowlist []string

	// NewCodec builds the protocol codec; nil uses [moduleproto.NewCodec].
	NewCodec func(r io.Reader, w io.Writer, maxFrameBytes int) (*moduleproto.Codec, error)
}

// DefaultChildEnvAllowlist returns a fresh copy of the host variables a child gets
// by default, none of them secrets.
func DefaultChildEnvAllowlist() []string {
	allow := append([]string(nil), defaultChildEnvAllowlist...)
	if runtime.GOOS == "windows" {
		allow = append(allow, "SYSTEMROOT", "COMSPEC", "PATHEXT", "TEMP", "TMP", "USERPROFILE")
	}
	return allow
}

// Start implements [Runner].
func (r *TrustedProcessRunner) Start(ctx context.Context, spec ChildSpec) (RunningChild, error) {
	prep, err := prepareChildForSpawn(spec, r.allowlist())
	if err != nil {
		return nil, err
	}
	return startPreparedChild(ctx, prep, r.NewCodec)
}

type childPrep struct {
	Cmd        *exec.Cmd
	Stdin      io.WriteCloser
	Stdout     io.ReadCloser
	StderrPipe io.ReadCloser
	Scratch    string
	OwnScratch bool
	Stderr     *moduleproto.RingSink
	Spec       ChildSpec
	Artifact   *os.File
	ChildEnds  []*os.File
}

const fdExecSupported = runtime.GOOS == "linux"

const artifactChildFd = 3

func prepareChildForSpawn(spec ChildSpec, allowlist []string) (*childPrep, error) {
	d := spec.Descriptor
	if d.ArtifactPath == "" || d.ArtifactSHA256 == "" {
		return nil, errors.New("processmodule: runner: descriptor artifact path/sha256 required")
	}
	artifact, err := os.Open(d.ArtifactPath)
	if err != nil {
		return nil, fmt.Errorf("processmodule: open %s: %w", d.ArtifactPath, err)
	}
	got, err := sha256OfReader(artifact)
	if err != nil {
		_ = artifact.Close()
		return nil, fmt.Errorf("processmodule: hash %s: %w", d.ArtifactPath, err)
	}
	if got != asciicase.Lower(d.ArtifactSHA256) {
		_ = artifact.Close()
		return nil, &ExecutableSHAMismatchError{
			Path: d.ArtifactPath, Expected: d.ArtifactSHA256, Actual: got,
		}
	}

	scratch := spec.ScratchDir
	if scratch == "" {
		scratch = filepath.Join(os.TempDir(),
			fmt.Sprintf("gofastr-module-%s-%s", safeDirName(d.Name), shortID(spec.InstanceID)))
	}
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		_ = artifact.Close()
		return nil, fmt.Errorf("processmodule: scratch dir %s: %w", scratch, err)
	}
	ownScratch := spec.ScratchDir == ""
	prepared := false
	defer func() {
		if !prepared && ownScratch {
			_ = os.RemoveAll(scratch)
		}
	}()

	stderr := spec.Stderr
	if stderr == nil {
		stderr = moduleproto.NewRingSink(moduleproto.DefaultRingSinkBytes)
	}

	var cmd *exec.Cmd
	var execFD *os.File
	if fdExecSupported {
		execFD = artifact
		cmd = &exec.Cmd{
			Path: fmt.Sprintf("/dev/fd/%d", artifactChildFd),
			Args: []string{d.ArtifactPath},
		}
		cmd.ExtraFiles = []*os.File{artifact}
	} else {
		_ = artifact.Close()
		cmd = exec.Command(d.ArtifactPath)
	}
	cmd.Dir = scratch
	cmd.Env = buildChildEnv(allowlist, spec.ExtraEnv, spec.InheritEnv)
	setChildProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = execFD.Close()
		return nil, fmt.Errorf("processmodule: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		_ = execFD.Close()
		return nil, fmt.Errorf("processmodule: stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = execFD.Close()
		return nil, fmt.Errorf("processmodule: stderr pipe: %w", err)
	}

	var childEnds []*os.File
	for _, end := range []any{cmd.Stdin, cmd.Stdout, cmd.Stderr} {
		if f, ok := end.(*os.File); ok {
			childEnds = append(childEnds, f)
		}
	}

	prepared = true
	return &childPrep{
		Cmd:        cmd,
		Stdin:      stdin,
		Stdout:     stdout,
		StderrPipe: stderrPipe,
		Scratch:    scratch,
		OwnScratch: ownScratch,
		Stderr:     stderr,
		Spec:       spec,
		Artifact:   execFD,
		ChildEnds:  childEnds,
	}, nil
}

func startPreparedChild(ctx context.Context, prep *childPrep, newCodec func(r io.Reader, w io.Writer, maxFrameBytes int) (*moduleproto.Codec, error)) (RunningChild, error) {
	cmd := prep.Cmd
	started := false
	defer func() {
		if !started {
			prep.removeOwnedScratch()
		}
	}()
	if prep.Artifact == nil && !fdExecSupported {
		d := prep.Spec.Descriptor
		got, err := sha256OfFile(d.ArtifactPath)
		if err != nil {
			releasePrepFDs(prep)
			return nil, fmt.Errorf("processmodule: re-hash %s: %w", d.ArtifactPath, err)
		}
		if got != asciicase.Lower(d.ArtifactSHA256) {
			releasePrepFDs(prep)
			return nil, &ExecutableSHAMismatchError{
				Path: d.ArtifactPath, Expected: d.ArtifactSHA256, Actual: got,
			}
		}
	}
	if err := cmd.Start(); err != nil {
		releasePrepFDs(prep)
		return nil, fmt.Errorf("processmodule: exec %s: %w", execName(cmd), err)
	}
	if prep.Artifact != nil {
		_ = prep.Artifact.Close()
	}
	if ctx.Err() != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		releasePrepFDs(prep)
		return nil, ctx.Err()
	}

	frameBytes := prep.Spec.Descriptor.Limits.FrameBytes
	if frameBytes <= 0 {
		frameBytes = moduleproto.DefaultMaxFrameBytes
	}
	if newCodec == nil {
		newCodec = moduleproto.NewCodec
	}
	codec, err := newCodec(prep.Stdout, prep.Stdin, frameBytes)
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		releasePrepFDs(prep)
		return nil, fmt.Errorf("processmodule: codec: %w", err)
	}

	go func() {
		_ = prep.Stderr.Drain(prep.StderrPipe)
	}()

	rc := &spawnedChild{
		cmd:    cmd,
		stdin:  prep.Stdin,
		codec:  codec,
		stderr: prep.Stderr,
		pgid:   childPgid(cmd),
	}
	if prep.OwnScratch {
		rc.scratch = prep.Scratch
	}
	started = true
	return rc, nil
}

func (p *childPrep) removeOwnedScratch() {
	if p != nil && p.OwnScratch {
		_ = os.RemoveAll(p.Scratch)
	}
}

func execName(cmd *exec.Cmd) string {
	if len(cmd.Args) > 0 {
		return cmd.Args[0]
	}
	return cmd.Path
}

func (r *TrustedProcessRunner) allowlist() []string {
	if r == nil || len(r.EnvAllowlist) == 0 {
		return DefaultChildEnvAllowlist()
	}
	return r.EnvAllowlist
}

type spawnedChild struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	codec    *moduleproto.Codec
	stderr   *moduleproto.RingSink
	pgid     int
	mu       sync.Mutex
	waitOnce sync.Once
	waitErr  error
	reaped   atomic.Bool

	scratch string
}

func (c *spawnedChild) Codec() *moduleproto.Codec     { return c.codec }
func (c *spawnedChild) Stderr() *moduleproto.RingSink { return c.stderr }
func (c *spawnedChild) Pid() int {
	if c.cmd.Process != nil {
		return c.cmd.Process.Pid
	}
	return -1
}
func (c *spawnedChild) ProcessGroup() int { return c.pgid }

func (c *spawnedChild) CloseStdin() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil {
		return nil
	}
	err := c.stdin.Close()
	c.stdin = nil
	return err
}

func (c *spawnedChild) Kill() error {
	if c.cmd.Process == nil {
		return nil
	}
	if c.reaped.Load() {
		return nil
	}
	if c.pgid > 0 {
		if err := signalProcessGroup(c.pgid, syscall.SIGKILL); err == nil {
			return nil
		}
	}
	return c.cmd.Process.Kill()
}

func (c *spawnedChild) Wait() error {
	c.waitOnce.Do(func() {
		c.waitErr = c.cmd.Wait()
		if c.pgid > 0 {
			_ = signalProcessGroup(c.pgid, syscall.SIGKILL)
		}
		c.reaped.Store(true)
		if c.scratch != "" {
			_ = os.RemoveAll(c.scratch)
		}
	})
	return c.waitErr
}

// ExecutableSHAMismatchError is the refusal of an artifact whose SHA-256 differs
// from the descriptor pin; the supervisor treats it as a terminal fault.
type ExecutableSHAMismatchError struct {
	Path     string
	Expected string
	Actual   string
}

func (e *ExecutableSHAMismatchError) Error() string {
	return fmt.Sprintf("processmodule: executable sha256 mismatch on %s: expected %s got %s",
		e.Path, e.Expected, e.Actual)
}

func sha256OfFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return sha256OfReader(f)
}

func sha256OfReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func buildChildEnv(allowlist, extras, inherit []string) []string {
	want := append([]string(nil), allowlist...)
	want = append(want, inherit...)
	env := make([]string, 0, len(extras)+len(want))
	seen := make(map[string]bool, len(extras)+len(want))
	for _, kv := range extras {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		env = append(env, kv)
	}
	for _, name := range want {
		if seen[name] {
			continue
		}
		seen[name] = true
		if v, ok := lookupChildEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

func lookupChildEnv(name string) (string, bool) {
	if v, ok := os.LookupEnv(name); ok {
		return v, true
	}
	if runtime.GOOS == "windows" && name == "HOME" {
		return os.LookupEnv("USERPROFILE")
	}
	return "", false
}

var defaultChildEnvAllowlist = []string{
	"LANG", "LC_ALL", "LC_CTYPE", "PATH", "HOME", "TMPDIR", "USER", "LOGNAME",
}

func safeDirName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "module"
	}
	return b.String()
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func childPgid(cmd *exec.Cmd) int {
	if cmd.Process == nil {
		return 0
	}
	return processPgid(cmd.Process.Pid)
}
