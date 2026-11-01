package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
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
	if err := os.Chmod(file, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "Notes.app")
	if err := copyTree(filepath.Join(src, "Notes.app"), dst); err != nil {
		t.Fatal(err)
	}
	filepermtest.AssertMode(t, filepath.Join(dst, "Contents", "MacOS", "Notes"), 0o755)
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
	if p.Identifier() != "" {
		t.Fatal("unsupported platform reports an identifier")
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

func TestLogDetailDropsPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Notes.app")
	_, statErr := os.Lstat(dir) // a real *fs.PathError naming dir
	renameErr := os.Rename(dir, dir+".old")
	var pe *fs.PathError
	if !errors.As(statErr, &pe) {
		t.Fatalf("the premise: Lstat of a missing path should be a *fs.PathError, got %v", statErr)
	}
	statOp := pe.Op // "lstat"; Windows names the API call
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"path error":         {statErr, statOp},
		"link error":         {renameErr, "rename"},
		"wrapped path error": {fmt.Errorf("desktop: create data dir %s: %w", dir, statErr), statOp},
		"MoveTree fixed":     {errMoveDestExists, errMoveDestExists.Error()},
		"other error":        {fmt.Errorf("must be absolute, got %q", dir), "not a file-system error"},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatal("the premise: the call should have failed")
			}
			got := LogDetail(tc.err)
			if strings.Contains(got, dir) || strings.Contains(got, filepath.Dir(dir)) || strings.Contains(got, "Notes.app") {
				t.Fatalf("LogDetail(%v) = %q names the path", tc.err, got)
			}
			if !strings.HasPrefix(got, tc.want) {
				t.Fatalf("LogDetail = %q, want it to start with %q", got, tc.want)
			}
		})
	}
}

func TestRelativeToArgsRewritesAbsolutePaths(t *testing.T) {
	app := filepath.Join(t.TempDir(), "update-123", "Notes.app")
	out := app + ": invalid signature (code or signature have been modified)\n" +
		"In subcomponent: " + filepath.Join(app, "Contents", "MacOS", "Notes") + "\n" +
		app + ": --verify failed\n"
	got := relativeToArgs(out, []string{"--verify", "--deep", "--strict", app})
	if strings.Contains(got, filepath.Dir(app)) {
		t.Fatalf("the command output still names the staging path:\n%s", got)
	}
	want := "Notes.app: invalid signature (code or signature have been modified)\n" +
		"In subcomponent: " + filepath.Join("Notes.app", "Contents", "MacOS", "Notes") + "\n" +
		"Notes.app: --verify failed\n"
	if got != want {
		t.Fatalf("relativeToArgs =\n%s\nwant\n%s", got, want)
	}
}
