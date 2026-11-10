package desktopcmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// previousBundle writes a stand-in for an earlier successful build and
// returns the dist dir and a check that it is still there.
func previousBundle(t *testing.T) (out string, intact func() error) {
	t.Helper()
	out = t.TempDir()
	sentinel := filepath.Join(out, "Notes.app", "Contents", "MacOS", "Notes")
	if err := os.MkdirAll(filepath.Dir(sentinel), 0o700); err != nil {
		t.Fatal(err)
	}
	const successfulBuild = "previous successful build"
	if err := os.WriteFile(sentinel, []byte(successfulBuild), 0o600); err != nil {
		t.Fatal(err)
	}
	return out, func() error {
		data, err := os.ReadFile(sentinel)
		if err != nil {
			return err
		}
		if string(data) != successfulBuild {
			return errors.New("the bundle executable was rewritten")
		}
		return nil
	}
}

func TestMacBuildRefusedArchKeepsBundle(t *testing.T) {
	t.Setenv("GOARCH", "386")
	out, intact := previousBundle(t)
	output, code := verbOutput(t, func() {
		runDesktopBuildForOS([]string{"--id=dev.gofastr.notes", "--name=Notes", "--no-sign", "-o", out}, "darwin")
	})
	if code != 1 || !strings.Contains(output, "GOARCH=386") {
		t.Fatalf("architecture refusal exited %d: %s", code, output)
	}
	if err := intact(); err != nil {
		t.Fatalf("refused architecture destroyed the existing bundle: %v", err)
	}
}

// A universal build needs lipo. Without it the build stops before it
// touches the previous bundle, and says how to build one architecture.
func TestMacBuildWithoutLipoKeepsBundle(t *testing.T) {
	t.Setenv("GOARCH", "")
	orig := lookTool
	lookTool = func(name string) (string, error) {
		if name == "lipo" {
			return "", errors.New("not found")
		}
		return orig(name)
	}
	t.Cleanup(func() { lookTool = orig })
	out, intact := previousBundle(t)
	output, code := verbOutput(t, func() {
		runDesktopBuildForOS([]string{"--id=dev.gofastr.notes", "--name=Notes", "--no-sign", "-o", out}, "darwin")
	})
	if code != 1 || !strings.Contains(output, "lipo") || !strings.Contains(output, "GOARCH=arm64") {
		t.Fatalf("missing-lipo refusal exited %d: %s", code, output)
	}
	if err := intact(); err != nil {
		t.Fatalf("a build that could not start destroyed the existing bundle: %v", err)
	}
}
