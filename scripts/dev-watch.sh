#!/bin/bash
# Dev watch loop: rebuild + restart a target app on any .go change
# under core-ui/, framework/, $TARGET. Polls every 1s.
#
# Pairs with the dev-only livereload that GOFASTR_DEV=1 unlocks —
# the browser long-polls /__livereload; when this script kills the
# old binary the fetch errors and the page reloads as soon as the
# new binary is up.
#
# Usage:   PORT=8082 ./scripts/dev-watch.sh                     # examples/site (default)
#          TARGET=examples/blog PORT=8083 ./scripts/dev-watch.sh
# Stop:    Ctrl-C
set -u

# Run from the repo root (this script's parent directory).
cd "$(dirname "$0")/.."

TARGET=${TARGET:-examples/site}
NAME=$(basename "$TARGET")
# Watch the framework dirs + whatever target dir was passed in. Apps under
# examples/site, examples/blog, etc. all get the same rebuild + reload loop
# without having to fork this script.
WATCH_DIRS=(core-ui framework "$TARGET" core/render core/markdown)
BIN=/tmp/$NAME-live
PORT=${PORT:-8082}

last_hash=""
PID=""

cleanup() { [ -n "$PID" ] && kill "$PID" 2>/dev/null; exit 0; }
trap cleanup INT TERM

current_hash() {
  # Watch .go + embedded asset types — runtime.js is //go:embed'd into
  # core-ui/runtime, theme.css is built from .go but inline tweaks to
  # the runtime/CSS need a rebuild too.
  find "${WATCH_DIRS[@]}" \( -name '*.go' -o -name '*.js' -o -name '*.css' \) -type f -print0 2>/dev/null \
    | xargs -0 stat -f '%m %N' 2>/dev/null \
    | sort | shasum | awk '{print $1}'
}

# Inject the framework version from the deployment's git tag so examples/site
# shows the real version instead of a hand-bumped constant (ignored by targets
# that don't define main.siteVersion). Empty when there's no tag → "dev".
VER=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
LDFLAGS=""
[ -n "$VER" ] && LDFLAGS="-ldflags=-X=main.siteVersion=$VER"

while true; do
  h=$(current_hash)
  if [ "$h" != "$last_hash" ]; then
    last_hash="$h"
    [ -n "$PID" ] && kill "$PID" 2>/dev/null && wait "$PID" 2>/dev/null
    if go build $LDFLAGS -o "$BIN" "./$TARGET" 2>&1; then
      # GOFASTR_DEV=1 unlocks /__livereload* endpoints so the browser
      # auto-refreshes after each rebuild.
      PORT=$PORT GOFASTR_DEV=1 "$BIN" > /tmp/$NAME-live.log 2>&1 &
      PID=$!
      echo "[$(date +%H:%M:%S)] restarted $NAME pid=$PID on :$PORT"
    else
      echo "[$(date +%H:%M:%S)] BUILD FAILED — keeping previous server"
    fi
  fi
  sleep 1
done
