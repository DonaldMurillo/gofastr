#!/usr/bin/env bash
# Vendors the gofastr-plugins registry index into the docs site.
#
# The site consumes plugins.json by COPY, not by import (a Go import would
# make gofastr depend on a repo that depends on gofastr). This fetches the
# published copy from a gofastr-plugins release, which carries a `release`
# stamp (tag, commit, published, source) the file in that repo's git does
# not, so the vendored copy says where it came from. The site's
# TestPluginRegistryVendoredCopy fails on a copy without the stamp.
#
# Usage:
#   scripts/vendor-plugins-json.sh            # latest release
#   scripts/vendor-plugins-json.sh v0.5.0     # a pinned release
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/examples/site/plugins/plugins.json"
REPO="https://github.com/DonaldMurillo/gofastr-plugins"
TAG="${1:-latest}"

if [ "$TAG" = "latest" ]; then
  URL="$REPO/releases/latest/download/plugins.json"
else
  URL="$REPO/releases/download/$TAG/plugins.json"
fi

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
curl -fsSL "$URL" -o "$TMP"
if ! grep -q '"release"' "$TMP"; then
  echo "vendor-plugins-json: $URL carries no release stamp; refusing to vendor an unstamped copy" >&2
  exit 1
fi
mv "$TMP" "$OUT"
trap - EXIT
echo "vendored $URL -> ${OUT#"$ROOT"/}"
echo "now run: go test ./examples/site -run 'TestPlugin'"
