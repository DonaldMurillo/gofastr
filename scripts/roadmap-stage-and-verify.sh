#!/bin/bash
# Roadmap worktree — stage + build + test
# Run from the worktree root:
#   cd /Users/dom/programming/gofastr/.pi/worktrees/roadmap
#   bash scripts/roadmap-stage-and-verify.sh

set -e
cd "$(dirname "$0")/.."

echo "=== Staging new files ==="
git add \
  framework/routegroup/group.go \
  framework/routegroup/group_test.go \
  framework/reexports_routegroup.go \
  framework/reexports_apiversions.go \
  core-ui/app/screen_group.go \
  core-ui/app/screen_group_test.go \
  framework/apiversions/version.go \
  framework/apiversions/projection.go \
  framework/apiversions/version_test.go \
  core/stream/websocket.go \
  core/stream/hub.go \
  cmd/gofastr/new.go \
  core/config/config.go \
  core/config/config_test.go \
  framework/lifecycle/lifecycle.go \
  framework/lifecycle/lifecycle_test.go \
  framework/i18nui/i18nui.go \
  battery/redisidempotency/store.go \
  battery/redisflags/store.go \
  framework/migrate/bulk.go \
  framework/ui/datepicker.go \
  framework/ui/repeater.go \
  framework/ui/wizard.go \
  framework/ui/inlineedit.go \
  framework/ui/form_inputs.go \
  scripts/roadmap-verify.sh \
  scripts/roadmap-stage-and-verify.sh

echo "=== Staging modified files ==="
git add \
  framework/app.go \
  framework/crud/crud.go \
  framework/internal/casing/casing.go \
  framework/ARCHITECTURE.md \
  core-ui/ARCHITECTURE.md \
  core-ui/runtime/runtime.js \
  core-ui/app/router.go \
  cmd/gofastr/main.go \
  ROADMAP.md

echo "=== Building new packages ==="
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
echo "=== Building modified packages ==="
go build ./framework/                && echo "✓ framework (app.go)" || echo "✗ framework"
go build ./framework/crud/           && echo "✓ crud"             || echo "✗ crud"
go build ./framework/ui/             && echo "✓ ui (new components)" || echo "✗ ui"
go build ./core-ui/app/              && echo "✓ core-ui/app (screen groups)" || echo "✗ core-ui/app"

echo ""
echo "=== Running new tests ==="
go test ./framework/routegroup/      -count=1 -timeout 30s -v 2>&1 | tail -5
go test ./framework/apiversions/     -count=1 -timeout 30s -v 2>&1 | tail -5
go test ./framework/lifecycle/       -count=1 -timeout 30s -v 2>&1 | tail -5
go test ./core/config/               -count=1 -timeout 30s -v 2>&1 | tail -5
go test ./core-ui/app/               -count=1 -timeout 30s -v 2>&1 | tail -5

echo ""
echo "=== Full repo build ==="
go build ./... && echo "✓ FULL BUILD PASSED" || echo "✗ FULL BUILD FAILED"

echo ""
echo "=== Running existing tests to check for regressions ==="
go test ./framework/... -count=1 -timeout 120s 2>&1 | tail -5

echo ""
echo "=== Done ==="
