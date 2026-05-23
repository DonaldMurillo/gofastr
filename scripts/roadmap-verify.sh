#!/bin/bash
# Build-check script for roadmap worktree
# Run from the worktree root: bash scripts/roadmap-verify.sh

set -e

echo "=== Roadmap Worktree Verification ==="
echo ""

echo "--- Building new packages ---"
go build ./framework/routegroup/     && echo "✓ routegroup"      || echo "✗ routegroup"
go build ./framework/apiversions/    && echo "✓ apiversions"     || echo "✗ apiversions"
go build ./framework/lifecycle/      && echo "✓ lifecycle"       || echo "✗ lifecycle"
go build ./framework/i18nui/         && echo "✓ i18nui"          || echo "✗ i18nui"
go build ./core/config/              && echo "✓ config"           || echo "✗ config"
go build ./core/stream/              && echo "✓ stream (websocket)" || echo "✗ stream"
go build ./battery/redisidempotency/ && echo "✓ redisidempotency" || echo "✗ redisidempotency"
go build ./battery/redisflags/       && echo "✓ redisflags"       || echo "✗ redisflags"
go build ./framework/migrate/        && echo "✓ migrate (bulk)"   || echo "✗ migrate"
go build ./cmd/gofastr/              && echo "✓ cmd/gofastr (new)" || echo "✗ cmd/gofastr"

echo ""
echo "--- Building modified packages ---"
go build ./framework/                && echo "✓ framework (app.go)" || echo "✗ framework"
go build ./framework/crud/           && echo "✓ crud"             || echo "✗ crud"
go build ./framework/ui/             && echo "✓ ui (new components)" || echo "✗ ui"
go build ./core-ui/app/              && echo "✓ core-ui/app (screen groups)" || echo "✗ core-ui/app"

echo ""
echo "--- Running new tests ---"
go test ./framework/routegroup/      -count=1 -timeout 30s 2>&1 | tail -3
go test ./framework/apiversions/     -count=1 -timeout 30s 2>&1 | tail -3
go test ./framework/lifecycle/       -count=1 -timeout 30s 2>&1 | tail -3
go test ./core/config/               -count=1 -timeout 30s 2>&1 | tail -3
go test ./core-ui/app/               -count=1 -timeout 30s 2>&1 | tail -3

echo ""
echo "--- Full build ---"
go build ./...                        && echo "✓ full build"     || echo "✗ full build"

echo ""
echo "=== Done ==="
