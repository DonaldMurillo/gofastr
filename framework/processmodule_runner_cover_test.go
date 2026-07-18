package framework

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// This file adds unit coverage for the PURE runner helpers + the spawnedChild
// accessors in processmodule_runner.go that are reachable WITHOUT spawning a
// real child (construct the struct directly). The spawn / Kill-on-live-child
// / Wait-on-live-child paths are environment-gated and covered elsewhere.

// ---- safeDirName ----

func TestSafeDirName_sanitizes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"demo", "demo"},
		{"my-mod_1", "my-mod_1"},
		{"with space!", "with-space-"},
		{"中文", "--"},   // each non-allowed rune → '-'; fallback only on empty result
		{"", "module"}, // empty → fallback
	}
	for _, c := range cases {
		if got := safeDirName(c.in); got != c.want {
			t.Errorf("safeDirName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---- shortID ----

func TestShortID_trimsTo12(t *testing.T) {
	if got := shortID("abc"); got != "abc" {
		t.Errorf("shortID(short) = %q", got)
	}
	if got := shortID("0123456789abcdef"); len(got) != 12 {
		t.Errorf("shortID(long) = %q, want len 12", got)
	}
}

// ---- buildChildEnv ----

func TestBuildChildEnv_dedupAndOverride(t *testing.T) {
	// Explicit KEY=VALUE entries win and emit first; allowlist + inherit are
	// unioned after, with unset host vars silently skipped.
	const tName = "GOFASTR_TEST_ENV_VAR_DO_NOT_EXIST"
	os.Unsetenv(tName) // ensure unset so the allowlist entry is skipped
	env := buildChildEnv(
		[]string{tName, "HOME"},                       // allowlist: HOME set, tName unset
		[]string{"EXPLICIT=1", "EXPLICIT=2", "B=A=B"}, // extras: dedup + override + '=' in value
		[]string{"NONEXISTENT_INHERIT"},               // inherit: unset → skipped
	)
	have := map[string]string{}
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		have[kv[:idx]] = kv[idx+1:]
	}
	if have["EXPLICIT"] != "1" {
		t.Errorf("EXPLICIT = %q, want 1 (first wins)", have["EXPLICIT"])
	}
	if have["B"] != "A=B" {
		t.Errorf("B = %q, want A=B ('=' in value preserved)", have["B"])
	}
	if _, has := have[tName]; has {
		t.Errorf("unset allowlist var %s should be skipped", tName)
	}
	if _, has := have["NONEXISTENT_INHERIT"]; has {
		t.Error("unset inherit var should be skipped")
	}
	if _, has := have["HOME"]; !has {
		t.Error("HOME should be present (host env typically set)")
	}
}

// ---- DefaultChildEnvAllowlist ----

func TestDefaultChildEnvAllowlist_isCopy(t *testing.T) {
	a := DefaultChildEnvAllowlist()
	b := DefaultChildEnvAllowlist()
	// Each call returns a fresh slice (mutations on one don't affect the next).
	if len(a) == 0 {
		t.Fatal("default allowlist is empty")
	}
	a[0] = "MUTATED"
	if b[0] == "MUTATED" {
		t.Error("DefaultChildEnvAllowlist returned shared backing slice")
	}
}

// ---- TrustedProcessRunner.allowlist override ----

func TestTrustedRunner_allowlistDefaultAndOverride(t *testing.T) {
	r := &TrustedProcessRunner{}
	// nil EnvAllowlist → default set.
	if got := r.allowlist(); len(got) == 0 {
		t.Error("nil EnvAllowlist must fall back to default")
	}
	// Custom EnvAllowlist is returned as-is.
	r.EnvAllowlist = []string{"CUSTOM1", "CUSTOM2"}
	got := r.allowlist()
	if len(got) != 2 || got[0] != "CUSTOM1" || got[1] != "CUSTOM2" {
		t.Errorf("custom allowlist = %+v", got)
	}
}

// ---- ExecutableSHAMismatchError ----

func TestExecutableSHAMismatchError_message(t *testing.T) {
	e := &ExecutableSHAMismatchError{Path: "/bin/x", Expected: "aaa", Actual: "bbb"}
	msg := e.Error()
	for _, want := range []string{"/bin/x", "aaa", "bbb"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, missing %q", msg, want)
		}
	}
}

// ---- spawnedChild accessors constructed directly (no spawn) ----

func TestSpawnedChild_accessorsOnNilProcess(t *testing.T) {
	c := &spawnedChild{cmd: &exec.Cmd{}} // cmd.Process is nil
	if got := c.Pid(); got != -1 {
		t.Errorf("Pid() = %d, want -1 when Process is nil", got)
	}
	if got := c.ProcessGroup(); got != 0 {
		t.Errorf("ProcessGroup() = %d, want 0", got)
	}
	if c.Codec() != nil {
		t.Error("Codec() should be nil for a zero codec")
	}
	if c.Stderr() != nil {
		t.Error("Stderr() should be nil for a zero stderr")
	}
}

func TestSpawnedChild_closeStdinIdempotent(t *testing.T) {
	c := &spawnedChild{cmd: &exec.Cmd{}, stdin: nopWriteCloser{}}
	if err := c.CloseStdin(); err != nil {
		t.Errorf("first CloseStdin: %v", err)
	}
	// Second call must be a no-op (stdin already nil), not a panic.
	if err := c.CloseStdin(); err != nil {
		t.Errorf("second CloseStdin: %v", err)
	}
}

func TestSpawnedChild_killNilProcessIsNoOp(t *testing.T) {
	c := &spawnedChild{cmd: &exec.Cmd{}} // Process nil
	if err := c.Kill(); err != nil {
		t.Errorf("Kill on nil Process = %v, want nil", err)
	}
}

// ---- sha256OfFile error path ----

func TestSha256OfFile_missingFileErrors(t *testing.T) {
	if _, err := sha256OfFile("/nonexistent/path/that/should/not/exist"); err == nil {
		t.Error("sha256OfFile on missing file must error")
	}
}

func TestSha256OfFile_hashesRealFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/blob"
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := sha256OfFile(path)
	if err != nil {
		t.Fatalf("sha256OfFile: %v", err)
	}
	// SHA-256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	if got != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Errorf("sha256OfFile(hello) = %q", got)
	}
}

// nopWriteCloser is a no-op io.WriteCloser for the CloseStdin test.
type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }

// ---- cleanupPrepPipes closes real pipes ----

func TestCleanupPrepPipes_closesRealPipes(t *testing.T) {
	// Build a childPrep with three real os.Pipe pairs and assert each is
	// closed after cleanupPrepPipes runs.
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prep := &childPrep{Stdin: inW, Stdout: outR, StderrPipe: errR}
	cleanupPrepPipes(prep)
	// A write to the read-end's now-closed writer should fail; and a read
	// from the write-end's now-closed reader should return EOF/error.
	if _, err := inW.Write([]byte("x")); err == nil {
		t.Error("Stdin not closed after cleanupPrepPipes")
	}
	// Close the other halves (the ones cleanupPrepPipes did NOT own) so the
	// test does not leak fds.
	_ = inR.Close()
	_ = outW.Close()
	_ = errW.Close()
}
