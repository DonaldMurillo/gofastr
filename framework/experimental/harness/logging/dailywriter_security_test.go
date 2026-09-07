package logging

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Pins: a fixed-name log sink refuses a symlinked target and keeps
// CONTRACT-QUESTION: battery/log's FileSink refuses symlinked log paths
// via O_NOFOLLOW (pinned: TestFileSinkRefusesSymlink, file_test.go:140)
// and pins 0600 on create (TestFileSinkDefaultModeIs0o600, :121),
// because "server logs often contain request paths + panic stacks;
// they should not be world-readable on a multi-user box". The harness's
// DailyFileWriter is the divergent twin writing the same class of data
// (component logs incl. provider/transport lines) — is the harness
// sink exempt from the family grammar?
// Property: a fixed-name log sink refuses a symlinked target and keeps
// its file owner-only on append (0600), mirroring battery/log fileSink
// (file.go:113-119).
// Surfaces: logging.go::DailyFileWriter.Write :228 —
// os.OpenFile(path, O_APPEND|O_CREATE|O_WRONLY, 0600) follows a
// symlink planted at harness-YYYYMMDD.log, and O_APPEND never
// tightens a pre-existing weaker mode (NewDailyFileWriter's MkdirAll
// :213 likewise never tightens an existing weaker dir).
// Finding: probe-verified both legs — (a) a symlink at the daily name
// redirects harness log lines into whatever file the link names;
// (b) a pre-existing 0644 daily file is appended to and stays 0644.
// Fix direction: Lstat-refuse the leaf before open (kiln journal.go::
// openJournalFile spelling, TestOpenJournalRefusesSymlinkLeaf) and
// chmod-on-handle to 0600 (pack.go::writeSecretFile grammar).
func TestDailyWriterRedSymlinkRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}

	today := time.Now().UTC().Format("20060102")
	dailyName := "harness-" + today + ".log"

	// Leg (a): symlink at the fixed daily name must be refused.
	dir := t.TempDir()
	realTarget := filepath.Join(dir, "target.log")
	if err := os.WriteFile(realTarget, []byte("attacker-owned\n"), 0o600); err != nil {
		t.Fatalf("setup broken: seed target: %v", err)
	}
	link := filepath.Join(dir, dailyName)
	if err := os.Symlink(realTarget, link); err != nil {
		t.Fatalf("setup broken: symlink: %v", err)
	}

	w, err := NewDailyFileWriter(dir)
	if err != nil {
		t.Fatalf("setup broken: NewDailyFileWriter: %v", err)
	}
	payload := []byte("harness-log-line\n")
	if _, err := w.Write(payload); err == nil {
		t.Errorf("SECURITY: [dailywriter-symlink] DailyFileWriter.Write followed the symlink planted at %s — the battery/log twin refuses symlinked sink paths via O_NOFOLLOW (TestFileSinkRefusesSymlink), the harness sink must refuse too, not append harness log lines into an attacker-chosen file", link)
	}
	_ = w.Close()

	got, err := os.ReadFile(realTarget)
	if err != nil {
		t.Fatalf("setup broken: read target: %v", err)
	}
	if strings.Contains(string(got), "harness-log-line") {
		t.Errorf("SECURITY: [dailywriter-symlink] payload landed in the symlink target %s — DailyFileWriter.Write's os.OpenFile(O_APPEND|O_CREATE|O_WRONLY) (logging.go:228) follows the link, redirecting harness log output to whatever file the planted leaf names", realTarget)
	}

	// Leg (b): a pre-existing weak-mode daily file must be tightened
	// to 0600 on append.
	dir2 := t.TempDir()
	stale := filepath.Join(dir2, dailyName)
	if err := os.WriteFile(stale, []byte("old line\n"), 0o644); err != nil {
		t.Fatalf("setup broken: seed stale daily file: %v", err)
	}
	if err := os.Chmod(stale, 0o644); err != nil {
		t.Fatalf("setup broken: chmod stale daily file: %v", err) // defeat umask so the precondition is exact
	}

	w2, err := NewDailyFileWriter(dir2)
	if err != nil {
		t.Fatalf("setup broken: NewDailyFileWriter: %v", err)
	}
	if _, err := w2.Write([]byte("new line\n")); err != nil {
		t.Fatalf("setup broken: append to stale daily file: %v", err)
	}
	_ = w2.Close()

	body, err := os.ReadFile(stale)
	if err != nil {
		t.Fatalf("setup broken: read stale daily file: %v", err)
	}
	if !strings.Contains(string(body), "new line") {
		t.Fatalf("setup broken: append did not land — file content %q", body)
	}

	fi, err := os.Stat(stale)
	if err != nil {
		t.Fatalf("setup broken: stat daily file: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: [dailywriter-symlink] daily log %s is %v after append — DailyFileWriter.Write's 0600 only applies at CREATE (logging.go:228), so a pre-existing 0644 daily file keeps harness log lines (request paths, panic stacks — the same class battery/log pins owner-only via TestFileSinkDefaultModeIs0o600) group/world-readable", stale, fi.Mode().Perm())
	}
}
