#!/usr/bin/env bash
# Run the real Linux shell tests, not the default no-display suite. Each
# invocation owns one GTK lifetime, an authenticated Xvfb and a session bus.
# Requires GTK 3, WebKitGTK 4.1, dbus, xvfb and xauth runtime packages.
set -euo pipefail

cd "$(dirname "$0")/.."
# WebKit treats any present value other than "0", including an empty value,
# as disabling its sandbox. Test presence separately from string length.
if [[ ${WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS+x} == x && ${WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS} != 0 ]]; then
  echo "Refusing graphical tests with WebKit's process sandbox disabled." >&2
  exit 1
fi
for tool in go dbus-run-session xvfb-run xauth; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Linux desktop tests require $tool on PATH." >&2
    exit 1
  fi
done

# Ignore inherited cross-compilation settings: these binaries run here.
host_os=$(go env GOHOSTOS)
if [[ $host_os != linux ]]; then
  echo "Linux desktop graphical tests require a Linux host." >&2
  exit 1
fi
export GOOS="$host_os" GOARCH="$(go env GOHOSTARCH)"
output="$PWD/dist/ci/linux-native"
mkdir -p "$output"
export CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 GDK_BACKEND=x11
# The permission-dialog test selects an English label. Do not inherit the
# developer's desktop locale (LANGUAGE takes precedence over LANG in gettext).
export LANG=C.UTF-8 LC_ALL=C.UTF-8 LANGUAGE=en
export GOFASTR_LINUX_TEST_OUTPUT="$output"

# Do not combine these in a single go test invocation: GTK currently keeps
# ownership of its UI thread after a shell exits. -count=1 also forbids a
# cached success from standing in for actual graphical execution.
for test in TestNativeGTKWebKit TestNativeGoFastrBridge; do
  log="$output/$test.log"
  dbus-run-session -- xvfb-run --auto-servernum \
    --server-args='-screen 0 1280x800x24 -nolisten tcp' \
    go test ./battery/desktop/linux -run "^${test}$" -count=1 -timeout=90s -v \
    2>&1 | tee "$log"
  # Missing/renamed tests and t.Skip both make go test exit zero. Require
  # this specific test to pass so CI cannot turn green without exercising it.
  if ! grep -q "^--- PASS: ${test} (" "$log"; then
    echo "Linux graphical test $test did not run and pass." >&2
    exit 1
  fi
done
