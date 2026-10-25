package buildcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// gofastr build regenerates for a settings file that has endpoints and
// nothing else: no codegen section, no entities/client.
func TestBuildGeneratesForEndpointsOnly(t *testing.T) {
	dir := t.TempDir()
	if buildGenerates(false, dir) {
		t.Fatal("an empty project ran codegen")
	}
	if err := os.WriteFile(filepath.Join(dir, "gofastr.settings.yml"), []byte(clitest.EndpointsSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	if !buildGenerates(false, dir) {
		t.Fatal("build skipped codegen for a settings file with endpoints")
	}
}
