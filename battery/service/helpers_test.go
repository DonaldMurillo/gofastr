package service

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// realHome is HOME as the test binary started.
var realHome = os.Getenv("HOME")

// fakeTools records every manager command instead of running it. fail
// maps a command prefix ("launchctl kickstart") to the error it
// returns.
type fakeTools struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]error
	// out maps a command prefix to the output it prints; hook to a
	// function run when it is called (a fake daemon reacting to it).
	out  map[string][]byte
	hook map[string]func()
}

func (f *fakeTools) output(name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.mu.Lock()
	f.calls = append(f.calls, line)
	var hook func()
	for prefix, h := range f.hook {
		if strings.HasPrefix(line, prefix) {
			hook = h
		}
	}
	var out []byte
	for prefix, o := range f.out {
		if strings.HasPrefix(line, prefix) {
			out = o
		}
	}
	for prefix, err := range f.fail {
		if strings.HasPrefix(line, prefix) {
			f.mu.Unlock()
			return []byte("refused\x1b[31m"), err
		}
	}
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return out, nil
}

func (f *fakeTools) stream(w io.Writer, name string, args ...string) error {
	_, err := f.output(name, args...)
	return err
}

func (f *fakeTools) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// shortTempDir is a temp dir short enough for a control socket path
// under it: macOS's per-user TMPDIR plus a test name overruns the
// 104-byte sun_path limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	base := ""
	if len(os.TempDir()) > 40 && filepath.Separator == '/' {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "svc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	// /tmp is a symlink on macOS; the daemon reports resolved paths.
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	return dir
}

// testBattery is a Battery for goos with fake tools and a private
// state base.
func testBattery(t *testing.T, goos string, cfg Config) (*Battery, *fakeTools, *bytes.Buffer) {
	t.Helper()
	t.Setenv(dataDirEnv, shortTempDir(t))
	// The LaunchAgents and systemd unit dirs hang off HOME: a test that
	// did not pick its own gets a temp one, so no install path can
	// write into the account running the tests.
	if os.Getenv("HOME") == realHome {
		setHome(t)
	}
	prev := currentOS
	currentOS = goos
	t.Cleanup(func() { currentOS = prev })
	// A non-root user, so a test container running as root exercises
	// the same paths a developer's machine does.
	prevEUID := geteuid
	geteuid = func() int { return 1000 }
	t.Cleanup(func() { geteuid = prevEUID })
	if cfg.ID == "" {
		cfg.ID = "dev.gofastr.svctest"
	}
	if cfg.Port == 0 {
		cfg.Port = 47811
	}
	var out bytes.Buffer
	cfg.Stdout, cfg.Stderr = &out, &out
	b := New(cfg)
	if b.err != nil {
		t.Fatal(b.err)
	}
	ft := &fakeTools{fail: map[string]error{}, out: map[string][]byte{}, hook: map[string]func(){}}
	b.tool = ft
	// The account the Windows backend names its task after.
	prevAccount := currentAccount
	currentAccount = func() (string, string, error) { return `PC\tester`, "S-1-5-21-7", nil }
	t.Cleanup(func() { currentAccount = prevAccount })
	// Layouts do not depend on whether the test tree is a linked
	// worktree; the per-worktree dev dir has its own test.
	prevIso := devIsolationID
	devIsolationID = func() string { return "" }
	t.Cleanup(func() { devIsolationID = prevIso })
	return b, ft, &out
}
