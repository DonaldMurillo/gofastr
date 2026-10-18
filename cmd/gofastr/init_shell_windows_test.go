package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestInitWindowsShells(t *testing.T) {
	bin := buildGofastrBin(t)
	for _, shell := range []string{"cmd", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			work := filepath.Join(t.TempDir(), "path with spaces")
			if err := os.Mkdir(work, 0o755); err != nil {
				t.Fatal(err)
			}
			var cmd *exec.Cmd
			if shell == "cmd" {
				cmd = exec.Command("cmd.exe")
				// cmd.exe does not use CommandLineToArgvW quoting.
				cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c ""%GOFASTR_INIT_TEST_BIN%" init shell-app --module=local/shell-app"`}
			} else {
				cmd = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
					`& $env:GOFASTR_INIT_TEST_BIN init shell-app --module=local/shell-app; exit $LASTEXITCODE`)
			}
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "GOFASTR_INIT_TEST_BIN="+bin)
			out, err := cmd.CombinedOutput()
			if err != nil || strings.Count(string(out), "Created project shell-app") != 1 || strings.Contains(string(out), "already exists") {
				t.Fatalf("%s fresh init: %v\n%s", shell, err, out)
			}
			if _, err := os.Stat(filepath.Join(work, "shell-app", "go.mod")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
