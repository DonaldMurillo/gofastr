//go:build !windows

package log

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestFileSinkTightensExistingLooseFile pins FileMode on a log file that
// already exists. The open's mode argument applies only at create, so a
// server.log left 0644 by an earlier build (before the 0600 default), a
// manual touch, or a provisioning script stayed world-readable for as
// long as it lived, while the sink's own rotation created every later
// file at FileMode. The sink now tightens the file to FileMode when it
// opens it, and never loosens one that is already tighter.
func TestFileSinkTightensExistingLooseFile(t *testing.T) {
	cases := []struct {
		name     string
		existing os.FileMode
		opt      os.FileMode
		want     os.FileMode
	}{
		{"default tightens 0644", 0o644, 0, 0o600},
		{"explicit 0640 tightens 0666", 0o666, 0o640, 0o640},
		{"never loosens", 0o600, 0o644, 0o600},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "server.log")
			if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, c.existing); err != nil {
				t.Fatal(err)
			}
			s, err := FileSink(path, FileOpts{FileMode: c.opt})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != c.want {
				t.Errorf("mode after FileSink = %o, want %o", got, c.want)
			}
			b, _ := os.ReadFile(path)
			if string(b) != "old\n" {
				t.Errorf("existing content changed: %q", b)
			}
		})
	}
}

// TestFileSinkLeavesSharedLogFileMode pins the other half of the
// tighten-on-open rule: the sink rewrites the mode only of a file this
// process owns. A provisioned /var/log/app/server.log at 0664 root:app,
// appended to by an app running as a member of group app, is the
// operator's arrangement; a chmod there returns EPERM, and the first
// cut of the tighten treated that as fatal, so the sink that used to
// append fine failed at boot and on every reopen. The container runs as
// root (chmod never fails), so the owner and the euid are injected.
func TestFileSinkLeavesSharedLogFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatal(err)
	}
	// The file belongs to somebody else: uid 0 owns it, we are uid 1000.
	restore := setOwnershipSeams(t, func(os.FileInfo) (int, bool) { return 0, true }, func() int { return 1000 })
	defer restore()
	var warned strings.Builder
	setWarnOut(t, &warned)

	s, err := FileSink(path, FileOpts{})
	if err != nil {
		t.Fatalf("FileSink on a shared file: %v (must append, not fail)", err)
	}
	defer s.Close()
	if err := s.Write([]byte(`{"msg":"x"}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o664 {
		t.Errorf("mode of a file another user owns = %o, want 0664 untouched", got)
	}
}

// TestFileSinkTightenEPERMWarnsOnce: when the chmod of a file the sink
// does own still fails (a read-only bind mount, an LSM refusing it), the
// sink keeps appending and says so once, not at every reopen.
func TestFileSinkTightenEPERMWarnsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	prev := chmodHandle
	chmodHandle = func(*os.File, os.FileMode) error { return &os.PathError{Op: "chmod", Path: path, Err: syscall.EPERM} }
	defer func() { chmodHandle = prev }()
	var warned strings.Builder
	setWarnOut(t, &warned)

	s, err := FileSink(path, FileOpts{})
	if err != nil {
		t.Fatalf("FileSink with a failing tighten: %v (must warn, not fail)", err)
	}
	defer s.Close()
	// Force the reopen path a few times: drop the handle the way a
	// failed rotation reopen does, so the next Write reopens the same,
	// still-loose file and tries the tighten again.
	fs := s.(*fileSink)
	for i := 0; i < 3; i++ {
		fs.mu.Lock()
		_ = fs.f.Close()
		fs.f = nil
		fs.mu.Unlock()
		if err := s.Write([]byte(`{"msg":"y"}`)); err != nil {
			t.Fatalf("Write %d after reopen: %v", i, err)
		}
	}
	if n := strings.Count(warned.String(), "log: could not tighten"); n != 1 {
		t.Errorf("tighten warnings = %d, want exactly 1; output:\n%s", n, warned.String())
	}
}

func setOwnershipSeams(t *testing.T, owner func(os.FileInfo) (int, bool), euid func() int) func() {
	t.Helper()
	prevOwner, prevEUID := fileOwnerUID, processEUID
	fileOwnerUID, processEUID = owner, euid
	return func() { fileOwnerUID, processEUID = prevOwner, prevEUID }
}

func setWarnOut(t *testing.T, w io.Writer) {
	t.Helper()
	prev := tightenWarnOut
	tightenWarnOut = w
	t.Cleanup(func() { tightenWarnOut = prev })
}
