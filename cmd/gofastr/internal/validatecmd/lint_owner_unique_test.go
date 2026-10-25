package validatecmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestValidateWarnsOwnerScopedUnique(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteFileAll(t, path, clitest.OwnerUniqueYML)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunValidate([]string{path}) })
	})
	if code != -1 {
		t.Fatalf("a warning must not fail validate, got exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "customers") || !strings.Contains(out, "user_id") {
		t.Fatalf("validate should warn about customers.email:\n%s", out)
	}
}
