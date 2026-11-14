package validatecmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestValidateNavWarns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteFileAll(t, path, "app:\n  name: Demo\n  module: example.com/demo\n  static_dir: public\nnav:\n  - label: Owned\n    href: /owned\n  - label: Terms\n    href: /terms.pdf\n")
	clitest.WriteFileAll(t, filepath.Join(dir, "public", "terms.pdf"), "terms")
	var code int
	out := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, func() { RunValidate([]string{path}) }) })
	if code != -1 || !strings.Contains(out, "is valid") || !strings.Contains(out, "nav[0].href") || !strings.Contains(out, "strict boot refuses") || strings.Contains(out, "nav[1].href") {
		t.Fatalf("validate must warn successfully and resolve static paths against project root: exit %d\n%s", code, out)
	}
}
