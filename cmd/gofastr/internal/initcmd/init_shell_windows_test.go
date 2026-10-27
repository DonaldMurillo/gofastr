package initcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestMain(m *testing.M) {
	clitest.Main(m)
}

func TestInitWindowsShells(t *testing.T) {
	bin := clitest.BuildGofastrBinary(t)
	for _, shell := range []string{"cmd", "powershell", "git-bash"} {
		t.Run(shell, func(t *testing.T) {
			work := filepath.Join(t.TempDir(), "path with spaces")
			if err := os.Mkdir(work, 0o755); err != nil {
				t.Fatal(err)
			}
			var bash string
			if shell == "git-bash" {
				git, err := exec.LookPath("git.exe")
				if err != nil {
					t.Skip("Git for Windows is not installed")
				}
				// Git Bash's PATH can select mingw64/bin/git.exe instead
				// of cmd/git.exe. Check both installation layouts.
				for _, candidate := range []string{
					filepath.Join(filepath.Dir(git), "..", "bin", "bash.exe"),
					filepath.Join(filepath.Dir(git), "..", "..", "bin", "bash.exe"),
				} {
					if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
						bash = candidate
						break
					}
				}
				if bash == "" {
					t.Skipf("Git Bash is not available beside %s", git)
				}
			}
			newCommand := func() *exec.Cmd {
				var cmd *exec.Cmd
				switch shell {
				case "cmd":
					cmd = exec.Command("cmd.exe")
					// cmd.exe does not use CommandLineToArgvW quoting.
					cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c ""%GOFASTR_INIT_TEST_BIN%" init shell-app --module=local/shell-app"`}
				case "powershell":
					cmd = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
						`& $env:GOFASTR_INIT_TEST_BIN init shell-app --module=local/shell-app; exit $LASTEXITCODE`)
				case "git-bash":
					cmd = exec.Command(bash, "--noprofile", "--norc", "-c",
						`"$GOFASTR_INIT_TEST_BIN" init shell-app --module=local/shell-app`)
				}
				cmd.Dir = work
				cmd.Env = append(os.Environ(), "GOFASTR_INIT_TEST_BIN="+filepath.ToSlash(bin))
				return cmd
			}
			cmd := newCommand()
			out, err := cmd.CombinedOutput()
			if err != nil || strings.Count(string(out), "Created project shell-app") != 1 || strings.Contains(string(out), "already exists") {
				t.Fatalf("%s fresh init: %v\n%s", shell, err, out)
			}
			if _, err := os.Stat(filepath.Join(work, "shell-app", "go.mod")); err != nil {
				t.Fatal(err)
			}
			// A launcher retry must fail before it changes the successful
			// scaffold, and the error must explain that this run wrote nothing.
			sentinel := filepath.Join(work, "shell-app", "main.go")
			envPath := filepath.Join(work, "shell-app", ".env")
			envBefore, err := os.ReadFile(envPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(sentinel, []byte("keep this file\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err = newCommand().CombinedOutput()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 ||
				!strings.Contains(string(out), "already exists") ||
				!strings.Contains(string(out), "This invocation has not written any files") ||
				strings.Contains(string(out), "Created project") ||
				strings.Contains(string(out), "run 'gofastr init .'") {
				t.Fatalf("%s repeat init: %v\n%s", shell, err, out)
			}
			if got, err := os.ReadFile(sentinel); err != nil || string(got) != "keep this file\n" {
				t.Fatalf("%s repeat init changed existing file: %q, %v", shell, got, err)
			}
			if got, err := os.ReadFile(envPath); err != nil || string(got) != string(envBefore) {
				t.Fatalf("%s repeat init changed the existing .env: %v", shell, err)
			}
		})
	}
}
