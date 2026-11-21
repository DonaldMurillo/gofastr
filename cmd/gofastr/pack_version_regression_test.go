package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

func TestPackVersionCheckUsesAppDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(project.EnvNoVersionCheck, "")
	version := cli.Version
	cli.Version = "v0.89.0"
	t.Cleanup(func() { cli.Version = version })

	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/packprobe\n\nrequire "+project.GofastrModule+" v0.90.0\n")
	stderr, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	originalStderr := os.Stderr
	os.Stderr = stderr
	defer func() { os.Stderr = originalStderr }()

	warnVersionMismatch("pack", []string{"--out", "snapshot.yml", dir})
	warning, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(warning), "requires "+project.GofastrModule+" v0.90.0") {
		t.Fatalf("pack did not warn about the target project's version: %q", warning)
	}
}
