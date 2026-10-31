package appgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The generated authPolicy looped over GetRoles itself, so a host's
// access Decider never bound a generated role-gated screen (2026-10-27
// G17). It composes auth.SessionPolicy and auth.RolePolicy now; RolePolicy
// asks the Decider first. Meridian's hand-owned app.go carries the same
// function, and examples/meridian's TestAuthPolicyAsksDecider drives it.
//
//gofastr:test-reads /examples/meridian/app.go
func TestAuthPolicyAsksTheDecider(t *testing.T) {
	want := appSpecAuthPolicyFunc()
	if !strings.Contains(want, "auth.RolePolicy(") || strings.Contains(want, "GetRoles()") {
		t.Fatalf("SECURITY: the generated authPolicy checks roles itself:\n%s", want)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `app:
  name: Gate
  module: example.com/gate
  auth:
    enabled: true
screens:
  - name: admin
    route: /admin-only
    access:
      role: admin
    body:
      - kind: markdown
        text: hi
`)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	if got := fileContent(mustRenderAppSpecFiles(t, bp), "app.go"); !strings.Contains(got, want) {
		t.Errorf("app.go does not carry the composed authPolicy:\n%s", got)
	}
	meridian, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "examples", "meridian", "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(meridian), want) {
		t.Error("examples/meridian/app.go's authPolicy differs from the generator's")
	}
}
