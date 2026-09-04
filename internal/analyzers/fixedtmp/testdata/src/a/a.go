// Package a holds the fixedtmp fixture reduced from the real pre-fix
// sites: cmd/gofastr dev.go devServerBinaryPath (pid-named temp binary
// built with `go build -o` then exec'd) and cmd/kiln adapters.go's Dir
// constants consumed by agent_watcher.go's MkdirAll + cmd.Dir, each
// with its fix posture (MkdirTemp/CreateTemp mint, crypto/rand nonce)
// next to it.
package a

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// ---------- pre-fix: dev server binary (cmd/gofastr/dev.go) ----------

type isolation struct{ id string }

func (r *isolation) Active() bool { return true }
func (r *isolation) ID() string   { return r.id }

// devServerBinaryPath is the real pre-fix shape: every input (pid,
// isolation ID, GOOS) is attacker-known or guessable, so the name is
// fully deterministic in the shared temp root.
func devServerBinaryPath(rt *isolation) string {
	tmpName := fmt.Sprintf("gofastr-dev-server-%d", os.Getpid())
	if rt.Active() {
		tmpName += "-" + rt.ID()
	}
	if runtime.GOOS == "windows" {
		tmpName += ".exe"
	}
	return filepath.Join(os.TempDir(), tmpName)
}

func buildAndServe(dir, pkg string, rt *isolation) bool {
	tmpBin := devServerBinaryPath(rt)
	buildCmd := exec.Command("go", "build", "-o", tmpBin, pkg) // want `exec/build reaches a fixed path under the shared temp root \(tmpBin\)`
	buildCmd.Dir = dir                                         // project dir: not a temp-root path, stays quiet
	if err := buildCmd.Run(); err != nil {
		return false
	}
	runCmd := exec.Command(tmpBin, "--addr", "localhost:8080") // want `exec/build reaches a fixed path under the shared temp root \(tmpBin\)`
	return runCmd != nil
}

// shutdownCleanup is dev.go's os.Remove: an unlink follows no symlink
// target, so it is not a sink.
func shutdownCleanup(rt *isolation) {
	_ = os.Remove(devServerBinaryPath(rt))
}

// ---------- pre-fix: kiln adapter registry (cmd/kiln/adapters.go) ----------

type agentAdapter struct {
	Name string
	Dir  string
}

// builtinAdapters is the real registry shape: package-level composite
// literals carrying fixed Dir paths under os.TempDir().
var builtinAdapters = map[string]agentAdapter{
	"omp":        {Name: "omp", Dir: filepath.Join(os.TempDir(), "kiln-omp")},
	"claude":     {Name: "claude", Dir: filepath.Join(os.TempDir(), "kiln-claude")},
	"claude-lit": {Name: "claude", Dir: "/tmp/kiln-codex"},
}

// runOneAgentTurn is agent_watcher.go: MkdirAll no-ops on a pre-created
// dir regardless of mode, then the fixed dir becomes the child's cwd.
func runOneAgentTurn(adapter agentAdapter) {
	c := exec.CommandContext(context.Background(), adapter.Name)
	_ = os.MkdirAll(adapter.Dir, 0o755) // want `mkdir on a fixed path under the shared temp root \(adapter\.Dir\)`
	c.Dir = adapter.Dir                 // want `exec Dir is a fixed path under the shared temp root \(adapter\.Dir\)`
	_ = c
}

// ---------- pre-fix: literals and pid-only names ----------

func ensureScratch() error {
	return os.MkdirAll("/tmp/kiln-lit", 0o755) // want `mkdir on a fixed path under the shared temp root \("/tmp/kiln-lit"\)`
}

func writeStamp() error {
	return os.WriteFile("/tmp/gofastr-stamp", []byte("x"), 0o644) // want `file created at a fixed path under the shared temp root \("/tmp/gofastr-stamp"\)`
}

func openPidLock() (*os.File, error) {
	return os.OpenFile(os.TempDir()+"/gofastr-"+strconv.Itoa(os.Getpid()), os.O_WRONLY|os.O_CREATE, 0o600) // want `file created at a fixed path under the shared temp root`
}

// ---------- silent posture: MkdirTemp / CreateTemp mints ----------

// ephemeralSQLite is kiln/db's EphemeralSQLite: the dir itself is minted
// unique and 0700; writes under its root are not under the SHARED root.
func ephemeralSQLite() (*os.File, error) {
	dir, err := os.MkdirTemp("", "kiln-*")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "wal"), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "app.db"), os.O_RDWR|os.O_CREATE, 0o600)
}

// createTempLog uses the stdlib mint directly.
func createTempLog() (*os.File, error) {
	return os.CreateTemp("", "kiln-log-*")
}

// stageUnderTest runs under t.TempDir: per-test private root.
func stageUnderTest(t *testing.T) error {
	scratch := t.TempDir()
	return os.MkdirAll(filepath.Join(scratch, "run"), 0o700)
}

// ---------- silent posture: crypto/rand entropy ----------

// randSuffix mints entropy a local co-user cannot guess; a component
// derived from it is not predictable even under the shared root.
func randSuffix() string {
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func buildWithRandSuffix() error {
	bin := filepath.Join(os.TempDir(), "srv-"+randSuffix())
	return exec.Command("go", "build", "-o", bin, ".").Run()
}

// The processmodule shape: the name derives from a struct field whose
// package-wide provenance is a crypto/rand nonce.
type childSpec struct {
	InstanceID string
}

func mintInstanceID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func spawnOnce() {
	id := mintInstanceID()
	spec := childSpec{InstanceID: id}
	startModule(spec)
}

func startModule(spec childSpec) {
	scratch := filepath.Join(os.TempDir(),
		fmt.Sprintf("gofastr-module-%s", shortID(spec.InstanceID)))
	_ = os.MkdirAll(scratch, 0o700)
}

// ---------- silent posture: not under the shared temp root ----------

func statePath() string { return filepath.Join("var", "lib", "gofastr") }

func openStateLock() (*os.File, error) {
	// A pid-only name OUTSIDE the temp root is another filesystem's
	// problem: the shared-root prefix is what makes it pre-creatable
	// by a co-user.
	return os.OpenFile(filepath.Join(statePath(), fmt.Sprintf("lock-%d", os.Getpid())),
		os.O_WRONLY|os.O_CREATE, 0o600)
}

func runList() error {
	// "/tmp" itself is not a fixed leaf, and listing is not a create
	// sink.
	return exec.Command("ls", "/tmp").Run()
}
