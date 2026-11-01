package desktop

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The swap step's failure paths: a failed rename and its rollback, the
// retry that follows, and what the log says about them.

// swapLogCapture keeps every record's message and attribute values raw
// (no quoting), so a Windows path with backslashes is found as written.
type swapLogCapture struct {
	mu    sync.Mutex
	lines []string
}

func (c *swapLogCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	line := r.Message
	r.Attrs(func(a slog.Attr) bool {
		line += " " + a.Key + "=" + a.Value.String()
		return true
	})
	c.lines = append(c.lines, line)
	return nil
}
func (c *swapLogCapture) Enabled(context.Context, slog.Level) bool { return true }
func (c *swapLogCapture) WithAttrs([]slog.Attr) slog.Handler       { return c }
func (c *swapLogCapture) WithGroup(string) slog.Handler            { return c }

func (c *swapLogCapture) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.lines, "\n")
}

// assertNoPath fails when the log names any of dirs, in either slash
// form.
func assertNoPath(t *testing.T, log string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		for _, form := range []string{d, filepath.ToSlash(d)} {
			if strings.Contains(log, form) {
				t.Fatalf("the log names the path %q:\n%s", form, log)
			}
		}
	}
}

// failRenameOnto returns a rename that refuses any move onto target with
// the *os.LinkError os.Rename itself returns (both paths inside), and
// runs every other rename for real.
func failRenameOnto(target string, failing *bool) func(string, string) error {
	return func(oldpath, newpath string) error {
		if *failing && newpath == target {
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: fs.ErrPermission}
		}
		return os.Rename(oldpath, newpath)
	}
}

func TestInstallSwapFailureLogsNoPath(t *testing.T) {
	b, _, bundleDir := installFixture(t, "1.2.0", installBundleZip(t, installTestID, "1.2.0"))
	logs := &swapLogCapture{}
	b.logger = slog.New(logs)
	current := filepath.Join(bundleDir, "Notes.app")
	// The second rename fails; the rollback (old onto current) is the
	// same target, so let it through by failing only the first move
	// onto current.
	moves := 0
	b.updaterFor().rename = func(oldpath, newpath string) error {
		if newpath == current {
			moves++
			if moves == 1 {
				return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: fs.ErrPermission}
			}
		}
		return os.Rename(oldpath, newpath)
	}
	if err := b.InstallUpdate(context.Background()); !errors.Is(err, errUpdateApply) {
		t.Fatalf("install with a failing rename = %v, want %v", err, errUpdateApply)
	}
	if got := runningExe(t, bundleDir); got != "#!old 1.0.0" {
		t.Fatalf("the rollback did not restore the running bundle: %q", got)
	}
	log := logs.text()
	assertNoPath(t, log, bundleDir, b.dataDir, os.TempDir())
	if !strings.Contains(log, "rename: permission denied") {
		t.Fatalf("the log lost the failed operation and its OS error:\n%s", log)
	}
	if strings.Contains(log, "Notes.app") {
		t.Fatalf("the log names the bundle:\n%s", log)
	}
}

// A second rename that fails and a rollback that fails too leave
// <Name>.app.old as the only copy of the app. A retry must refuse
// instead of clearing .old for its own swap.
func TestInstallRetryAfterFailedRollbackKeepsOldBundle(t *testing.T) {
	b, fp, bundleDir := installFixture(t, "1.2.0", installBundleZip(t, installTestID, "1.2.0"))
	logs := &swapLogCapture{}
	b.logger = slog.New(logs)
	current := filepath.Join(bundleDir, "Notes.app")
	old := current + ".old"
	failing := true
	b.updaterFor().rename = failRenameOnto(current, &failing)

	if err := b.InstallUpdate(context.Background()); !errors.Is(err, errUpdateApply) {
		t.Fatalf("install with a failing rename and rollback = %v, want %v", err, errUpdateApply)
	}
	if _, err := os.Lstat(current); !os.IsNotExist(err) {
		t.Fatalf("the premise: the running bundle should be gone after the failed rollback, got %v", err)
	}
	oldExe := filepath.Join(old, "Contents", "MacOS", "Notes")
	if got, err := os.ReadFile(oldExe); err != nil || string(got) != "#!old 1.0.0" {
		t.Fatalf("the premise: .old should hold the running bundle, got %q, %v", got, err)
	}

	// The retry, with renames working again: it must not get as far as
	// RemoveAll(old).
	failing = false
	if err := b.InstallUpdate(context.Background()); !errors.Is(err, errUpdateApply) {
		t.Fatalf("retry over a missing bundle = %v, want %v", err, errUpdateApply)
	}
	if got, err := os.ReadFile(oldExe); err != nil || string(got) != "#!old 1.0.0" {
		t.Fatalf("the retry deleted the only copy of the app: %q, %v", got, err)
	}
	if len(fp.relaunched) != 0 {
		t.Fatalf("relaunched = %v, want none", fp.relaunched)
	}
	log := logs.text()
	if !strings.Contains(log, "ROLLBACK FAILED") || !strings.Contains(log, "the app bundle is missing, so the update was not applied") {
		t.Fatalf("the log does not report the failed rollback and the refused retry:\n%s", log)
	}
	assertNoPath(t, log, bundleDir, b.dataDir, os.TempDir())
	if strings.Contains(log, "Notes.app") {
		t.Fatalf("the log names the bundle:\n%s", log)
	}
}
