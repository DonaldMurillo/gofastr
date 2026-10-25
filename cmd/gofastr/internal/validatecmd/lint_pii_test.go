package validatecmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

const piiAuthOnYML = `
app:
  name: Demo
  auth:
    enabled: true
entities:
  - name: users
    fields:
      - name: email
        type: string
`

func TestValidatePIIExits1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteFileAll(t, path, clitest.PiiTripYML)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunValidate([]string{path}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	for _, want := range []string{"patients", "email", "ssn", "owner_field", "access", "multi_tenant"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output should contain %q:\n%s", want, out)
		}
	}
}

// The blog repro end to end: auth enabled in the app spec, users entity
// with email, no scoping; validate must still exit 1.
func TestValidateAuthOnPIIExits1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteFileAll(t, path, piiAuthOnYML)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunValidate([]string{path}) })
	})
	if code != 1 {
		t.Fatalf("auth alone must not suppress unscoped PII, want exit 1, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "users") {
		t.Fatalf("output should name the entity:\n%s", out)
	}
}

func TestValidatePIIOwnerFieldOk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteFileAll(t, path, clitest.PiiOwnerYML)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunValidate([]string{path}) })
	})
	if code != -1 {
		t.Fatalf("owner_field set, app spec should validate, got exit %d\n%s", code, out)
	}
}
