package desktopcmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLinuxArchiveRejectsSpecialIcon(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "icon.pipe")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "icon-link")
	if err := os.Symlink(fifo, link); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, path string }{{"fifo", fifo}, {"symlink-to-fifo", link}, {"directory", dir}, {"device", "/dev/null"}} {
		t.Run(test.name, func(t *testing.T) {
			// Keep the regression bounded even if a future change opens a FIFO before
			// rejecting it. No writer is created; the original behavior hangs here.
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestLinuxArchiveSpecialIconProcess$", "-test.v")
			cmd.WaitDelay = time.Second
			cmd.Env = append(os.Environ(), "GOFASTR_TEST_SPECIAL_ICON="+test.path)
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("special icon blocked instead of rejecting: %v\n%s", ctx.Err(), output)
			}
			if err != nil || !strings.Contains(string(output), "--- PASS: TestLinuxArchiveSpecialIconProcess (") {
				t.Fatalf("special icon check: %v\n%s", err, output)
			}
		})
	}
}

func TestLinuxArchiveSpecialIconProcess(t *testing.T) {
	icon := os.Getenv("GOFASTR_TEST_SPECIAL_ICON")
	if icon == "" {
		t.Skip("subprocess helper")
	}
	output := filepath.Join(t.TempDir(), "not-created")
	f := desktopBuildFlags{format: "tar.gz", id: "dev.example.app", version: "1.0", icon: icon, pkg: "./missing", out: output}
	err := buildLinuxDesktopArchive(f, "Notes", "amd64")
	if err == nil || !strings.Contains(err.Error(), "regular PNG file") {
		t.Fatalf("expected regular-file refusal before compiler, got %v", err)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("invalid icon changed output: %v", err)
	}
}
