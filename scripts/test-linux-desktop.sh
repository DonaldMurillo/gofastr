#!/usr/bin/env bash
# Run the real Linux shell tests, not the default no-display suite. Each
# graphical invocation gets its own process, an authenticated Xvfb and a
# session bus. The activation transport check needs only its private bus.
# Requires GTK 3, WebKitGTK 4.1, dbus, xvfb, xauth, dunst >= 1.7.0, xdotool
# and xcompmgr.
set -euo pipefail

cd "$(dirname "$0")/.."
# WebKit treats any present value other than "0", including an empty value,
# as disabling its sandbox. Test presence separately from string length.
if [[ ${WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS+x} == x && ${WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS} != 0 ]]; then
  echo "Refusing graphical tests with WebKit's process sandbox disabled." >&2
  exit 1
fi
for tool in go dbus-run-session dbus-daemon xvfb-run xauth dunst dunstctl xdotool xcompmgr; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Linux desktop tests require $tool on PATH." >&2
    exit 1
  fi
done

# dunstctl count arrived in 1.6; the fixture's width/height/origin settings
# require 1.7. Reject older or unidentifiable installations before any suites.
dunst_pattern='^Dunst[[:space:]]+-[[:space:]]+.*notification-daemon[[:space:]]+v?([0-9]+)\.([0-9]+)\.([0-9]+)([^0-9.]|$)'
if ! dunst_info=$(dunst --version 2>/dev/null) || [[ ! $dunst_info =~ $dunst_pattern ]]; then
  echo "Linux desktop tests require dunst >= 1.7.0; cannot determine the installed version." >&2
  exit 1
fi
dunst_version="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[3]}"
if [[ $(printf '%s\n' 1.7.0 "$dunst_version" | sort -V | head -n 1) != 1.7.0 ]]; then
  echo "Linux desktop tests require dunst >= 1.7.0 (count and geometry support); found $dunst_version." >&2
  exit 1
fi

# Ignore inherited cross-compilation settings: these binaries run here.
host_os=$(go env GOHOSTOS)
if [[ $host_os != linux ]]; then
  echo "Linux desktop graphical tests require a Linux host." >&2
  exit 1
fi
export GOOS="$host_os" GOARCH="$(go env GOHOSTARCH)"
output="$PWD/dist/ci/linux-native"
mkdir -p "$output"
export CGO_ENABLED=0 GOFASTR_LINUX_COMPOSITOR=1 GOFASTR_LINUX_NATIVE=1 GOFASTR_LINUX_ACTIVATION=1 GOFASTR_LINUX_PROFILE=1 GOFASTR_LINUX_TRAY=1 GOFASTR_LINUX_NOTIFICATIONS=1 GOFASTR_LINUX_PICKERS=1 GDK_BACKEND=x11
# The permission-dialog test selects an English label. Do not inherit the
# developer's desktop locale (LANGUAGE takes precedence over LANG in gettext).
export LANG=C.UTF-8 LC_ALL=C.UTF-8 LANGUAGE=en
export GOFASTR_LINUX_TEST_OUTPUT="$output"

# One go test invocation per suite: a native crash then names its suite, and
# TestNativeGTKLifetimes alone proves several shell lifetimes share a process.
# -count=1 also forbids a cached success from standing in for actual
# graphical execution.
for test in TestNativeGTKWebKit TestNativeGTKLifetimes TestNativeWindowEffects TestNativeGoFastrBridge TestNativeActivation TestNativeActivationGraphical TestNativeProfile TestNativeTray TestNativeTrayGraphical TestNativeNotifications TestNativeNotificationGraphical TestNativePickersAutomated; do
  log="$output/$test.log"
  timeout=90s
  linker=()
  case "$test" in
    TestNativeActivation) timeout=120s ;;
    TestNativeActivationGraphical) timeout=150s ;;
    TestNativeProfile) timeout=240s ;;
    TestNativeTray) timeout=120s ;;
    TestNativeTrayGraphical) timeout=180s ;;
  esac
  if [[ $test == TestNativeActivation* ]]; then
    # Exercise the production embedded-scheme gate. Do not set this for the
    # original smoke tests: their default, unembedded behavior must stay covered.
    linker=(-ldflags '-X github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=gofastr605test')
  fi
  runner=(dbus-run-session --)
  if [[ $test != TestNativeActivation && $test != TestNativeTray ]]; then
    runner+=(xvfb-run --auto-servernum '--server-args=-screen 0 1280x800x24 -nolisten tcp')
  fi
  # bash 3.2 (macOS /bin/bash) reads an empty array as unbound under set -u.
  "${runner[@]}" go test ./battery/desktop/linux -run "^${test}$" -count=1 -timeout="$timeout" -v ${linker[@]+"${linker[@]}"} \
    2>&1 | tee "$log"
  # Missing/renamed tests and t.Skip both make go test exit zero. Require
  # this specific test to pass so CI cannot turn green without exercising it.
  if ! grep -q "^--- PASS: ${test} (" "$log"; then
    echo "Linux graphical test $test did not run and pass." >&2
    exit 1
  fi
done
