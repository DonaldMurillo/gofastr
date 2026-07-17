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
	"strings"
	"sync"
	"syscall"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// Runner is the seam a process module's spawn path plugs into (design §6,
// decision C). Wave 2a ships exactly one implementation,
// [TrustedProcessRunner]; the next wave slots [SandboxRunner] in behind the
// same interface.
//
// The Runner is RESPONSIBLE FOR and ONLY FOR:
//
//   - verifying the executable SHA-256 equals the descriptor pin BEFORE exec
//     (verify-then-exec, design §4.6 — mirrors
//     framework/harness/mcpclient/client.go:83-91);
//   - applying baseline hygiene (empty-by-default env allowlist, no fds
//     beyond stdio, cwd = per-module scratch dir, own process group);
//   - wiring the child's stdin/stdout to a [moduleproto.Codec];
//   - draining the child's stderr to a bounded [moduleproto.RingSink].
//
// It does NOT perform the handshake, poll ready, or apply capability policy
// — those live in the supervisor above. The returned [RunningChild] is at
// "process started, codec wired, peer NOT yet started": the supervisor
// installs the reverse broker handlers and then calls Peer.Start.
type Runner interface {
	// Start spawns the child per spec and returns the live handle.
	// Canceling ctx cancels the spawn (kills a half-spawned child); it does
	// NOT cancel the child's lifetime — the supervisor owns teardown via
	// [RunningChild.CloseStdin] / [RunningChild.Kill] / [RunningChild.Wait].
	Start(ctx context.Context, spec ChildSpec) (RunningChild, error)
}

// ChildSpec is the supervisor's spawn request. Every field is host-derived
// (operator-approved descriptor + the store's read generation + the
// per-spawn instance_id). The child never supplies these.
type ChildSpec struct {
	// Descriptor is the operator-approved descriptor. The runner reads
	// ArtifactPath / ArtifactSHA256 / TrustTier / Limits from it.
	Descriptor ProcessModuleDescriptor

	// InstanceID is the random per-spawn liveness nonce (design §4.7
	// step 1). The runner does not interpret it; the supervisor passes it
	// through to the handshake via its own state.
	InstanceID string

	// EffectiveGrants is the post-approval grant set for this spawn.
	// Stored on the spec so the supervisor (which calls Start) keeps a
	// single source of truth; the runner does not interpret it.
	EffectiveGrants []access.Permission

	// Generation is the store's desired_generation read at spawn. Stored
	// on the spec for the same reason as EffectiveGrants.
	Generation uint64

	// Stderr is the bounded sink the child's stderr is drained into. If
	// nil the runner constructs a default-capacity [moduleproto.RingSink]
	// tagged for the module (so the supervisor can Tail recent output).
	Stderr *moduleproto.RingSink

	// ExtraEnv is a list of "KEY=VALUE" entries added on top of the
	// default scrubbed allowlist (mirrors mcpclient.SpawnConfig.Env).
	// These win over the allowlist on collision. Use only for values the
	// caller knows; never as a back-channel for host secrets.
	ExtraEnv []string

	// InheritEnv is a list of host env var names to copy through beyond
	// the default allowlist (mirrors mcpclient.SpawnConfig.InheritEnv).
	// Every name is a deliberate allow decision.
	InheritEnv []string

	// ScratchDir is the per-module working directory. If empty the runner
	// creates one under os.TempDir named "gofastr-module-<name>-<instance>".
	// The supervisor usually supplies t.TempDir() in tests.
	ScratchDir string
}

// RunningChild is the supervisor's handle to a spawned child process. The
// lifetime contract is design §4.6's lift list: CloseStdin → wait deadline
// → Kill → Wait.
type RunningChild interface {
	// Codec is the protocol pipe (child's stdout read side + stdin write
	// side wrapped in a moduleproto.Codec). The supervisor builds its
	// [moduleproto.Peer] over this.
	Codec() *moduleproto.Codec

	// Stderr is the bounded sink draining the child's stderr. Use Tail()
	// to read recent log output for diagnostics / Failed-state reports.
	Stderr() *moduleproto.RingSink

	// Pid returns the child's process id, or -1 if the process has exited
	// and been reaped.
	Pid() int

	// ProcessGroup returns the child's Unix process group id (equal to
	// the child's pid under Setpgid:true), or 0 on Windows / pre-spawn.
	ProcessGroup() int

	// CloseStdin closes the write side of the child's stdin so the child
	// observes EOF. Idempotent. This is step 1 of the §4.6 teardown.
	CloseStdin() error

	// Kill sends SIGKILL to the child (and its process group on Unix).
	// Idempotent. This is step 3 of the §4.6 teardown, used after the
	// drain deadline expires. It does NOT block; call Wait to reap.
	Kill() error

	// Wait blocks until the child exits and returns the wait error
	// (nil for a clean exit). It is the reap step. Calling Wait before
	// Kill/CloseStdin is legal — it just blocks until natural exit.
	Wait() error
}

// TrustedProcessRunner is the wave-2a Runner: crash isolation only (design
// §6 decision C — "TrustedProcessRunner for dev"). It applies baseline
// hygiene (empty-by-default env allowlist, own process group, cwd =
// per-module scratch dir, SHA-256 pin verified before exec, stderr →
// RingSink) via the shared [prepareChildForSpawn]/[startPreparedChild]
// helpers — the SAME helpers [SandboxRunner] uses, so the two runners
// cannot drift on baseline hygiene (design §6: "applied by BOTH runners").
// It is NOT a security sandbox — an unconfined child can still
// open/connect/dial. Untrusted modules must use [SandboxRunner], which
// layers OS-enforced denials on top of the same baseline.
type TrustedProcessRunner struct {
	// EnvAllowlist is the base env-var names the child receives (PATH,
	// HOME, …). If nil, [DefaultChildEnvAllowlist] is used. Names not in
	// the union of this list + spec.InheritEnv are dropped.
	EnvAllowlist []string

	// NewCodec constructs a Codec over the supplied reader/writer. If nil,
	// [moduleproto.NewCodec] is used with the spec's negotiated frame
	// bytes (clamped to DefaultMaxFrameBytes). Tests inject a fake to
	// avoid touching real stdio.
	NewCodec func(r io.Reader, w io.Writer, maxFrameBytes int) (*moduleproto.Codec, error)
}

// DefaultChildEnvAllowlist is the minimal set of host env-var names a child
// needs to exec and run ordinary tools, with no secrets. Grows only when a
// real child genuinely cannot run without a name. Mirrors
// framework/harness/mcpclient.defaultEnvAllowlist so the two spawners share
// the same baseline.
func DefaultChildEnvAllowlist() []string {
	return append([]string(nil), defaultChildEnvAllowlist...)
}

// Start implements [Runner.Start]. It is the trusted-tier spawn: verify
// the SHA pin, build the exec.Cmd with baseline hygiene, wire stdio, exec.
// It is a thin composition of [prepareChildForSpawn] (verify + cmd + pipes)
// and [startPreparedChild] (exec + codec + stderr drain); [SandboxRunner]
// reuses both with a backend.Wrap step in between.
func (r *TrustedProcessRunner) Start(ctx context.Context, spec ChildSpec) (RunningChild, error) {
	prep, err := prepareChildForSpawn(spec, r.allowlist())
	if err != nil {
		return nil, err
	}
	return startPreparedChild(ctx, prep, r.NewCodec)
}

// childPrep is the result of preparing a child for exec: the exec.Cmd with
// baseline hygiene applied (scrubbed env, own process group, cwd=scratch),
// the wired stdio pipes, and the resolved scratch dir + stderr sink. It is
// the shared handshake between [TrustedProcessRunner] (which exec's
// directly) and [SandboxRunner] (which calls backend.Wrap on prep.Cmd
// between prepare and start).
type childPrep struct {
	Cmd        *exec.Cmd
	Stdin      io.WriteCloser
	Stdout     io.ReadCloser
	StderrPipe io.ReadCloser
	Scratch    string
	Stderr     *moduleproto.RingSink
	Spec       ChildSpec
}

// prepareChildForSpawn is the baseline-hygiene half of the §6 spawn
// contract, shared by BOTH runners (design §6: "applied by BOTH runners"):
//
//   - verify-then-exec: SHA-256 pin checked BEFORE exec (mirrors
//     mcpclient.SpawnWithConfig, framework/harness/mcpclient/client.go:82-128;
//     the #37 trust anchor, design §3 decision B);
//   - per-module scratch dir as cwd (0o700; concurrent children never
//     collide);
//   - empty-default env allowlist: cmd.Env is the explicit union of the
//     runner's allowlist + spec.InheritEnv + spec.ExtraEnv — NEVER the
//     full os.Environ() (this is the §6 finding that mcpclient.Spawn
//     leaked every host secret; baseline hygiene is the P2 enforcement);
//   - own process group (Setpgid on Unix, CREATE_NEW_PROCESS_GROUP on
//     Windows — the sysproc_unix.go/_other.go split);
//   - stdio pipes wired: stdin/stdout = protocol, stderr = bounded
//     RingSink (design §4.2; stdout=protocol only, stderr=bounded log).
//
// It does NOT call cmd.Start — the caller may apply sandbox wrapping
// (SandboxRunner) or exec directly (TrustedProcessRunner). No fds beyond
// 0/1/2 are inherited (cmd.ExtraFiles stays nil — the P3 enforcement).
func prepareChildForSpawn(spec ChildSpec, allowlist []string) (*childPrep, error) {
	d := spec.Descriptor
	if d.ArtifactPath == "" || d.ArtifactSHA256 == "" {
		return nil, errors.New("processmodule: runner: descriptor artifact path/sha256 required")
	}
	// Verify-then-exec: pin the binary BEFORE spawning so a swapped file
	// never reaches exec (§4.6 lift + #37 trust anchor).
	got, err := sha256OfFile(d.ArtifactPath)
	if err != nil {
		return nil, fmt.Errorf("processmodule: hash %s: %w", d.ArtifactPath, err)
	}
	if got != d.ArtifactSHA256 {
		return nil, &ExecutableSHAMismatchError{
			Path: d.ArtifactPath, Expected: d.ArtifactSHA256, Actual: got,
		}
	}

	// Scratch dir = cwd. Per-module so concurrent children never collide.
	scratch := spec.ScratchDir
	if scratch == "" {
		scratch = filepath.Join(os.TempDir(),
			fmt.Sprintf("gofastr-module-%s-%s", safeDirName(d.Name), shortID(spec.InstanceID)))
	}
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		return nil, fmt.Errorf("processmodule: scratch dir %s: %w", scratch, err)
	}

	stderr := spec.Stderr
	if stderr == nil {
		stderr = moduleproto.NewRingSink(moduleproto.DefaultRingSinkBytes)
	}

	cmd := exec.Command(d.ArtifactPath)
	cmd.Dir = scratch
	cmd.Env = buildChildEnv(allowlist, spec.ExtraEnv, spec.InheritEnv)
	// Own process group (Unix: Setpgid; Windows: CREATE_NEW_PROCESS_GROUP).
	// SandboxRunner's backend may ADD fields (Cloneflags, UidMappings) on
	// top without clearing Setpgid — they compose.
	setChildProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("processmodule: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("processmodule: stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("processmodule: stderr pipe: %w", err)
	}

	return &childPrep{
		Cmd:        cmd,
		Stdin:      stdin,
		Stdout:     stdout,
		StderrPipe: stderrPipe,
		Scratch:    scratch,
		Stderr:     stderr,
		Spec:       spec,
	}, nil
}

// startPreparedChild is the exec half of the §6 spawn contract: it calls
// cmd.Start, honors ctx cancellation up to Start (kill a half-spawned
// child if the spawn deadline fired mid-spawn), wires the moduleproto
// Codec over stdin/stdout, and starts the stderr drain goroutine. Once it
// returns, the supervisor owns teardown (ctx no longer bounds lifetime).
// The returned [RunningChild] is the shared spawnedChild concrete handle.
func startPreparedChild(ctx context.Context, prep *childPrep, newCodec func(r io.Reader, w io.Writer, maxFrameBytes int) (*moduleproto.Codec, error)) (RunningChild, error) {
	cmd := prep.Cmd
	if err := cmd.Start(); err != nil {
		_ = prep.Stdin.Close()
		_ = prep.Stdout.Close()
		_ = prep.StderrPipe.Close()
		return nil, fmt.Errorf("processmodule: exec %s: %w", cmd.Path, err)
	}
	if ctx.Err() != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, ctx.Err()
	}

	// Wire the protocol codec over stdin/stdout.
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
		return nil, fmt.Errorf("processmodule: codec: %w", err)
	}

	// Drain stderr in the background; RingSink.Write never blocks.
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
	return rc, nil
}

// allowlist returns the runner's env allowlist, defaulting to
// DefaultChildEnvAllowlist.
func (r *TrustedProcessRunner) allowlist() []string {
	if r == nil || len(r.EnvAllowlist) == 0 {
		return DefaultChildEnvAllowlist()
	}
	return r.EnvAllowlist
}

// spawnedChild is the concrete [RunningChild] returned by BOTH
// [TrustedProcessRunner.Start] and [SandboxRunner.Start] (they share the
// [prepareChildForSpawn]/[startPreparedChild] spawn path; only the
// backend.Wrap step in between differs). The handle's lifetime contract
// (CloseStdin → Wait → Kill) is identical for trusted and sandboxed
// children, so one concrete type serves both — a sandboxed child whose
// backend set its own process group / kill-tree just feeds a different
// pgid into the same Kill path.
type spawnedChild struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	codec  *moduleproto.Codec
	stderr *moduleproto.RingSink

	pgid int

	// mu guards stdin / Kill against concurrent CloseStdin from a drain
	// path. waitOnce serializes [os/exec.Cmd.Wait] — exec.Cmd.Wait is NOT
	// safe for concurrent calls, but the supervisor has two legitimate
	// callers (the spawn's exit-watcher and a lease-expired / shutdown
	// drain), so the first caller reaps and the rest observe the same
	// error.
	mu       sync.Mutex
	waitOnce sync.Once
	waitErr  error
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
	// No lock: [os.Process].Kill is safe for concurrent calls and we read
	// c.cmd.Process only to decide whether to skip a nil process. The
	// pointer is set once at Start and never mutated afterwards.
	if c.cmd.Process == nil {
		return nil
	}
	// Group kill first (cleans up grandchildren the module may have
	// forked); per-process kill as a fallback if the group signal fails
	// (e.g. Windows where signalProcessGroup is a no-op).
	if c.pgid > 0 {
		if err := signalProcessGroup(c.pgid, syscall.SIGKILL); err == nil {
			return nil
		}
	}
	return c.cmd.Process.Kill()
}

// Wait reaps the child process. Safe for concurrent calls: the first
// caller performs the actual [os/exec.Cmd.Wait]; later callers observe
// the cached error. This matters because the supervisor legitimately has
// two Wait sites (the spawn's exit-watcher + a lease-expired or shutdown
// drain) that may both be live when the child exits.
func (c *spawnedChild) Wait() error {
	c.waitOnce.Do(func() {
		c.waitErr = c.cmd.Wait()
	})
	return c.waitErr
}

// ExecutableSHAMismatchError is returned when the executable's measured
// SHA-256 does not equal the descriptor pin. Terminal per design §3/§6: a
// swapped binary never reaches exec, and the supervisor transitions the
// module to Failed rather than retry.
type ExecutableSHAMismatchError struct {
	Path     string
	Expected string
	Actual   string
}

func (e *ExecutableSHAMismatchError) Error() string {
	return fmt.Sprintf("processmodule: executable sha256 mismatch on %s: expected %s got %s",
		e.Path, e.Expected, e.Actual)
}

// sha256OfFile hashes the file at path (verbatim lift of
// framework/harness/mcpclient.sha256OfBinary, kept local so this runner does
// not import the harness subpackage — the framework root may not import
// framework/harness per ARCHITECTURE.md layering).
func sha256OfFile(path string) (string, error) {
	f, err := os.Open(path)
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

// buildChildEnv assembles the child environment from the runner's allowlist
// plus extras (host vars copied by name) plus explicit KEY=VALUE entries
// (which override on collision). Names are de-duplicated; explicit entries
// win and emit first; host vars that are unset are silently skipped. The
// result is never nil so the child never inherits the full os.Environ()
// (design §6 baseline hygiene: the §6-finding that mcpclient.Spawn leaked
// every host secret).
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
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// defaultChildEnvAllowlist is the same minimal set mcpclient uses, kept in
// sync so a future consolidation puts the two on one helper.
var defaultChildEnvAllowlist = []string{
	"LANG", "LC_ALL", "LC_CTYPE", "PATH", "HOME", "TMPDIR", "USER", "LOGNAME",
}

// safeDirName sanitizes a module name for use as a scratch-dir component.
// Falls back to "module" if the result would be empty.
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

// shortID reduces an instance_id to its first 12 chars for path names.
func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

// childPgid returns the child's process group id (Unix only; 0 on Windows).
// Under Setpgid:true with no Pgid, the kernel assigns the child's own pid
// as the group id.
func childPgid(cmd *exec.Cmd) int {
	if cmd.Process == nil {
		return 0
	}
	return processPgid(cmd.Process.Pid)
}
