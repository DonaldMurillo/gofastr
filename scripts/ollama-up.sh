#!/usr/bin/env bash
# Bring up the local Ollama service and ensure the default embedding
# model is pulled. Idempotent — running it twice is a no-op when
# everything is already in place.
#
# Usage:
#   scripts/ollama-up.sh                # default model: nomic-embed-text
#   OLLAMA_MODEL=mxbai-embed-large scripts/ollama-up.sh
#
# Exit codes:
#   0 — ollama is up and the model is pulled
#   1 — docker missing or compose up failed
#   2 — ollama did not become healthy within the timeout
#   3 — model pull failed
set -euo pipefail

MODEL="${OLLAMA_MODEL:-nomic-embed-text}"
PORT="${OLLAMA_PORT:-11434}"
TIMEOUT_S="${OLLAMA_READY_TIMEOUT:-60}"

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red()   { printf '\033[31m%s\033[0m\n' "$*"; }
info()  { printf '  → %s\n' "$*"; }

if ! command -v docker >/dev/null 2>&1; then
  red "✗ docker not found on PATH"
  exit 1
fi

info "starting ollama container (compose service: ollama)"
docker compose up -d ollama >/dev/null

info "waiting up to ${TIMEOUT_S}s for http://localhost:${PORT}/api/tags to respond"
deadline=$(( $(date +%s) + TIMEOUT_S ))
while :; do
  if curl -fsS "http://localhost:${PORT}/api/tags" >/dev/null 2>&1; then
    break
  fi
  if [ "$(date +%s)" -ge "$deadline" ]; then
    red "✗ ollama did not become ready within ${TIMEOUT_S}s"
    info "logs:"
    docker compose logs --tail=40 ollama || true
    exit 2
  fi
  sleep 1
done
green "✓ ollama is up at http://localhost:${PORT}"

# Check if the model is already pulled; pull if not.
if curl -fsS "http://localhost:${PORT}/api/tags" \
    | grep -q "\"name\":\"${MODEL}\""; then
  green "✓ model ${MODEL} already pulled"
else
  info "pulling model ${MODEL} (one-time per workstation, ~270 MB for nomic-embed-text)"
  if ! docker exec gofastr-ollama ollama pull "${MODEL}"; then
    red "✗ ollama pull ${MODEL} failed"
    exit 3
  fi
  green "✓ pulled ${MODEL}"
fi

info "next steps:"
info "  make embed-live                     # run live-tagged tests"
info "  EMBED_BACKEND=ollama gofastr embed query 'your query'"
