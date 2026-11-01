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
	for _, mode := range []string{"pass", "safe-zero", "skip", "missing", "fail", "pass-then-fail", "wrapper-fail", "unsafe-sandbox", "empty-sandbox", "activation-skip", "activation-missing", "activation-fail", "graphical-activation-skip", "profile-skip", "profile-missing", "profile-fail", "tray-skip", "tray-missing", "tray-fail", "tray-graphical-skip", "tray-graphical-missing", "tray-graphical-fail", "notifications-skip", "notifications-missing", "notifications-fail", "notification-graphical-skip", "notification-graphical-missing", "notification-graphical-fail", "lifetimes-skip", "lifetimes-missing", "lifetimes-fail", "effects-skip", "effects-missing", "effects-fail", "pickers-skip", "pickers-missing", "pickers-fail", "dunst-minimum", "dunst-current", "dunst-future", "dunst-1.5", "dunst-1.6", "dunst-malformed", "dunst-unrelated-version", "dunst-version-fail", "dunst-v-minimum", "dunst-v-current", "dunst-v-old", "dunst-v-malformed"} {
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
[[ $GOFASTR_LINUX_ACTIVATION = 1 && $GOFASTR_LINUX_PROFILE = 1 && $GOFASTR_LINUX_TRAY = 1 && $GOFASTR_LINUX_NOTIFICATIONS = 1 ]]
[[ $GOFASTR_LINUX_COMPOSITOR = 1 && $GOFASTR_LINUX_PICKERS = 1 ]]
[[ $GOOS = linux && $GOARCH = amd64 && $LC_ALL = C.UTF-8 && $LANGUAGE = en ]]
[[ $GOFASTR_LINUX_TEST_OUTPUT = "$PWD/dist/ci/linux-native" ]]
[[ $1 = test && $2 = ./battery/desktop/linux && $3 = -run && $5 = -count=1 && $7 = -v ]]
name=${4#^}; name=${name%$}
case "$name" in
  TestNativeGTKWebKit|TestNativeGTKLifetimes|TestNativeWindowEffects|TestNativeGoFastrBridge|TestNativeNotifications|TestNativeNotificationGraphical|TestNativePickersAutomated) [[ $# = 7 && $6 = -timeout=90s ]] ;;
  TestNativeProfile) [[ $# = 7 && $6 = -timeout=240s ]] ;;
  TestNativeTray) [[ $# = 7 && $6 = -timeout=120s ]] ;;
  TestNativeTrayGraphical) [[ $# = 7 && $6 = -timeout=180s ]] ;;
  TestNativeActivation|TestNativeActivationGraphical)
    [[ $# = 9 && $8 = -ldflags && $9 = '-X github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=gofastr605test' ]]
    if [[ $name = TestNativeActivation ]]; then [[ $6 = -timeout=120s ]]; else [[ $6 = -timeout=150s ]]; fi
    ;;
  *) exit 1 ;;
esac
mode=$MODE
case "$MODE:$name" in
  activation-skip:TestNativeActivation|graphical-activation-skip:TestNativeActivationGraphical) mode=skip ;;
  activation-missing:TestNativeActivation) mode=missing ;;
  activation-fail:TestNativeActivation) mode=fail ;;
  profile-skip:TestNativeProfile) mode=skip ;;
  profile-missing:TestNativeProfile) mode=missing ;;
  profile-fail:TestNativeProfile) mode=fail ;;
  tray-skip:TestNativeTray|tray-graphical-skip:TestNativeTrayGraphical) mode=skip ;;
  tray-missing:TestNativeTray|tray-graphical-missing:TestNativeTrayGraphical) mode=missing ;;
  tray-fail:TestNativeTray|tray-graphical-fail:TestNativeTrayGraphical) mode=fail ;;
  notifications-skip:TestNativeNotifications|notification-graphical-skip:TestNativeNotificationGraphical) mode=skip ;;
  notifications-missing:TestNativeNotifications|notification-graphical-missing:TestNativeNotificationGraphical) mode=missing ;;
  notifications-fail:TestNativeNotifications|notification-graphical-fail:TestNativeNotificationGraphical) mode=fail ;;
  lifetimes-skip:TestNativeGTKLifetimes|effects-skip:TestNativeWindowEffects|pickers-skip:TestNativePickersAutomated) mode=skip ;;
  lifetimes-missing:TestNativeGTKLifetimes|effects-missing:TestNativeWindowEffects|pickers-missing:TestNativePickersAutomated) mode=missing ;;
  lifetimes-fail:TestNativeGTKLifetimes|effects-fail:TestNativeWindowEffects|pickers-fail:TestNativePickersAutomated) mode=fail ;;
esac
case "$mode" in
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
				"xauth":       "#!/usr/bin/env bash\nexit 0\n",
				"dbus-daemon": "#!/usr/bin/env bash\nexit 0\n",
				"dunst": `#!/usr/bin/env bash
[[ $1 = --version ]] || exit 1
case "$MODE" in
  dunst-minimum) echo 'Dunst - notification-daemon 1.7.0' ;;
  dunst-future) echo 'Dunst - notification-daemon 2.0.0' ;;
  dunst-v-minimum) echo 'Dunst - A customizable and lightweight notification-daemon v1.7.0' ;;
  dunst-v-current) echo 'Dunst - A customizable and lightweight notification-daemon v1.12.2-3-gabc123' ;;
  dunst-v-old) echo 'Dunst - A customizable and lightweight notification-daemon v1.6.1' ;;
  dunst-v-malformed) echo 'Dunst - A customizable and lightweight notification-daemon vv1.7.0' ;;
  dunst-1.5) echo 'Dunst - notification-daemon 1.5.0' ;;
  dunst-1.6) echo 'Dunst - notification-daemon 1.6.1' ;;
  dunst-malformed) echo 'unrecognized version' ;;
  dunst-unrelated-version) echo 'unknown build (libglib 2.80.0)' ;;
  dunst-version-fail) exit 1 ;;
  *) echo 'Dunst - notification-daemon 1.12.2 (2025-03-05)' ;;
esac
`,
				"dunstctl": "#!/usr/bin/env bash\nexit 0\n",
				"xdotool":  "#!/usr/bin/env bash\nexit 0\n",
				"xcompmgr": "#!/usr/bin/env bash\nexit 0\n",
			}
			for name, body := range stubs {
				path := filepath.Join("bin", name)
				writeTestFile(t, dir, path, body)
				if err := os.Chmod(filepath.Join(dir, path), 0700); err != nil {
					t.Fatal(err)
				}
			}
			// A stale successful log must not satisfy a skipped/missing test.
			for _, name := range []string{"TestNativeGTKWebKit", "TestNativeGTKLifetimes", "TestNativeWindowEffects", "TestNativeGoFastrBridge", "TestNativeActivation", "TestNativeActivationGraphical", "TestNativeProfile", "TestNativeTray", "TestNativeTrayGraphical", "TestNativeNotifications", "TestNativeNotificationGraphical", "TestNativePickersAutomated"} {
				writeTestFile(t, dir, "dist/ci/linux-native/"+name+".log", "--- PASS: "+name+" (0.00s)\n")
			}
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
				"TRACE="+trace, "MODE="+mode, "CGO_ENABLED=1", "GOFASTR_LINUX_NATIVE=0", "GOFASTR_LINUX_ACTIVATION=0", "GOFASTR_LINUX_PROFILE=0", "GOFASTR_LINUX_TRAY=0", "GOFASTR_LINUX_NOTIFICATIONS=0", "GOFASTR_LINUX_COMPOSITOR=0", "GOFASTR_LINUX_PICKERS=0", "GOOS=windows", "GOARCH=arm64",
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
			if mode == "pass" || mode == "safe-zero" || mode == "dunst-minimum" || mode == "dunst-current" || mode == "dunst-future" || mode == "dunst-v-minimum" || mode == "dunst-v-current" {
				if err != nil {
					t.Fatalf("runner: %v\n%s", err, out)
				}
				got, err := os.ReadFile(trace)
				if err != nil {
					t.Fatal(err)
				}
				want := "dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativeGTKWebKit$ -count=1 -timeout=90s -v\n" +
					"dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativeGTKLifetimes$ -count=1 -timeout=90s -v\n" +
					"dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativeWindowEffects$ -count=1 -timeout=90s -v\n" +
					"dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativeGoFastrBridge$ -count=1 -timeout=90s -v\n" +
					"dbus\ngo test ./battery/desktop/linux -run ^TestNativeActivation$ -count=1 -timeout=120s -v -ldflags -X github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=gofastr605test\n" +
					"dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativeActivationGraphical$ -count=1 -timeout=150s -v -ldflags -X github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=gofastr605test\n" +
					"dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativeProfile$ -count=1 -timeout=240s -v\n" +
					"dbus\ngo test ./battery/desktop/linux -run ^TestNativeTray$ -count=1 -timeout=120s -v\n" +
					"dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativeTrayGraphical$ -count=1 -timeout=180s -v\n" +
					"dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativeNotifications$ -count=1 -timeout=90s -v\n" +
					"dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativeNotificationGraphical$ -count=1 -timeout=90s -v\n" +
					"dbus\nxvfb\ngo test ./battery/desktop/linux -run ^TestNativePickersAutomated$ -count=1 -timeout=90s -v\n"
				if string(got) != want {
					t.Errorf("invocations = %q, want %q", got, want)
				}
			} else if err == nil {
				t.Fatalf("%s unexpectedly passed:\n%s", mode, out)
			}

			if mode == "dunst-1.5" || mode == "dunst-1.6" || mode == "dunst-malformed" || mode == "dunst-unrelated-version" || mode == "dunst-version-fail" || mode == "dunst-v-old" || mode == "dunst-v-malformed" {
				if !strings.Contains(string(out), "require dunst >= 1.7.0") {
					t.Errorf("missing version prerequisite: %s", out)
				}
				if _, err := os.Stat(trace); !os.IsNotExist(err) {
					t.Error("unsupported dunst ran a native suite")
				}
			}

			if mode == "dunst-v-old" && !strings.Contains(string(out), "found 1.6.1") {
				t.Errorf("tagged old version was not checked against the minimum: %s", out)
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
