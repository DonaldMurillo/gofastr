package validatecmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestValidateWarnsOwnerScopedUnique(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module github.com/DonaldMurillo/gofastr/example\n")
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteFileAll(t, path, clitest.OwnerUniqueYML)
	var code int
	out := clitest.CovT_capOutput(t, func() {
		code = clitest.CovT_capExit(t, func() { RunValidate([]string{path}) })
	})
	if code != -1 {
		t.Fatalf("a warning must not fail validate, got exit %d\n%s", code, out)
	}
	collision := strings.Index(out, "ambiguous import")
	unique := strings.Index(out, "uniqueness spans every account")
	if collision < 0 || unique < 0 || collision >= unique {
		t.Fatalf("validate should print the module collision before the uniqueness warning:\n%s", out)
	}
}
