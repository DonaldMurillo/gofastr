package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

//gofastr:test-reads /scripts/test-linux-desktop.sh
func TestLinuxDesktopRunner(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "test-linux-desktop.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"pass", "safe-zero", "skip", "missing", "fail", "pass-then-fail", "wrapper-fail", "unsafe-sandbox", "empty-sandbox"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, dir, "scripts/test-linux-desktop.sh", string(src))
			stubs := map[string]string{
				"go": `#!/usr/bin/env bash
set -eu
case "$*" in
  'env GOHOSTOS') echo linux; exit 0 ;;
  'env GOHOSTARCH') echo amd64; exit 0 ;;
esac
printf 'go %s\n' "$*" >> "$TRACE"
[[ $CGO_ENABLED = 0 && $GOFASTR_LINUX_NATIVE = 1 && $GDK_BACKEND = x11 ]]
[[ $GOOS = linux && $GOARCH = amd64 && $LC_ALL = C.UTF-8 && $LANGUAGE = en ]]
[[ $GOFASTR_LINUX_TEST_OUTPUT = "$PWD/dist/ci/linux-native" ]]
[[ $* == 'test ./battery/desktop/linux -run ^TestNative'*'$ -count=1 -timeout=90s -v' ]]
name=${4#^}; name=${name%$}
case "$MODE" in
  skip) printf -- '--- SKIP: %s (0.00s)\nPASS\n' "$name" ;;
  missing) echo 'testing: warning: no tests to run'; echo PASS ;;
  fail) exit 1 ;;
  pass-then-fail) printf -- '--- PASS: %s (0.00s)\n' "$name"; exit 1 ;;
  *) printf -- '--- PASS: %s (0.00s)\nPASS\n' "$name" ;;
esac
`,
				"dbus-run-session": `#!/usr/bin/env bash
set -eu
echo dbus >> "$TRACE"
[[ $1 = -- ]]; shift
[[ $MODE != wrapper-fail ]] || exit 1
exec "$@"
`,
				"xvfb-run": `#!/usr/bin/env bash
set -eu
echo xvfb >> "$TRACE"
[[ $1 = --auto-servernum ]]
[[ $2 = '--server-args=-screen 0 1280x800x24 -nolisten tcp' ]]
shift 2
exec "$@"
`,
				"xauth": "#!/usr/bin/env bash\nexit 0\n",
			}
			for name, body := range stubs {
				path := filepath.Join("bin", name)
				writeTestFile(t, dir, path, body)
				if err := os.Chmod(filepath.Join(dir, path), 0700); err != nil {
					t.Fatal(err)
				}
			}
			// A stale successful log must not satisfy a skipped/missing test.
			writeTestFile(t, dir, "dist/ci/linux-native/TestNativeGTKWebKit.log", "--- PASS: TestNativeGTKWebKit (0.00s)\n")
			trace := filepath.Join(dir, "trace")
			cmd := exec.Command("bash", "scripts/test-linux-desktop.sh")
			cmd.Dir = dir
			cmd.WaitDelay = 5 * time.Second
			for _, kv := range os.Environ() {
				if !strings.HasPrefix(kv, "WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=") {
					cmd.Env = append(cmd.Env, kv)
				}
			}
			cmd.Env = append(cmd.Env, "PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TRACE="+trace, "MODE="+mode, "CGO_ENABLED=1", "GOFASTR_LINUX_NATIVE=0", "GOOS=windows", "GOARCH=arm64",
				"LC_ALL=C", "LANGUAGE=es")
			switch mode {
			case "unsafe-sandbox":
				cmd.Env = append(cmd.Env, "WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1")
			case "empty-sandbox":
				cmd.Env = append(cmd.Env, "WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=")
			case "safe-zero":
				cmd.Env = append(cmd.Env, "WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=0")
			}
			out, err := cmd.CombinedOutput()
			if mode == "pass" || mode == "safe-zero" {
				if err != nil {
					t.Fatalf("runner: %v\n%s", err, out)
				}
				got, err := os.ReadFile(trace)
				if err != nil {
					t.Fatal(err)
				}
				want := "dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativeGTKWebKit$ -count=1 -timeout=90s -v\n" +
					"dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativeGoFastrBridge$ -count=1 -timeout=90s -v\n"
				if string(got) != want {
					t.Errorf("invocations = %q, want %q", got, want)
				}
			} else if err == nil {
				t.Fatalf("%s unexpectedly passed:\n%s", mode, out)
			}
			if mode == "unsafe-sandbox" || mode == "empty-sandbox" {
				if !strings.Contains(string(out), "sandbox disabled") {
					t.Errorf("missing refusal: %s", out)
				}
				if _, err := os.Stat(trace); !os.IsNotExist(err) {
					t.Error("unsafe runner executed a command before refusing")
				}
			}
		})
	}
}
