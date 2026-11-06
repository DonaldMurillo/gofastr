package desktopcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacBuildRefusedArchKeepsBundle(t *testing.T) {
	t.Setenv("GOARCH", "amd64")
	out := t.TempDir()
	sentinel := filepath.Join(out, "Notes.app", "Contents", "MacOS", "Notes")
	if err := os.MkdirAll(filepath.Dir(sentinel), 0o700); err != nil {
		t.Fatal(err)
	}
	const successfulBuild = "previous successful build"
	if err := os.WriteFile(sentinel, []byte(successfulBuild), 0o600); err != nil {
		t.Fatal(err)
	}
	output, code := verbOutput(t, func() {
		runDesktopBuildForOS([]string{"--id=dev.gofastr.notes", "--name=Notes", "--no-sign", "-o", out}, "darwin")
	})
	if code != 1 || !strings.Contains(output, "GOARCH=amd64") {
		t.Fatalf("architecture refusal exited %d: %s", code, output)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != successfulBuild {
		t.Fatalf("refused architecture destroyed the existing bundle: %q, %v", data, err)
	}
}
