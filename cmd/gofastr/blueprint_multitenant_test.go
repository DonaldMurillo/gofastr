package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// A multi_tenant entity with no host-wired resolver produces a
// silently-broken (fail-closed-empty) app. The generator refuses it.
func TestMultiTenantRefusedWithoutResolver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
app:
  name: Tenanted
  module: example.com/tenanted
entities:
  - name: orders
    multi_tenant: true
    fields:
      - name: total
        type: float
`)
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), "multi_tenant") {
		t.Fatalf("multi_tenant without a resolver should fail validation, got: %v", err)
	}
	if !strings.Contains(err.Error(), "resolver") {
		t.Fatalf("error should point at the missing resolver: %v", err)
	}
}
