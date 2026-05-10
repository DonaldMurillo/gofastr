#!/bin/bash
# Dev watch loop: rebuild + restart the example website on any .go
# change under core-ui/, framework/, examples/website/. Polls every 1s.
#
# Pairs with the dev-only livereload wired into examples/website
# (GOFASTR_DEV=1 enables /__livereload + /__livereload.js, which the
# browser long-polls — when this script kills the old binary, the
# fetch errors and the page reloads as soon as the new binary is up).
#
# Usage:   PORT=8082 ./scripts/dev-watch.sh
# Stop:    Ctrl-C
set -u

# Run from the repo root (this script's parent directory).
cd "$(dirname "$0")/.."

WATCH_DIRS=(core-ui framework examples/website core/render core/markdown)
BIN=/tmp/website-live
PORT=${PORT:-8082}

last_hash=""
PID=""

cleanup() { [ -n "$PID" ] && kill "$PID" 2>/dev/null; exit 0; }
trap cleanup INT TERM

current_hash() {
  find "${WATCH_DIRS[@]}" -name '*.go' -type f -print0 2>/dev/null \
    | xargs -0 stat -f '%m %N' 2>/dev/null \
    | sort | shasum | awk '{print $1}'
}

while true; do
  h=$(current_hash)
  if [ "$h" != "$last_hash" ]; then
    last_hash="$h"
    [ -n "$PID" ] && kill "$PID" 2>/dev/null && wait "$PID" 2>/dev/null
    if go build -o "$BIN" ./examples/website 2>&1; then
      # GOFASTR_DEV=1 unlocks /__livereload* endpoints in main.go so
      # the browser auto-refreshes after each rebuild.
      PORT=$PORT GOFASTR_DEV=1 "$BIN" > /tmp/website-live.log 2>&1 &
      PID=$!
      echo "[$(date +%H:%M:%S)] restarted website pid=$PID on :$PORT"
    else
      echo "[$(date +%H:%M:%S)] BUILD FAILED — keeping previous server"
    fi
  fi
  sleep 1
done
