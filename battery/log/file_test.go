package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSinkAppendsLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	s, err := FileSink(path, FileOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{`{"a":1}`, `{"b":2}`, `{"c":3}`} {
		if err := s.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFileSinkRotatesOnSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	// 20-byte cap; each entry "X" + "\n" is 2 bytes — but writes go in
	// blocks of ~22 bytes once we factor the JSON shape we'll use here.
	s, err := FileSink(path, FileOpts{MaxSize: 20, MaxBackups: 3})
	if err != nil {
		t.Fatal(err)
	}
	// 4 entries of 10 bytes each + newlines = 44 bytes total → at least
	// one rotation.
	for i := 0; i < 4; i++ {
		if err := s.Write([]byte(`xxxxxxxxxx`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	hasBackup := false
	for _, n := range names {
		if strings.HasPrefix(n, "app.log.") {
			hasBackup = true
		}
	}
	if !hasBackup {
		t.Fatalf("expected a rotated backup, got %v", names)
	}
}

func TestFileSinkRespectsMaxBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	s, err := FileSink(path, FileOpts{MaxSize: 10, MaxBackups: 2})
	if err != nil {
		t.Fatal(err)
	}
	// Force many rotations.
	for i := 0; i < 20; i++ {
		_ = s.Write([]byte(`xxxxxxxxxx`))
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "app.log.") {
			count++
		}
	}
	if count > 2 {
		t.Fatalf("expected ≤2 backups, got %d", count)
	}
}

func TestDefaultFileSinkUsesXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	s, err := DefaultFileSink("myapp", FileOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Write([]byte(`{"k":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "myapp", "server.log")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected %s, stat err: %v", want, err)
	}
}

// TestFileSinkDefaultModeIs0o600 pins the perms hardening: server logs
// often contain request paths + panic stacks; they should not be
// world-readable on a multi-user box.
func TestFileSinkDefaultModeIs0o600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perm.log")
	s, err := FileSink(path, FileOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 0600", got)
	}
}

// TestFileSinkRefusesSymlink (unix only) pins the O_NOFOLLOW behavior:
// a planted symlink at the log path can't redirect writes elsewhere.
func TestFileSinkRefusesSymlink(t *testing.T) {
	if unixNoFollow == 0 {
		t.Skip("no O_NOFOLLOW on this platform")
	}
	dir := t.TempDir()
	realTarget := filepath.Join(dir, "target.log")
	if err := os.WriteFile(realTarget, []byte("attacker"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "app.log")
	if err := os.Symlink(realTarget, link); err != nil {
		t.Fatal(err)
	}
	if _, err := FileSink(link, FileOpts{}); err == nil {
		t.Fatal("FileSink opened a symlinked log path; expected error from O_NOFOLLOW")
	}
}

// TestFileSinkWriteAfterClose verifies the sink no-ops after Close so
// long-lived loggers held by goroutines across shutdown don't panic.
func TestFileSinkWriteAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	s, err := FileSink(path, FileOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Write([]byte(`{"a":1}`)); err != ErrSinkClosed {
		t.Fatalf("Write after Close = %v, want ErrSinkClosed", err)
	}
	// Close is idempotent.
	if err := s.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
}
