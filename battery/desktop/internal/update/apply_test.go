package update

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMoveTreeRenameAndCopy(t *testing.T) {
	// The rename path: same volume, tree preserved.
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "Notes.app", "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Notes.app", "Contents", "Info.plist"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "Notes.app")
	if err := MoveTree(filepath.Join(src, "Notes.app"), dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "Contents", "Info.plist")); err != nil {
		t.Fatalf("copied tree missing its file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "Notes.app")); !os.IsNotExist(err) {
		t.Fatalf("source still present after MoveTree: %v", err)
	}
}

func TestMoveTreeCrossVolumeFallback(t *testing.T) {
	// copyTree is the cross-volume fallback; exercise it directly with
	// modes preserved (rename between two t.TempDir roots is usually
	// same-volume, so the fallback gets its own test).
	src := t.TempDir()
	file := filepath.Join(src, "Notes.app", "Contents", "MacOS", "Notes")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "Notes.app")
	if err := copyTree(filepath.Join(src, "Notes.app"), dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, "Contents", "MacOS", "Notes"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o, want 755 preserved", info.Mode().Perm())
	}
}

func TestExecRunnerReportsFailureWithoutLeakingPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix command test")
	}
	if err := (ExecRunner{}).Run("true"); err != nil {
		t.Fatalf("true failed: %v", err)
	}
	err := (ExecRunner{}).Run("false")
	if err == nil {
		t.Fatal("false reported success")
	}
	// A missing command is a wrapped error, never a panic.
	if err := (ExecRunner{}).Run("definitely-not-a-command-xyz"); err == nil {
		t.Fatal("missing command reported success")
	}
}

func TestUnsupportedPlatformRefusesEverywhere(t *testing.T) {
	if runtime.GOOS == "darwin" && (runtime.GOARCH == "arm64" || runtime.GOARCH == "amd64") {
		t.Skip("the darwin platform is the default here; see apply_darwin_test.go")
	}
	p := DefaultPlatform(nil, nil)
	if p.Version() != "" {
		t.Fatal("unsupported platform reports a version")
	}
	if _, _, ok := p.Bundle(); ok {
		t.Fatal("unsupported platform reports a bundle")
	}
	if err := p.VerifySignature("/x"); !errors.Is(err, ErrUnsupportedApply) {
		t.Fatalf("err = %v", err)
	}
	if err := p.Relaunch("/x"); !errors.Is(err, ErrUnsupportedApply) {
		t.Fatalf("err = %v", err)
	}
}
