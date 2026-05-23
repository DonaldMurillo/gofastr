#!/bin/bash
set -e
cd /Users/dom/programming/gofastr/.pi/worktrees/roadmap

echo "=== BUILD ==="
go build ./...
echo "BUILD: CLEAN"

echo ""
echo "=== UI COMPONENT TESTS ==="
go test ./framework/ui/ -run "TestDatePicker|TestRepeater|TestWizard|TestInlineEdit" -v -count=1 2>&1

echo ""
echo "=== I18NUI TESTS ==="
go test ./framework/i18nui/ -v -count=1 2>&1

echo ""
echo "=== STREAM TESTS ==="
go test ./core/stream/ -v -count=1 2>&1

echo ""
echo "=== DSL TESTS ==="
go test ./framework/dsl/ -v -count=1 2>&1

echo ""
echo "=== ALL NEW PACKAGE TESTS ==="
go test ./framework/routegroup/ ./framework/apiversions/ ./framework/lifecycle/ ./core/config/ ./core-ui/app/ -v -count=1 2>&1

echo ""
echo "=== DONE ==="
